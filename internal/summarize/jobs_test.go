package summarize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/queue"
	"github.com/calebtonn/coal/internal/store"
)

type env struct {
	t     *testing.T
	a     *app.App
	id    string
	tr    string
	ran   string // the fake summarizer touches this file when it runs
	start string
}

func newEnv(t *testing.T, body string) *env {
	t.Helper()
	a, err := app.Open(filepath.Join(t.TempDir(), "coal"), store.WithHistory(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	m := manifest.New("Household budget tracker as a single HTML page", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	if err := a.Create(m, store.Op{Tool: "test"}); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, a: a, id: m.ID, tr: projectTranscript(t, user("/w", "SECRET-ISH transcript text")), ran: filepath.Join(t.TempDir(), "ran"), start: "2026-11-06T10:00:00Z"}
	e.config(map[string]any{"command": []string{script(t, `touch "$1"; cat >/dev/null; `+body), e.ran}, "timeoutSeconds": 1})
	return e
}

// projectTranscript writes body under a fresh $CLAUDE_CONFIG_DIR/projects,
// where AllowedTranscript accepts it.
func projectTranscript(t *testing.T, body string) string {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	dir := filepath.Join(cfgDir, "projects", "-w")
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// config merges summarizer into the store's config.json.
func (e *env) config(summarizer any) {
	p := filepath.Join(e.a.Store.Dir(), "config.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		json.Unmarshal(b, &cfg)
	}
	cfg["summarizer"] = summarizer
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// job writes a job file the way capture's fold does.
func (e *env) job() string { return e.jobWith(func(*Job) {}) }

func (e *env) jobWith(edit func(*Job)) string {
	dir := Dir(e.a.Store.Dir())
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "claude-code_session_s1.json")
	j := Job{ManifestID: e.id, SessionRef: "claude-code:session:s1", TranscriptPath: e.tr, StartedAt: e.start}
	edit(&j)
	b, _ := json.Marshal(j)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) stamp(tool, at string) {
	e.a.Update(e.id, 0, func(m *manifest.Manifest) error {
		manifest.StampState(m, tool)
		var st manifest.StateStamp
		json.Unmarshal(m.Ext[manifest.StateExtKey], &st)
		st.At = at
		m.Ext[manifest.StateExtKey], _ = json.Marshal(st)
		m.State.NextStep = "Recorded by " + tool
		return nil
	}, store.Op{Tool: "test"})
}

func (e *env) run() Result {
	e.t.Helper()
	r, err := RunJobs(context.Background(), e.a)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

const twoLines = `printf 'Summary: Added a chart.\nNext step: Add CSV export\n'`

func TestJobApplies(t *testing.T) {
	e := newEnv(t, twoLines)
	// A stamp from an earlier summarizer run never blocks a new one.
	e.stamp(Tool, "2026-11-06T11:00:00Z")
	p := e.job()
	if r := e.run(); r.Done != 1 || exists(p) {
		t.Fatalf("result %+v, job left %v", r, exists(p))
	}
	m, _ := e.a.Get(e.id)
	st, _ := manifest.StateStampOf(m)
	if m.State.Summary != "Added a chart." || m.State.NextStep != "Add CSV export" || st.RecordedBy != Tool {
		t.Errorf("state %+v, stamp %+v", m.State, st)
	}
	if m.Intent.Summary != "Household budget tracker as a single HTML page" || m.Intent.Source != "user" {
		t.Errorf("intent changed: %+v", m.Intent)
	}
	if rs, _ := e.a.Search("CSV export", indexOpts); len(rs) != 1 {
		t.Error("the index did not follow the update")
	}
}

func TestJobSkippedWhenModelRecorded(t *testing.T) {
	e := newEnv(t, twoLines)
	e.stamp("claude-code", e.start) // at startedAt counts
	p := e.job()
	if r := e.run(); r.Skipped != 1 || exists(p) || exists(e.ran) {
		t.Fatalf("result %+v, job left %v, summarizer ran %v", r, exists(p), exists(e.ran))
	}
	if m, _ := e.a.Get(e.id); m.State.NextStep != "Recorded by claude-code" {
		t.Errorf("model's state overwritten: %+v", m.State)
	}
}

func TestJobStampBeforeStartDoesNotSkip(t *testing.T) {
	e := newEnv(t, twoLines)
	e.stamp("claude-code", "2026-11-06T09:59:59Z")
	e.job()
	if r := e.run(); r.Done != 1 {
		t.Fatalf("result %+v", r)
	}
}

func TestJobFailures(t *testing.T) {
	for name, body := range map[string]string{
		"exit 1":      "exit 1",
		"timeout":     "sleep 10",
		"unparseable": "echo nothing useful",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, body)
			p := e.job()
			if r := e.run(); r.Failed != 1 || exists(p) || !exists(filepath.Join(filepath.Dir(p), "failed", filepath.Base(p))) {
				t.Fatalf("result %+v", r)
			}
			if m, _ := e.a.Get(e.id); m.State.Summary != "" {
				t.Errorf("state written: %+v", m.State)
			}
			log, _ := os.ReadFile(filepath.Join(e.a.Store.Dir(), "logs", "coal.log"))
			if !strings.Contains(string(log), "summarizer: job") || strings.Contains(string(log), "SECRET-ISH") {
				t.Errorf("log:\n%s", log)
			}
		})
	}
}

func TestJobsWaitWithoutConfig(t *testing.T) {
	e := newEnv(t, twoLines)
	e.config(nil)
	p := e.job()
	if r := e.run(); r != (Result{}) || !exists(p) {
		t.Fatalf("result %+v, job left %v", r, exists(p))
	}
}

func TestHookDrainIgnoresJobs(t *testing.T) {
	e := newEnv(t, twoLines)
	p := e.job()
	calls := 0
	if _, err := queue.Drain(e.a.Store.Dir(), func(*capture.Event) error { calls++; return nil }, true); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !exists(p) || exists(e.ran) {
		t.Errorf("queue drain touched the summarize job: calls %d", calls)
	}
}

var indexOpts = index.SearchOpts{}

func TestRunnerKick(t *testing.T) {
	e := newEnv(t, "sleep 0.3; "+twoLines)
	p := e.job()
	r := NewRunner(e.a)
	r.Kick(context.Background())
	r.Kick(context.Background()) // a second kick while running is a no-op
	deadline := time.Now().Add(5 * time.Second)
	for exists(p) || r.running.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the background run did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m, _ := e.a.Get(e.id); m.State.NextStep != "Add CSV export" {
		t.Errorf("state %+v", m.State)
	}
}

// setActivity sets state.lastActivityAt as a later session would.
func (e *env) setActivity(at string) {
	e.a.Update(e.id, 0, func(m *manifest.Manifest) error {
		m.State.LastActivityAt, m.State.NextStep = at, "From session 2"
		return nil
	}, store.Op{Tool: "test"})
}

// Fable review M11, R2: an older session's job must not overwrite a
// newer session's state, in either order.
func TestStaleJobSkipped(t *testing.T) {
	// Session 1's job waits while session 2 ends and sets state.
	e := newEnv(t, twoLines)
	p := e.jobWith(func(j *Job) { j.EndedAt = "2026-11-06T10:30:00Z" })
	e.setActivity("2026-11-06T12:00:00Z")
	if r := e.run(); r.Skipped != 1 || exists(p) || exists(e.ran) {
		t.Fatalf("stale job: %+v, summarizer ran %v", r, exists(e.ran))
	}
	if m, _ := e.a.Get(e.id); m.State.NextStep != "From session 2" {
		t.Errorf("newer state overwritten: %+v", m.State)
	}

	// The job runs before any later activity: it applies.
	e = newEnv(t, twoLines)
	e.setActivity("2026-11-06T10:30:00Z")
	e.jobWith(func(j *Job) { j.EndedAt = "2026-11-06T10:30:00Z" })
	if r := e.run(); r.Done != 1 {
		t.Fatalf("current job: %+v", r)
	}
	if m, _ := e.a.Get(e.id); m.State.NextStep != "Add CSV export" {
		t.Errorf("state %+v", m.State)
	}
}

func TestJobTranscriptMustBeAllowed(t *testing.T) {
	e := newEnv(t, twoLines)
	outside := write(t, user("/w", "a transcript outside the projects dir"))
	link := filepath.Join(filepath.Dir(e.tr), "link.jsonl")
	os.Symlink(outside, link)
	for _, tr := range []string{outside, link, filepath.Join(filepath.Dir(e.tr), "notes.txt")} {
		os.WriteFile(filepath.Join(filepath.Dir(e.tr), "notes.txt"), []byte("x"), 0o600)
		p := e.jobWith(func(j *Job) { j.TranscriptPath = tr })
		if r := e.run(); r.Failed != 1 || exists(p) || exists(e.ran) {
			t.Errorf("%s: %+v, summarizer ran %v", tr, r, exists(e.ran))
		}
	}
	if !AllowedTranscript(e.tr) {
		t.Error("a transcript under projects/ was refused")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(e.tr)))))
	if AllowedTranscript(e.tr) {
		t.Error("without CLAUDE_CONFIG_DIR only ~/.claude/projects counts")
	}
}
