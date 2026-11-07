package fold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/summarize"
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
	os.WriteFile(filepath.Join(sub, "export.ts"), []byte("export function csv() {}\n"), 0o644)
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) {
		e.SessionRef, e.Cwd, e.Path = capture.SessionRef("8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"), sub, filepath.Join(sub, "export.ts")
	})
	// Another file in the same session is not a second attach.
	os.WriteFile(filepath.Join(sub, "notes.md"), []byte("1\n"), 0o644)
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) {
		e.SessionRef, e.Cwd, e.Path = capture.SessionRef("8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"), sub, filepath.Join(sub, "notes.md")
	})
	sm, err := metrics.Summarize(v.a.Store.Dir(), time.Time{})
	if err != nil || sm.Counts[metrics.ManifestCreated] != 1 || sm.Counts[metrics.SessionAttached] != 1 || sm.Counts[metrics.ResumeActed] != 1 {
		t.Errorf("metrics: %+v %v", sm.Counts, err)
	}
	ms = v.all()
	if len(ms) != 1 || len(ms[0].Artifacts) != 3 || ms[0].Intent.Summary != m.Intent.Summary {
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
	v.a.Update(id, 0, func(m *manifest.Manifest) error {
		m.State.NextStep = "Add charts"
		manifest.StampState(m, "coal-mcp")
		return nil
	}, op("test"))
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

const sidB = "8d2a4f3b-6c5e-4f90-8b12-3c4d5e6f7a81"

// asB makes an event belong to the second recorded session.
func asB(e *capture.Event) { e.SessionRef = capture.SessionRef(sidB) }

func TestTwoSessionsBothExtract(t *testing.T) {
	v := setup(t)
	v.write("budget.html", "x")
	// A creates the manifest, then B starts on it, then A ends with an
	// extraction, then B ends. B must still get its own extraction.
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	v.send("session-start-resume.json", "SessionStart", nil)
	v.send("stop.json", "Stop", nil)
	v.send("session-end.json", "SessionEnd", nil)
	if m := v.all()[0]; m.State.NextStep != "add CSV export" {
		t.Fatalf("A's extraction: %+v", m.State)
	}
	v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { asB(e); v.write("budget.html", "y") })
	v.send("stop.json", "Stop", func(e *capture.Event) {
		asB(e)
		e.LastAssistantMessage = "Added CSV export.\n\nNext step: add charts"
	})
	v.send("session-end.json", "SessionEnd", asB)
	ms := v.all()
	if len(ms) != 1 || ms[0].State.NextStep != "add charts" || ms[0].State.Summary != "Added CSV export." {
		t.Errorf("B's extraction did not apply: %+v", ms[0].State)
	}
}

func TestUnanchoredSession(t *testing.T) {
	v := setup(t)
	home := os.Getenv("HOME")
	os.MkdirAll(filepath.Join(home, ".git"), 0o755)
	os.WriteFile(filepath.Join(home, "notes.md"), []byte("hi"), 0o644)
	for _, file := range []string{"session-start.json", "user-prompt-submit.json"} {
		v.send(file, "", func(e *capture.Event) { e.Cwd = home })
	}
	for range 2 {
		v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Cwd, e.Path = home, filepath.Join(home, "notes.md") })
	}
	if ms := v.all(); len(ms) != 0 {
		t.Errorf("captured %d manifests in ~", len(ms))
	}
	if _, err := os.Stat(filepath.Join(home, ".coal")); err == nil {
		t.Error("wrote ~/.coal")
	}
	log, _ := os.ReadFile(filepath.Join(v.a.Store.Dir(), "logs", "coal.log"))
	if strings.Count(string(log), "unanchored session in "+home+", not captured") != 1 {
		t.Errorf("log:\n%s", log)
	}
}

func TestOutsideProjectNotCaptured(t *testing.T) {
	v := setup(t)
	outside := filepath.Join(filepath.Dir(v.project), "elsewhere.html")
	os.WriteFile(outside, []byte("x"), 0o644)
	link := filepath.Join(v.project, "link.html")
	os.Symlink(outside, link)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	for _, p := range []string{outside, link} {
		v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Path = p })
	}
	if ms := v.all(); len(ms) != 0 {
		t.Errorf("files outside the project made %d manifests", len(ms))
	}
}

func TestPlaceholderUpgradedByAnySession(t *testing.T) {
	v := setup(t)
	v.write("budget.html", "x")
	v.send("session-start.json", "SessionStart", nil)
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	v.send("session-end.json", "SessionEnd", nil)
	if m := v.all()[0]; m.Intent.Summary != "Work in budget" {
		t.Fatalf("placeholder: %+v", m.Intent)
	}
	v.send("session-start-resume.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) { asB(e); e.Prompt = "Add a CSV export button to the budget page" })
	if m := v.all()[0]; m.Intent.Summary != "Add a CSV export button to the budget page" || m.Intent.Source != manifest.SourceUser {
		t.Errorf("not upgraded: %+v", m.Intent)
	}
}

func TestDotfilesHomeStillCaptures(t *testing.T) {
	v := setup(t)
	home := os.Getenv("HOME")
	os.MkdirAll(filepath.Join(home, ".git"), 0o755)
	plain := filepath.Join(home, "plain")
	os.MkdirAll(plain, 0o755)
	os.WriteFile(filepath.Join(plain, "page.html"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(home, "notes.md"), []byte("x"), 0o644)
	// A session in ~ itself captures nothing.
	v.send("session-start.json", "SessionStart", func(e *capture.Event) { e.Cwd = home })
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Cwd, e.Path = home, filepath.Join(home, "notes.md") })
	if ms := v.all(); len(ms) != 0 {
		t.Fatalf("captured in ~: %d", len(ms))
	}
	// A session in ~/plain, with no repo of its own, is rooted there.
	v.send("session-start-resume.json", "SessionStart", func(e *capture.Event) { e.Cwd = plain })
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) {
		asB(e)
		e.Cwd, e.Path = plain, filepath.Join(plain, "page.html")
	})
	ms := v.all()
	if len(ms) != 1 || len(ms[0].Contexts) != 1 || ms[0].Contexts[0].Key != plain {
		t.Fatalf("~/plain: %+v", ms)
	}
	if _, err := os.Stat(filepath.Join(plain, ".coal")); err != nil {
		t.Error("no marker in ~/plain")
	}
	if _, err := os.Stat(filepath.Join(home, ".coal")); err == nil {
		t.Error("wrote ~/.coal")
	}
}

func TestResumeRefreshesStart(t *testing.T) {
	v := setup(t)
	v.write("budget.html", "x")
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	id := v.all()[0].ID
	// Another session records state on purpose while this one is away.
	v.a.Update(id, 0, func(m *manifest.Manifest) error {
		m.State.NextStep = "Add charts"
		manifest.StampState(m, "coal-mcp")
		return nil
	}, op("test"))
	later := time.Now().Add(5 * time.Second).UTC().Format(time.RFC3339)
	v.send("session-start.json", "SessionStart", func(e *capture.Event) { e.Source, e.At = "resume", later })
	v.send("stop.json", "Stop", nil)
	v.send("session-end.json", "SessionEnd", nil)
	if m, _ := v.a.Get(id); m.State.NextStep != "add CSV export" {
		t.Errorf("resumed session's extraction was suppressed: %+v", m.State)
	}
}

func writeConfig(t *testing.T, v *env, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(v.a.Store.Dir(), "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureExtensions(t *testing.T) {
	v := setup(t)
	v.send("session-start.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	for _, name := range []string{"app.js", "style.css", "Makefile"} {
		v.write(name, "x")
		v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Path = filepath.Join(v.project, name) })
	}
	if ms := v.all(); len(ms) != 0 {
		t.Fatalf("default extensions captured %d", len(ms))
	}
	v.write("README.MD", "x")
	v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Path = filepath.Join(v.project, "README.MD") })
	if ms := v.all(); len(ms) != 1 || len(ms[0].Artifacts) != 1 {
		t.Fatalf("upper-case .MD not captured")
	}
	writeConfig(t, v, `{"captureExtensions": ["JS", ".css"]}`)
	for _, name := range []string{"app.js", "style.css", "page.html"} {
		v.write(name, "y")
		v.send("post-tool-use-write.json", "PostToolUse", func(e *capture.Event) { e.Path = filepath.Join(v.project, name) })
	}
	if m := v.all()[0]; len(m.Artifacts) != 3 {
		t.Errorf("configured extensions: %d artifacts", len(m.Artifacts))
	}
}

func TestMarkersNone(t *testing.T) {
	v := setup(t)
	writeConfig(t, v, `{"markers": "none"}`)
	v.session1()
	if _, err := os.Stat(filepath.Join(v.project, ".coal")); err == nil {
		t.Fatal("wrote .coal with markers none")
	}
	m := v.all()[0]
	// Matching still works through contexts, and an existing marker is
	// still read.
	sub := filepath.Join(v.project, "src")
	os.MkdirAll(sub, 0o755)
	if got := match(v.a, sub); got == nil || got.ID != m.ID {
		t.Errorf("context match with markers none: %v", got)
	}
	other := manifest.New("Other work", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	v.a.Create(other, op("test"))
	os.WriteFile(filepath.Join(v.project, ".coal"), []byte(`{"manifest":"`+other.ID+`"}`), 0o644)
	if got := match(v.a, sub); got == nil || got.ID != other.ID {
		t.Errorf("existing marker ignored with markers none")
	}
	if LoadConfig(t.TempDir()).Markers != MarkersProject {
		t.Error("default is not project")
	}
}

func TestSummarizeJob(t *testing.T) {
	v := setup(t)
	v.session1()
	if files, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json")); len(files) != 0 {
		t.Fatalf("job queued without a summarizer: %v", files)
	}
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	v.send("session-start-resume.json", "SessionStart", nil)
	v.send("session-end.json", "SessionEnd", asB)
	files, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json"))
	if len(files) != 1 || !strings.Contains(files[0], "claude-code_session_"+sidB) {
		t.Fatalf("jobs: %v", files)
	}
	var job summarize.Job
	b, _ := os.ReadFile(files[0])
	json.Unmarshal(b, &job)
	if job.ManifestID != v.all()[0].ID || job.SessionRef != capture.SessionRef(sidB) || job.StartedAt == "" || !strings.HasSuffix(job.TranscriptPath, ".jsonl") {
		t.Errorf("job: %+v", job)
	}
	// No job when the summarizer is off or the model recorded state.
	os.Remove(files[0])
	writeConfig(t, v, `{"summarizer": null}`)
	v.send("session-end.json", "SessionEnd", asB)
	if files, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json")); len(files) != 0 {
		t.Errorf("job with the summarizer off: %v", files)
	}
}

// transcript writes a Claude Code transcript with the given lines.
func transcript(t *testing.T, lines ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		j, _ := json.Marshal(l)
		b.Write(append(j, '\n'))
	}
	p := filepath.Join(t.TempDir(), "session.jsonl")
	os.WriteFile(p, []byte(b.String()), 0o600)
	return p
}

func tline(typ string, content any) map[string]any {
	return map[string]any{"type": typ, "sessionId": "s", "timestamp": "2026-11-06T10:00:00Z", "message": map[string]any{"id": typ + "1", "content": content}}
}

func toolUse(path string) map[string]any {
	return map[string]any{"type": "tool_use", "name": "Write", "input": map[string]any{"file_path": path}}
}

func TestTranscriptExtraction(t *testing.T) {
	v := setup(t)
	var blocks []any
	for _, n := range []string{"budget.html", "budget.html", "a.md", "b.md", "c.py", "d.ts", "e.tsx"} {
		blocks = append(blocks, toolUse(filepath.Join(v.project, n)))
	}
	blocks = append(blocks, map[string]any{"type": "text", "text": "Added a CSV export button.\n\nNext step: add charts"})
	path := transcript(t, tline("user", "Build me a household budget tracker as a single HTML page"), tline("assistant", blocks))
	withPath := func(e *capture.Event) { e.TranscriptPath = path }
	v.write("budget.html", "x")
	v.send("session-start.json", "SessionStart", withPath)
	v.send("user-prompt-submit.json", "UserPromptSubmit", withPath)
	v.send("post-tool-use-write.json", "PostToolUse", withPath)
	v.send("stop.json", "Stop", withPath)
	v.send("session-end.json", "SessionEnd", withPath)
	m := v.all()[0]
	want := "Added a CSV export button. Files touched: budget.html, a.md, b.md, c.py, d.ts"
	if m.State.Summary != want || m.State.NextStep != "add charts" {
		t.Errorf("state: %q / %q\nwant %q", m.State.Summary, m.State.NextStep, want)
	}
	if m.Intent.Summary != "Build me a household budget tracker as a single HTML page" {
		t.Errorf("intent changed: %q", m.Intent.Summary)
	}
}

func TestTranscriptUserFallback(t *testing.T) {
	v := setup(t)
	path := transcript(t, tline("user", "Please make the totals row bold and right aligned"))
	v.write("budget.html", "x")
	v.send("session-start.json", "SessionStart", func(e *capture.Event) { e.TranscriptPath = path })
	v.send("user-prompt-submit.json", "UserPromptSubmit", nil)
	v.send("post-tool-use-write.json", "PostToolUse", nil)
	// No Stop: the session has no last assistant message.
	v.send("session-end.json", "SessionEnd", func(e *capture.Event) { e.TranscriptPath = path })
	if m := v.all()[0]; m.State.Summary != "Please make the totals row bold and right aligned" || m.State.NextStep != "" {
		t.Errorf("state: %+v", m.State)
	}
}

func TestLateSummarizerStampDoesNotHideNextSession(t *testing.T) {
	v := setup(t)
	v.write("budget.html", "x")
	// Session A runs and ends with its own extraction.
	v.session1()
	id := v.all()[0].ID
	// Session B starts. Then the summarizer finishes A's job and stamps
	// the state, after B's start.
	v.send("session-start-resume.json", "SessionStart", nil)
	v.a.Update(id, 0, func(m *manifest.Manifest) error {
		m.State.Summary = "Summarizer's account of session A."
		manifest.StampState(m, summarize.Tool)
		return nil
	}, op("test"))
	v.send("stop.json", "Stop", func(e *capture.Event) {
		asB(e)
		e.LastAssistantMessage = "Added CSV export.\n\nNext step: add charts"
	})
	v.send("session-end.json", "SessionEnd", asB)
	m, _ := v.a.Get(id)
	if m.State.Summary != "Added CSV export." || m.State.NextStep != "add charts" {
		t.Errorf("B's extraction was hidden by the summarizer's stamp: %+v", m.State)
	}
}
