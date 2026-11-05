package fold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
)

type env struct {
	t       *testing.T
	a       *app.App
	project string
}

func setup(t *testing.T) *env {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	project := filepath.Join(base, "projects", "budget")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(project, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:dave/budget.git\n"), 0o644)
	a, err := app.Open(filepath.Join(base, "coal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return &env{t, a, project}
}

// send translates a recorded payload with the placeholder path
// rewritten, overrides fields, and applies it.
func (v *env) send(file, hook string, edit func(*capture.Event)) {
	v.t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "claude-code", file))
	if err != nil {
		v.t.Fatal(err)
	}
	b = []byte(strings.ReplaceAll(string(b), "/PROJECT/budget", v.project))
	e, err := capture.FromClaudeCode(hook, b)
	if err != nil || e == nil {
		v.t.Fatalf("%s: %v %v", file, e, err)
	}
	if edit != nil {
		edit(e)
	}
	if err := Apply(v.a, e); err != nil {
		v.t.Fatalf("%s: %v", file, err)
	}
}

func (v *env) write(name, body string) {
	v.t.Helper()
	if err := os.WriteFile(filepath.Join(v.project, name), []byte(body), 0o644); err != nil {
		v.t.Fatal(err)
	}
}

func (v *env) all() []*manifest.Manifest {
	ms, errs := v.a.Store.List()
	if len(errs) > 0 {
		v.t.Fatal(errs)
	}
	return ms
}

func (v *env) session1() {
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.write("budget.html", "<h1>Budget</h1>")
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	v.send("stop.json", "Stop", nil)
	v.send("session-end.json", "SessionEnd", nil)
}

func TestRecordedSession(t *testing.T) {
	v := setup(t)
	v.session1()
	ms := v.all()
	if len(ms) != 1 {
		t.Fatalf("%d manifests", len(ms))
	}
	m := ms[0]
	if m.Intent.Summary != "Build me a household budget tracker as a single HTML page" || m.Intent.Source != manifest.SourceUser {
		t.Errorf("intent: %+v", m.Intent)
	}
	if len(m.Artifacts) != 1 || len(m.Artifacts[0].Versions) != 1 || m.Artifacts[0].Title != "budget.html" {
		t.Fatalf("artifacts: %+v", m.Artifacts)
	}
	p := m.Artifacts[0].Producer
	if p.Tool != "claude-code" || p.Model != "claude-opus-5-5" || !strings.HasPrefix(p.SessionRef, "claude-code:session:") || p.CapturedBy != CapturedBy {
		t.Errorf("producer: %+v", p)
	}
	if m.State.NextStep != "add CSV export" || !strings.HasPrefix(m.State.Summary, "I built budget.html") {
		t.Errorf("state: %+v", m.State)
	}
	if len(m.Contexts) != 2 || m.Contexts[0].Key != v.project || m.Contexts[1].Key != "github.com/dave/budget" {
		t.Errorf("contexts: %+v", m.Contexts)
	}
	b, _ := os.ReadFile(filepath.Join(v.project, ".coal"))
	if !strings.Contains(string(b), m.ID) {
		t.Errorf("marker: %s", b)
	}
	ex, _ := os.ReadFile(filepath.Join(v.project, ".git", "info", "exclude"))
	if !strings.Contains(string(ex), ".coal") {
		t.Errorf("exclude: %q", ex)
	}

	// Editing the same file twice more gives three versions.
	v.write("budget.html", "<h1>Budget v2</h1>")
	v.send("post-tool-use-edit.json", "PostToolUse", nil)
	v.write("budget.html", "<h1>Budget v3</h1>")
	v.send("post-tool-use-edit.json", "PostToolUse", nil)
	m, _ = v.a.Get(m.ID)
	if len(m.Artifacts) != 1 || len(m.Artifacts[0].Versions) != 3 {
		t.Errorf("after edits: %d artifacts, %+v", len(m.Artifacts), m.Artifacts)
	}

	// A second session from a subdirectory attaches to the same manifest.
	sub := filepath.Join(v.project, "src")
	os.MkdirAll(sub, 0o755)
	v.send("session-start-resume.json", "SessionStart", func(e *capture.Event) { e.Cwd = sub })
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) {
		e.SessionRef, e.Cwd, e.Prompt = capture.SessionRef("8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"), sub, "Now add a CSV export button please"
	})
	os.WriteFile(filepath.Join(sub, "export.js"), []byte("export function csv() {}\n"), 0o644)
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) {
		e.SessionRef, e.Cwd, e.Path = capture.SessionRef("8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"), sub, filepath.Join(sub, "export.js")
	})
	ms = v.all()
	if len(ms) != 1 || len(ms[0].Artifacts) != 2 || ms[0].Intent.Summary != m.Intent.Summary {
		t.Errorf("second session: %d manifests, %+v", len(ms), ms[0].Artifacts)
	}
}

func TestModelRecordedStateWins(t *testing.T) {
	v := setup(t)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.write("budget.html", "x")
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	id := v.all()[0].ID
	// What coal_update_state would do mid-session.
	v.a.Update(id, 0, func(m *manifest.Manifest) error { m.State.NextStep = "Add charts"; return nil }, op("test"))
	v.send("stop.json", "Stop", nil)
	v.send("session-end.json", "SessionEnd", nil)
	m, _ := v.a.Get(id)
	if m.State.NextStep != "Add charts" || m.State.Summary != "" {
		t.Errorf("state: %+v", m.State)
	}
}

func TestPlaceholderAndSkips(t *testing.T) {
	v := setup(t)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) { e.Prompt = "continue" })
	// Files in .git, in the data dir, missing or too big are not captured.
	big := filepath.Join(v.project, "big.bin")
	os.WriteFile(big, make([]byte, MaxFile+1), 0o644)
	for _, p := range []string{filepath.Join(v.project, ".git", "config"), filepath.Join(v.a.Store.Dir(), "config.json"), filepath.Join(v.project, "gone.html"), big} {
		v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Path = p })
	}
	if ms := v.all(); len(ms) != 0 {
		t.Fatalf("skipped files created %d manifests", len(ms))
	}
	v.write("budget.html", "x")
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	m := v.all()[0]
	if m.Intent.Summary != "Work in budget" || m.Intent.Source != manifest.SourceModel {
		t.Errorf("placeholder: %+v", m.Intent)
	}
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	m, _ = v.a.Get(m.ID)
	if m.Intent.Source != manifest.SourceUser || !strings.Contains(m.Intent.Summary, "budget tracker") {
		t.Errorf("upgrade: %+v", m.Intent)
	}
}

func TestNoFileNoManifest(t *testing.T) {
	v := setup(t)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.send("stop.json", "Stop", nil)
	v.send("session-end.json", "SessionEnd", nil)
	if ms := v.all(); len(ms) != 0 {
		t.Errorf("%d manifests", len(ms))
	}
}

func TestStaleSessionClosed(t *testing.T) {
	v := setup(t)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.write("budget.html", "x")
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	v.send("stop.json", "Stop", nil)
	// No SessionEnd: the session was killed. Age its scratch file.
	ref := capture.SessionRef("7c1f3e2a-5b4d-4e8f-9a01-2b3c4d5e6f70")
	var s Session
	v.a.Store.ReadSession(ref, &s)
	s.StartedAt = time.Now().Add(-13 * time.Hour).UTC().Format(time.RFC3339)
	v.a.Store.WriteSession(ref, &s)
	old := time.Now().Add(-13 * time.Hour)
	os.Chtimes(v.a.Store.SessionPath(ref), old, old)

	v.send("session-start-resume.json", "SessionStart", nil)
	m := v.all()[0]
	if m.State.NextStep != "add CSV export" {
		t.Errorf("stale close-out: %+v", m.State)
	}
	v.a.Store.ReadSession(ref, &s)
	if s.EndedAt == "" {
		t.Error("endedAt not set")
	}
	var s2 Session
	v.a.Store.ReadSession(capture.SessionRef("8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"), &s2)
	if s2.ManifestID != m.ID {
		t.Errorf("new session not matched: %+v", s2)
	}
}
