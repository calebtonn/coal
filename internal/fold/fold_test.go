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
	return setupData(t, nil)
}

// setupData is setup with the data dir that dataDir picks from the temp
// base and the project; nil puts it at <base>/coal, outside the project.
func setupData(t *testing.T, dataDir func(base, project string) string) *env {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	project := filepath.Join(base, "projects", "budget")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(project, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:dave/budget.git\n"), 0o644)
	data := filepath.Join(base, "coal")
	if dataDir != nil {
		data = dataDir(base, project)
	}
	a, err := app.Open(data)
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

const sidC = "0f3b5a7c-9d1e-4a2b-8c3d-4e5f6a7b8c92"

// asC makes an event belong to a third session.
func asC(e *capture.Event) { e.SessionRef = capture.SessionRef(sidC) }

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
	// The resumed stretch writes a file; one that only talks would keep
	// the recorded state (#20, TestResumedStretchKeepsRecordedState).
	v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { e.At = later })
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
	tp := transcript(t, tline("user", "Add a CSV export button to the page"), tline("assistant", []any{map[string]any{"type": "text", "text": "Added the button."}}))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	// A transcript outside Claude Code's projects dir gets no job.
	v.send("session-start-resume.json", "SessionStart", nil)
	v.send("session-end.json", "SessionEnd", asB)
	if files, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json")); len(files) != 0 {
		t.Fatalf("job for a transcript outside ~/.claude/projects: %v", files)
	}
	v.send("session-end.json", "SessionEnd", withTP)
	files, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json"))
	if len(files) != 1 || !strings.Contains(files[0], "claude-code_session_"+sidB) {
		t.Fatalf("jobs: %v", files)
	}
	var job summarize.Job
	b, _ := os.ReadFile(files[0])
	json.Unmarshal(b, &job)
	if job.ManifestID != v.all()[0].ID || job.SessionRef != capture.SessionRef(sidB) || job.StartedAt == "" || job.TranscriptPath != tp || job.EndedAt == "" || job.EndedAt < job.StartedAt {
		t.Errorf("job: %+v", job)
	}
	// No job when the summarizer is off or the model recorded state.
	os.Remove(files[0])
	writeConfig(t, v, `{"summarizer": null}`)
	v.send("session-end.json", "SessionEnd", withTP)
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
	// Claude Code keeps transcripts under ~/.claude/projects, the only
	// place capture reads them from.
	dir := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-budget")
	os.MkdirAll(dir, 0o700)
	f, _ := os.CreateTemp(dir, "session-*.jsonl")
	f.WriteString(b.String())
	f.Close()
	return f.Name()
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

func TestTranscriptOutsideClaudeIsNotRead(t *testing.T) {
	v := setup(t)
	// A payload that names some other file as its transcript.
	other := filepath.Join(t.TempDir(), "secrets.jsonl")
	os.WriteFile(other, []byte(`{"type":"user","message":{"content":"the database password is hunter2 and more"}}`+"\n"), 0o600)
	v.write("budget.html", "x")
	with := func(e *capture.Event) { e.TranscriptPath = other }
	v.send("session-start.json", "SessionStart", with)
	v.send("user-prompt-submit.json", "UserPromptSubmit", with)
	v.send("post-tool-use-write.json", "PostToolUse", with)
	v.send("stop.json", "Stop", with)
	v.send("session-end.json", "SessionEnd", with)
	m := v.all()[0]
	if strings.Contains(m.State.Summary, "hunter2") || m.State.NextStep != "add CSV export" {
		t.Errorf("read a transcript outside ~/.claude/projects: %+v", m.State)
	}
}

func TestTrivialSessionKeepsSummary(t *testing.T) {
	v := setup(t)
	v.session1()
	want := v.all()[0].State
	// Session B: "hello", then exit. No file, no reply.
	tp := transcript(t, tline("user", "hello there, just checking in"))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", nil)
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) { withTP(e); e.Prompt = "hello there, just checking in" })
	v.send("session-end.json", "SessionEnd", withTP)
	if got := v.all()[0].State; got.Summary != want.Summary || got.NextStep != want.NextStep {
		t.Errorf("a trivial session changed state:\n got %+v\nwant %+v", got, want)
	}
}

func TestSummarizerResultSurvivesReappliedEnd(t *testing.T) {
	v := setup(t)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	tp := transcript(t, tline("user", "Build the budget page"), tline("assistant", []any{map[string]any{"type": "text", "text": "Built it.\n\nNext step: add CSV export"}}))
	withTP := func(e *capture.Event) { e.TranscriptPath = tp }
	end := time.Now().UTC().Format(time.RFC3339)
	atEnd := func(e *capture.Event) { withTP(e); e.At = end }
	v.write("budget.html", "x")
	v.send("session-start.json", "SessionStart", withTP)
	v.send("user-prompt-submit.json", "UserPromptSubmit", withTP)
	v.send("post-tool-use-write.json", "PostToolUse", withTP)
	v.send("session-end.json", "SessionEnd", atEnd)
	jobs := func() []string {
		f, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json"))
		return f
	}
	if len(jobs()) != 1 {
		t.Fatalf("jobs: %v", jobs())
	}
	// The summarizer runs the job, then deletes it, stamping after the end.
	id := v.all()[0].ID
	os.Remove(jobs()[0])
	later, _ := json.Marshal(manifest.StateStamp{RecordedBy: summarize.Tool, At: time.Now().Add(5 * time.Second).UTC().Format(time.RFC3339)})
	v.a.Update(id, 0, func(m *manifest.Manifest) error {
		m.State.Summary, m.State.NextStep = "The summarizer's account.", "Wire up CSV export"
		if m.Ext == nil {
			m.Ext = map[string]json.RawMessage{}
		}
		m.Ext[manifest.StateExtKey] = later
		return nil
	}, op("test"))
	// The same sessionEnd is applied again, say from queue/failed.
	v.send("session-end.json", "SessionEnd", atEnd)
	m, _ := v.a.Get(id)
	if m.State.Summary != "The summarizer's account." || m.State.NextStep != "Wire up CSV export" {
		t.Errorf("re-applied sessionEnd undid the summarizer: %+v", m.State)
	}
	if len(jobs()) != 0 {
		t.Errorf("re-queued a job: %v", jobs())
	}
}

// The resume offer Claude gave in the chat-only session of #20.
const resumeOffer = "Want me to pick up where you left off and add CSV export and import of transactions to `budget.html`, so your data can be backed up and moved between browsers?"

// stampAt writes state as recordedBy would, with the stamp's time set to
// at, as if it was recorded then.
func stampAt(v *env, id, recordedBy, at, summary, next string) {
	v.t.Helper()
	b, _ := json.Marshal(manifest.StateStamp{RecordedBy: recordedBy, At: at})
	if _, err := v.a.Update(id, 0, func(m *manifest.Manifest) error {
		m.State.Summary, m.State.NextStep = summary, next
		if m.Ext == nil {
			m.Ext = map[string]json.RawMessage{}
		}
		m.Ext[manifest.StateExtKey] = b
		return nil
	}, op("test")); err != nil {
		v.t.Fatal(err)
	}
}

// chatOnlySession runs session B in the project: a prompt, one reply,
// no file written, then the end. It returns B's end time.
func (v *env) chatOnlySession(reply string) string {
	v.t.Helper()
	tp := transcript(v.t, tline("user", "Hi. I'm back in this directory. What should I do?"), tline("assistant", []any{map[string]any{"type": "text", "text": reply}}))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", withTP)
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) { withTP(e); e.Prompt = "Hi. I'm back in this directory. What should I do?" })
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = reply })
	end := time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)
	v.send("session-end.json", "SessionEnd", func(e *capture.Event) { withTP(e); e.At = end })
	return end
}

func (v *env) jobs() []string {
	f, _ := filepath.Glob(filepath.Join(summarize.Dir(v.a.Store.Dir()), "*.json"))
	return f
}

// hourAgo is a stamp time from an earlier session.
func hourAgo() string { return time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) }

const (
	modelSummary = "Built budget.html with income and expense tables and a running total."
	modelNext    = "Add CSV export and import"
)

// #20: a later session that only talks keeps the state the model
// recorded in an earlier one, and queues no summarize job for it.
func TestChatOnlySessionKeepsRecordedState(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	end := v.chatOnlySession(resumeOffer)
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("a chat-only session replaced recorded state: %+v", m.State)
	}
	if m.State.LastActivityAt < end {
		t.Errorf("lastActivityAt %q did not move to the session end %q", m.State.LastActivityAt, end)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job for a chat-only session: %v", j)
	}
}

// #20: extraction still fills a field the recorded state left empty.
func TestChatOnlySessionFillsEmptyFields(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, "")
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	v.chatOnlySession(resumeOffer + "\n\nNext step: add CSV export")
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != "add CSV export" {
		t.Errorf("state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job for a chat-only session: %v", j)
	}
}

// A later session that writes a file still updates recorded state by
// extraction, whether or not the file's type is captured (#32), and
// whether the write reached capture through a hook or only shows in the
// transcript.
func TestWritingSessionReplacesRecordedState(t *testing.T) {
	reply := "Added CSV export.\n\nNext step: add charts"
	for _, tc := range []struct {
		name  string
		write func(v *env, tp *string)
	}{
		{"captured file", func(v *env, _ *string) {
			v.write("budget.html", "<h1>Budget v2</h1>")
			v.send("post-tool-use-edit.json", "PostToolUse", asB)
		}},
		{"file type not captured", func(v *env, _ *string) {
			v.write("main.go", "package main\n")
			v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { asB(e); e.Path = filepath.Join(v.project, "main.go") })
		}},
		{"write only in the transcript", func(v *env, tp *string) {
			*tp = transcript(v.t, stamped(tline("user", "Add CSV export to the budget page"), time.Now()),
				stamped(tline("assistant", []any{toolUse(filepath.Join(v.project, "main.go")), map[string]any{"type": "text", "text": reply}}), time.Now()))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := setup(t)
			v.session1()
			id := v.all()[0].ID
			stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
			var tp string
			withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
			v.send("session-start-resume.json", "SessionStart", asB)
			tc.write(v, &tp)
			v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = reply })
			v.send("session-end.json", "SessionEnd", withTP)
			m, _ := v.a.Get(id)
			if !strings.HasPrefix(m.State.Summary, "Added CSV export.") || m.State.NextStep != "add charts" {
				t.Errorf("a session that wrote a file did not update state: %+v", m.State)
			}
		})
	}
}

// With no stamp, a chat-only session's extraction applies as before.
func TestChatOnlySessionWithoutStamp(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	v.chatOnlySession(resumeOffer)
	m, _ := v.a.Get(id)
	if m.State.Summary != resumeOffer || m.State.NextStep != "add CSV export" {
		t.Errorf("state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 1 {
		t.Errorf("jobs: %v", j)
	}
}

// A summarizer stamp does not protect state from a chat-only session.
func TestChatOnlySessionAfterSummarizerStamp(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, summarize.Tool, hourAgo(), "The summarizer's account.", "Wire up CSV export")
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	v.chatOnlySession(resumeOffer)
	m, _ := v.a.Get(id)
	if m.State.Summary != resumeOffer || m.State.NextStep != "Wire up CSV export" {
		t.Errorf("state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 1 {
		t.Errorf("jobs: %v", j)
	}
}

// #20 as the code applies it: protection follows the manifest's state
// stamp, not who wrote the current text. After a model stamp, a session
// that writes a file replaces the state by extraction, and a later
// session that only talks keeps that extraction and queues no job.
func TestChatOnlyAfterWritingSessionKeepsExtractedState(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)

	reply := "Added CSV export.\n\nNext step: add charts"
	v.send("session-start-resume.json", "SessionStart", asB)
	v.write("budget.html", "<h1>Budget v2</h1>")
	v.send("post-tool-use-edit.json", "PostToolUse", asB)
	v.send("stop.json", "Stop", func(e *capture.Event) { asB(e); e.LastAssistantMessage = reply })
	v.send("session-end.json", "SessionEnd", asB)
	m, _ := v.a.Get(id)
	if !strings.HasPrefix(m.State.Summary, "Added CSV export.") || m.State.NextStep != "add charts" {
		t.Fatalf("the writing session did not update state: %+v", m.State)
	}
	jobsAfterB := len(v.jobs())

	tp := transcript(t, tline("user", "Hi. I'm back in this directory. What should I do?"), tline("assistant", []any{map[string]any{"type": "text", "text": resumeOffer}}))
	withTP := func(e *capture.Event) { asC(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", withTP)
	v.send("user-prompt-submit.json", "UserPromptSubmit", func(e *capture.Event) { withTP(e); e.Prompt = "Hi. I'm back in this directory. What should I do?" })
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = resumeOffer })
	v.send("session-end.json", "SessionEnd", withTP)

	m, _ = v.a.Get(id)
	if !strings.HasPrefix(m.State.Summary, "Added CSV export.") || m.State.NextStep != "add charts" {
		t.Errorf("a chat-only session replaced the writing session's extraction: %+v", m.State)
	}
	if n := len(v.jobs()); n != jobsAfterB {
		t.Errorf("the chat-only session queued a summarize job: %d jobs, was %d", n, jobsAfterB)
	}
}

// stamped sets the line's transcript timestamp.
func stamped(l map[string]any, at time.Time) map[string]any {
	l["timestamp"] = at.UTC().Format(time.RFC3339Nano)
	return l
}

// The transcript of a resumed session still holds the writes of the
// stretch before the resume. They are not writes of the resumed
// stretch, which only talks, so recorded state is kept (#20).
func TestResumedTranscriptOldWriteIsNotAWrite(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	now := time.Now()
	tp := transcript(t,
		stamped(tline("assistant", []any{toolUse(filepath.Join(v.project, "budget.html"))}), now.Add(-2*time.Hour)),
		stamped(tline("user", "Hi. I'm back in this directory. What should I do?"), now),
		stamped(tline("assistant", []any{map[string]any{"type": "text", "text": resumeOffer}}), now))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", func(e *capture.Event) { withTP(e); e.Source = "resume" })
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = resumeOffer })
	v.send("session-end.json", "SessionEnd", withTP)
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("a resumed chat-only stretch replaced recorded state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job for a chat-only stretch: %v", j)
	}
}

// A resumed stretch of the same session that only talks keeps state
// the model recorded in the stretch before, even though that earlier
// stretch wrote a file (#20).
func TestResumedStretchKeepsRecordedState(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	now := time.Now()
	stampAt(v, id, "claude-code", now.UTC().Format(time.RFC3339), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	resumed := now.Add(2 * time.Second).UTC().Format(time.RFC3339)
	end := now.Add(3 * time.Second).UTC().Format(time.RFC3339)
	v.send("session-start.json", "SessionStart", func(e *capture.Event) { e.Source, e.At = "resume", resumed })
	v.send("stop.json", "Stop", func(e *capture.Event) { e.LastAssistantMessage, e.At = resumeOffer, end })
	v.send("session-end.json", "SessionEnd", func(e *capture.Event) { e.At = end })
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("a resumed chat-only stretch replaced recorded state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job for a chat-only stretch: %v", j)
	}
}

// Writes outside the project or in .git, such as Claude Code's plan
// files under ~/.claude, do not make a session one that wrote a file
// (#20). The data dir here is outside the project, so its config.json
// fails the root check; TestWritesInDataDirInsideProjectDoNotCount
// covers the data-dir exclusion itself.
func TestWritesOutsideProjectDoNotCount(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	outside := filepath.Join(filepath.Dir(v.project), "other.html")
	os.WriteFile(outside, []byte("x"), 0o644)
	plan := filepath.Join(os.Getenv("HOME"), ".claude", "plans", "plan.md")
	os.MkdirAll(filepath.Dir(plan), 0o700)
	os.WriteFile(plan, []byte("x"), 0o644)
	tp := transcript(t,
		stamped(tline("user", "Hi. I'm back in this directory. What should I do?"), time.Now()),
		stamped(tline("assistant", []any{toolUse(plan), map[string]any{"type": "text", "text": resumeOffer}}), time.Now()))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", withTP)
	for _, p := range []string{outside, plan, filepath.Join(v.project, ".git", "config"), filepath.Join(v.a.Store.Dir(), "config.json")} {
		v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { withTP(e); e.Path = p })
	}
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = resumeOffer })
	v.send("session-end.json", "SessionEnd", withTP)
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("writes outside the project replaced recorded state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job: %v", j)
	}
}

// A write the user rejected at the permission prompt, or an Edit that
// failed, shows in the transcript as a tool_use whose tool_result has
// is_error set. It wrote nothing, so a stretch whose only write is that
// one is chat-only and keeps the recorded state (#20).
func TestRejectedTranscriptWriteIsNotAWrite(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	now := time.Now()
	write := toolUse(filepath.Join(v.project, "budget.html"))
	write["id"] = "toolu_rejected"
	rejected := map[string]any{"type": "tool_result", "tool_use_id": "toolu_rejected", "is_error": true,
		"content": "The user doesn't want to proceed with this tool use."}
	tp := transcript(t,
		stamped(tline("user", "Add CSV export to the budget page"), now),
		stamped(tline("assistant", []any{write}), now),
		stamped(tline("user", []any{rejected}), now),
		stamped(tline("assistant", []any{map[string]any{"type": "text", "text": resumeOffer}}), now))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", withTP)
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = resumeOffer })
	v.send("session-end.json", "SessionEnd", withTP)
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("a rejected write replaced recorded state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job for a chat-only stretch: %v", j)
	}
}

// A project reached through a symlink still counts a write to a file
// that was removed before the fold, as it does with the real path (#20).
func TestRemovedWriteThroughSymlinkCounts(t *testing.T) {
	v := setup(t)
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	link := filepath.Join(filepath.Dir(filepath.Dir(v.project)), "linkproj")
	if err := os.Symlink(v.project, link); err != nil {
		t.Fatal(err)
	}
	reply := "Tried a scratch script.\n\nNext step: add charts"
	inLink := func(e *capture.Event) { asB(e); e.Cwd = link }
	v.send("session-start-resume.json", "SessionStart", inLink)
	v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { inLink(e); e.Path = filepath.Join(link, "scratch.py") })
	v.send("stop.json", "Stop", func(e *capture.Event) { inLink(e); e.LastAssistantMessage = reply })
	v.send("session-end.json", "SessionEnd", inLink)
	m, _ := v.a.Get(id)
	if m.State.NextStep != "add charts" {
		t.Errorf("a removed write through a symlinked project did not count: %+v", m.State)
	}
}

// A data dir inside the project, as with COAL_HOME=$PWD/.sandbox/coal,
// is still not the project: writes under it, by hook or in the
// transcript, do not make a session one that wrote a file (#20).
func TestWritesInDataDirInsideProjectDoNotCount(t *testing.T) {
	v := setupData(t, func(_, project string) string { return filepath.Join(project, ".sandbox", "coal") })
	v.session1()
	id := v.all()[0].ID
	stampAt(v, id, "claude-code", hourAgo(), modelSummary, modelNext)
	writeConfig(t, v, `{"summarizer": {"command": ["true"]}}`)
	inData := filepath.Join(v.a.Store.Dir(), "config.json")
	tp := transcript(t,
		stamped(tline("user", "Hi. I'm back in this directory. What should I do?"), time.Now()),
		stamped(tline("assistant", []any{toolUse(inData), map[string]any{"type": "text", "text": resumeOffer}}), time.Now()))
	withTP := func(e *capture.Event) { asB(e); e.TranscriptPath = tp }
	v.send("session-start-resume.json", "SessionStart", withTP)
	v.send("post-tool-use-edit.json", "PostToolUse", func(e *capture.Event) { withTP(e); e.Path = inData })
	v.send("stop.json", "Stop", func(e *capture.Event) { withTP(e); e.LastAssistantMessage = resumeOffer })
	v.send("session-end.json", "SessionEnd", withTP)
	m, _ := v.a.Get(id)
	if m.State.Summary != modelSummary || m.State.NextStep != modelNext {
		t.Errorf("writes in the data dir replaced recorded state: %+v", m.State)
	}
	if j := v.jobs(); len(j) != 0 {
		t.Errorf("queued a summarize job: %v", j)
	}
}

// A session whose cwd is gone by the time of the fold, reached through
// a symlinked parent, still counts its writes in the project: the root
// is resolved the same way as the written path (#20). The cwd may be a
// subdirectory removed while the repo stays, or the whole project may
// be removed, as with a scratch project under /tmp on macOS.
func TestMissingCwdThroughSymlinkCounts(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	a, err := app.Open(filepath.Join(base, "coal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	proj := filepath.Join(base, "realp", "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(base, "lnkp")
	if err := os.Symlink(filepath.Join(base, "realp"), lnk); err != nil {
		t.Fatal(err)
	}
	s := &Session{Cwd: filepath.Join(lnk, "proj", "sub")}
	for _, p := range []string{filepath.Join(lnk, "proj", "x.go"), filepath.Join(lnk, "proj", "sub", "y.go")} {
		if !inProject(a, s, p) {
			t.Errorf("missing subdirectory cwd: %s not in project", p)
		}
	}
	if err := os.RemoveAll(proj); err != nil {
		t.Fatal(err)
	}
	s = &Session{Cwd: filepath.Join(lnk, "proj")}
	if p := filepath.Join(lnk, "proj", "x.go"); !inProject(a, s, p) {
		t.Errorf("removed project: %s not in project", p)
	}
}
