package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/fold"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/queue"
)

// hookEvents maps coal hook arguments to Claude Code hook event names.
var hookEvents = map[string]string{
	"session-start":      "SessionStart",
	"user-prompt-submit": "UserPromptSubmit",
	"post-tool-use":      "PostToolUse",
	"stop":               "Stop",
	"session-end":        "SessionEnd",
}

// maxPayload caps hook stdin. Larger payloads are read to EOF and
// dropped, so Claude Code never blocks writing to the hook.
const maxPayload = 16 << 20

// sessionStartWait bounds how long session-start waits for the queue lock.
const sessionStartWait = time.Second

func init() {
	register(hookCmd)
	register(queueCmd)
}

func hookCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "hook <session-start|user-prompt-submit|post-tool-use|stop|session-end>",
		Short:     "Run a Claude Code hook: read its JSON on stdin and queue the event",
		Long:      "Run a Claude Code hook. It never fails the session: errors go to logs/coal.log and the hook exits 0.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"session-start", "user-prompt-submit", "post-tool-use", "stop", "session-end"},
		RunE: func(cmd *cobra.Command, args []string) error {
			stdin := cmd.InOrStdin()
			// Whatever happens, read stdin to EOF, never panic out, and
			// exit 0: a hook must not fail the user's session. The
			// recover is deferred first so it also covers the drain.
			var home string
			defer func() {
				if r := recover(); r != nil {
					hookLog(home, "hook %s: panic: %v\n%s", args[0], r, debug.Stack())
				}
			}()
			defer io.Copy(io.Discard, stdin)
			home, err := dataHome()
			if err != nil {
				return nil
			}
			out, err := runHook(home, args[0], stdin)
			if err != nil {
				hookLog(home, "hook %s: %v", args[0], err)
			}
			if len(out) > 0 {
				cmd.OutOrStdout().Write(out)
			}
			return nil
		},
	}
}

// runHook does the work of one hook and returns what goes on stdout.
func runHook(home, name string, stdin io.Reader) ([]byte, error) {
	event, ok := hookEvents[name]
	if !ok {
		return nil, fmt.Errorf("unknown hook %q", name)
	}
	payload, err := io.ReadAll(io.LimitReader(stdin, maxPayload+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxPayload {
		return nil, fmt.Errorf("payload over %d bytes, ignored", maxPayload)
	}
	e, err := capture.FromClaudeCode(event, payload)
	if err != nil || e == nil {
		return nil, err
	}
	if err := queue.Enqueue(home, e); err != nil {
		return nil, err
	}
	f := &folder{home: home}
	defer f.close()
	if e.Kind != capture.KindSessionStart {
		_, err := queue.Drain(home, f.apply, false)
		return nil, err
	}

	// SessionStart folds what is queued, including the previous
	// session's end and this start, then resumes the matched manifest.
	_, busy, err := queue.DrainUntil(home, f.apply, time.Now().Add(sessionStartWait))
	if err != nil {
		hookLog(home, "hook session-start: drain: %v", err)
	} else if busy {
		hookLog(home, "hook session-start: queue busy for %v; resuming from the last folded state", sessionStartWait)
	}
	a, err := f.open()
	if err != nil {
		return nil, err
	}
	m := sessionManifest(a, e)
	if m == nil {
		return nil, nil
	}
	ids, err := a.Index.IDs()
	if err != nil {
		ids = nil
	}
	out, err := capture.ResumeOutput(m, shortID(m.ID, ids))
	if err == nil {
		metrics.Record(home, metrics.ResumeShown, map[string]any{"manifest": m.ID, "session": e.SessionRef})
	}
	return out, err
}

// sessionManifest returns the manifest the fold attached the session
// to, or a read-only match when the fold did not run.
func sessionManifest(a *app.App, e *capture.Event) *manifest.Manifest {
	var s fold.Session
	if a.Store.ReadSession(e.SessionRef, &s) == nil && s.ManifestID != "" {
		if m, err := a.Get(s.ManifestID); err == nil && (m.Status == manifest.StatusActive || m.Status == manifest.StatusPaused) {
			return m
		}
	}
	ms, _ := a.Store.List()
	return marker.Match(e.Cwd, ms)
}

// folder opens the app only when there is something to fold.
type folder struct {
	home string
	a    *app.App
}

func (f *folder) open() (*app.App, error) {
	if f.a != nil {
		return f.a, nil
	}
	a, err := app.Open(f.home)
	f.a = a
	return a, err
}

func (f *folder) apply(e *capture.Event) error {
	a, err := f.open()
	if err != nil {
		return fmt.Errorf("%w: %v", queue.ErrUnavailable, err)
	}
	return fold.Apply(a, e)
}

func (f *folder) close() {
	if f.a != nil {
		f.a.Close()
	}
}

func hookLog(home, format string, args ...any) {
	if home == "" {
		return
	}
	dir := filepath.Join(home, "logs")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	if fh, err := os.OpenFile(filepath.Join(dir, "coal.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(fh, "%s %s\n", manifest.Now(), fmt.Sprintf(format, args...))
		fh.Close()
	}
}

func queueCmd() *cobra.Command {
	q := &cobra.Command{Use: "queue", Short: "Work with the capture queue"}
	var retry bool
	drain := &cobra.Command{
		Use:   "drain",
		Short: "Fold every queued capture event into manifests, waiting for the lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := dataHome()
			if err != nil {
				return err
			}
			if retry {
				n, err := queue.RetryFailed(home)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "moved %d events from queue/failed back into the queue\n", n)
			}
			f := &folder{home: home}
			defer f.close()
			n, err := queue.Drain(home, f.apply, true)
			if err != nil {
				return err
			}
			failed, _ := filepath.Glob(filepath.Join(queue.Dir(home), "failed", "*.json"))
			fmt.Fprintf(cmd.OutOrStdout(), "folded %d events\n", n)
			if len(failed) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d events in queue/failed; see logs/coal.log\n", len(failed))
			}
			return nil
		},
	}
	drain.Flags().BoolVar(&retry, "retry-failed", false, "move events in queue/failed back into the queue first")
	q.AddCommand(drain)
	return q
}
