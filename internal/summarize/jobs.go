package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

// Tool names the summarizer in state stamps and history commits.
const Tool = "coal-summarizer"

// Job is a summarize request in queue/summarize/<name>.json. Capture's
// fold writes it at session end when config.summarizer is set; only
// the daemon tick and coal queue drain run it, never a hook.
type Job struct {
	ManifestID     string `json:"manifestID"`
	SessionRef     string `json:"sessionRef"`
	TranscriptPath string `json:"transcriptPath"`
	StartedAt      string `json:"startedAt"`
}

// Dir is where jobs wait. Hook drains read only regular files directly
// in queue/, so they never see it.
func Dir(home string) string { return filepath.Join(home, "queue", "summarize") }

// Result counts what RunJobs did.
type Result struct {
	Done, Skipped, Failed int
	Busy                  bool // another runner holds the lock
}

// RunJobs runs every pending job once, oldest name first, while holding
// queue/summarize/.lock. With no summarizer configured it does nothing
// and the jobs wait. A job is deleted after it is applied or skipped,
// and moved to failed/ after one failed attempt.
func RunJobs(ctx context.Context, a *app.App) (Result, error) {
	var r Result
	home := a.Store.Dir()
	cfg, err := LoadConfig(home)
	if err != nil || cfg == nil {
		return r, err
	}
	dir := Dir(home)
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	} else if err != nil {
		return r, err
	}
	var names []string
	for _, e := range ents {
		if n := e.Name(); e.Type().IsRegular() && strings.HasSuffix(n, ".json") && !strings.HasPrefix(n, ".") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return r, nil
	}
	lf, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return r, err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		r.Busy = true
		return r, nil
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	sort.Strings(names)
	for _, n := range names {
		if ctx.Err() != nil {
			break
		}
		path := filepath.Join(dir, n)
		applied, err := runJob(ctx, a, cfg, path)
		switch {
		case errors.Is(err, os.ErrNotExist) && !fileExists(path):
			continue // another writer took it
		case err != nil && ctx.Err() != nil:
			continue // shutting down; the job waits for the next run
		case err != nil:
			a.Store.Log("summarizer: job %s: %v", n, err)
			r.Failed++
			if err := os.MkdirAll(filepath.Join(dir, "failed"), 0o700); err == nil {
				err = os.Rename(path, filepath.Join(dir, "failed", n))
			}
			continue
		case applied:
			r.Done++
		default:
			r.Skipped++
		}
		os.Remove(path)
	}
	return r, nil
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }

// runJob applies one job. It reports false when the model had already
// recorded state, so the summarizer did not run or its result was
// dropped.
func runJob(ctx context.Context, a *app.App, cfg *Config, path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return false, errors.New("job file does not parse")
	}
	if j.ManifestID == "" || j.TranscriptPath == "" {
		return false, errors.New("job has no manifest id or transcript path")
	}
	m, err := a.Get(j.ManifestID)
	if err != nil {
		return false, err
	}
	if recorded(m, j.StartedAt) {
		return false, nil
	}
	summary, next, err := Run(ctx, cfg, j.TranscriptPath)
	if err != nil {
		return false, err
	}
	applied := false
	_, err = a.Update(j.ManifestID, 0, func(m *manifest.Manifest) error {
		// The model may have recorded state while the summarizer ran.
		if recorded(m, j.StartedAt) {
			return store.ErrNoChange
		}
		before := m.State
		m.State.Summary = summary
		if next != "" {
			m.State.NextStep = next
		}
		if !manifest.StateChanged(before, m.State) {
			return store.ErrNoChange
		}
		manifest.StampState(m, Tool)
		applied = true
		return nil
	}, store.Op{Tool: Tool, Action: "summarize session " + j.SessionRef})
	return applied, err
}

// recorded reports whether something other than the summarizer stamped
// the state at or after the session started: the model recorded its own
// state, which always wins. The summarizer's own stamps do not count, or
// a late run for one session would hide the next session.
func recorded(m *manifest.Manifest, startedAt string) bool {
	st, ok := manifest.StateStampOf(m)
	if !ok || st.RecordedBy == Tool {
		return false
	}
	at, err1 := time.Parse(time.RFC3339, st.At)
	start, err2 := time.Parse(time.RFC3339, startedAt)
	if err1 != nil || err2 != nil {
		return st.At >= startedAt
	}
	return !at.Before(start)
}

// Runner runs jobs in the background for the daemon, one run at a time,
// so a slow model call never holds up the daemon's tick.
type Runner struct {
	app     *app.App
	running atomic.Bool
}

// NewRunner returns a runner for a.
func NewRunner(a *app.App) *Runner { return &Runner{app: a} }

// Kick starts a run unless one is in progress. It returns at once.
func (r *Runner) Kick(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.running.Store(false)
		if _, err := RunJobs(ctx, r.app); err != nil {
			r.app.Store.Log("summarizer: %v", err)
		}
	}()
}
