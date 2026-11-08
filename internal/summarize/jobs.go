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
	// EndedAt is when the session ended. A job whose session ended
	// before the manifest's latest activity is stale and skipped.
	EndedAt string `json:"endedAt,omitempty"`
	// ProjectsRoot is ProjectsRoot() as the hook saw it, an extra root
	// for AllowedTranscript.
	ProjectsRoot string `json:"projectsRoot,omitempty"`
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
	cfg.WorkRoot = filepath.Join(home, "tmp")
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
	if !AllowedTranscript(j.TranscriptPath, j.ProjectsRoot) {
		return false, errors.New("transcript path is not a .jsonl file under a Claude Code projects directory")
	}
	m, err := a.Get(j.ManifestID)
	if err != nil {
		return false, err
	}
	if recorded(m, j.StartedAt) || stale(m, j.EndedAt) {
		return false, nil
	}
	summary, next, err := Run(ctx, cfg, j.TranscriptPath)
	if err != nil {
		return false, err
	}
	applied := false
	_, err = a.Update(j.ManifestID, 0, func(m *manifest.Manifest) error {
		// The model may have recorded state while the summarizer ran.
		if recorded(m, j.StartedAt) || stale(m, j.EndedAt) {
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

// stale reports whether activity on m happened after the job's session
// ended, so a later session's state must not be overwritten by an older
// one (Fable review M11, R2). A job without endedAt is never stale.
func stale(m *manifest.Manifest, endedAt string) bool {
	if endedAt == "" {
		return false
	}
	last, err1 := time.Parse(time.RFC3339, m.State.LastActivityAt)
	end, err2 := time.Parse(time.RFC3339, endedAt)
	if err1 != nil || err2 != nil {
		return m.State.LastActivityAt > endedAt
	}
	return last.After(end)
}

// ProjectsRoot is the Claude Code projects directory as this process
// sees it: $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects. The
// hook records it on each job, because the daemon or drain that runs
// the job may have a different environment.
func ProjectsRoot() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".claude", "projects")
	}
	return ""
}

// AllowedTranscript reports whether path is a regular .jsonl file whose
// fully resolved path is under one of: each root given, the
// $CLAUDE_CONFIG_DIR/projects of this process when set, and
// ~/.claude/projects. Summarize jobs only read such files. ReadFile does
// not check this, because coal import reads directories the user names.
func AllowedTranscript(path string, roots ...string) bool {
	if !strings.HasSuffix(path, ".jsonl") {
		return false
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		roots = append(roots, filepath.Join(d, "projects"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, ".claude", "projects"))
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasSuffix(real, ".jsonl") {
		return false
	}
	if fi, err := os.Stat(real); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	for _, r := range roots {
		if r == "" || !filepath.IsAbs(r) {
			continue
		}
		if root, err := filepath.EvalSymlinks(r); err == nil && strings.HasPrefix(real, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
