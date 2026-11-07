package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/queue"
)

// hookBudget is the hook time limit asserted in tests. The target is
// 200 ms; the margin keeps a loaded host from failing the suite.
const hookBudget = 500 * time.Millisecond

// hook runs coal hook <name> in process with a recorded payload whose
// placeholder path is rewritten to project, and returns stdout.
func hook(t *testing.T, home, project, name, file string) (string, time.Duration) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "claude-code", file))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := rootCmd()
	root.SetIn(strings.NewReader(strings.ReplaceAll(string(b), "/PROJECT/budget", project)))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--home", home, "hook", name})
	start := time.Now()
	if err := root.Execute(); err != nil {
		t.Fatalf("hook %s: %v", name, err)
	}
	return out.String(), time.Since(start)
}

func TestHookSession(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	home, project := filepath.Join(base, "coal"), filepath.Join(base, "budget")
	os.MkdirAll(filepath.Join(project, ".git"), 0o755)
	os.WriteFile(filepath.Join(project, "budget.html"), []byte("<h1>Budget</h1>"), 0o644)

	steps := []struct{ name, file string }{
		{"session-start", "session-start.json"},
		{"user-prompt-submit", "user-prompt-submit.json"},
		{"post-tool-use", "post-tool-use-write.json"},
		{"stop", "stop.json"},
		{"session-end", "session-end.json"},
	}
	for _, s := range steps {
		out, took := hook(t, home, project, s.name, s.file)
		if out != "" {
			t.Errorf("%s printed %q", s.name, out)
		}
		t.Logf("%s took %v", s.name, took)
		if took > hookBudget {
			t.Errorf("%s took %v", s.name, took)
		}
	}
	out, took := hook(t, home, project, "session-start", "session-start-resume.json")
	t.Logf("resume session-start took %v", took)
	if took > hookBudget {
		t.Errorf("resume session-start took %v", took)
	}
	var res struct {
		SystemMessage      string `json:"systemMessage"`
		HookSpecificOutput struct {
			HookEventName, AdditionalContext string
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("resume output %q: %v", out, err)
	}
	if res.SystemMessage != `COAL: Build me a household budget tracker as a single HTML page. Next: add CSV export.` || res.HookSpecificOutput.HookEventName != "SessionStart" ||
		!strings.Contains(res.HookSpecificOutput.AdditionalContext, "budget.html") {
		t.Errorf("resume: %+v", res)
	}

	// Hooks never run git: the writes are marked pending for the daemon
	// or the next CLI command to commit (ADR-014).
	if _, err := os.Stat(filepath.Join(home, ".history-pending")); err != nil {
		t.Errorf("no pending history marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".git", "HEAD")); err == nil {
		if log, _ := exec.Command("git", "-C", home, "log", "--oneline").Output(); len(log) > 0 {
			t.Errorf("a hook committed:\n%s", log)
		}
	}
	a, err := app.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ms, _ := a.Store.List()
	if len(ms) != 1 || ms[0].Intent.Source != "user" || len(ms[0].Artifacts) != 1 {
		t.Fatalf("manifests: %+v", ms)
	}
	if sm, _ := metrics.Summarize(home, time.Time{}); sm.Counts[metrics.ResumeShown] != 1 {
		t.Errorf("resume_shown: %+v", sm.Counts)
	}
	if left, _ := filepath.Glob(filepath.Join(home, "queue", "*.json")); len(left) != 0 {
		t.Errorf("queue not drained: %v", left)
	}
	if out := coal(t, home, "find", "budget"); !strings.Contains(out, "budget tracker") {
		t.Errorf("find: %s", out)
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("stdin exploded") }

func TestHookNeverFails(t *testing.T) {
	home := t.TempDir()
	for _, in := range []string{"not json", "{}", strings.Repeat("x", maxPayload+10)} {
		var out bytes.Buffer
		root := rootCmd()
		r := strings.NewReader(in)
		root.SetIn(r)
		root.SetOut(&out)
		root.SetArgs([]string{"--home", home, "hook", "stop"})
		if err := root.Execute(); err != nil || out.Len() != 0 || r.Len() != 0 {
			t.Errorf("%.20q: err=%v out=%q unread=%d", in, err, out.String(), r.Len())
		}
	}
	var out bytes.Buffer
	root := rootCmd()
	root.SetIn(panicReader{})
	root.SetOut(&out)
	root.SetArgs([]string{"--home", home, "hook", "stop"})
	if err := root.Execute(); err != nil || out.Len() != 0 {
		t.Errorf("panic: err=%v out=%q", err, out.String())
	}
	log, _ := os.ReadFile(filepath.Join(home, "logs", "coal.log"))
	if !strings.Contains(string(log), "panic: stdin exploded") {
		t.Errorf("panic not logged:\n%s", log)
	}
	if strings.Count(string(log), "hook stop") != 4 {
		t.Errorf("log:\n%s", log)
	}
}

func TestAdapterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	orig := `{
  "model": "opus",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}]
  }
}
`
	os.WriteFile(settings, []byte(orig), 0o600)
	t.Setenv("COAL_HOME", filepath.Join(dir, "coal home"))

	var out bytes.Buffer
	run := func(args ...string) error {
		root := rootCmd()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		return root.Execute()
	}
	if err := run("adapter", "install", "claude-code"); err == nil {
		t.Fatal("install without --settings ran")
	}
	for range 2 {
		if err := run("adapter", "install", "claude-code", "--settings", settings); err != nil {
			t.Fatal(err)
		}
	}
	var s struct {
		Model string
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct {
				Command string
				Timeout int
			}
		}
	}
	b, _ := os.ReadFile(settings)
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	bin, _ := os.Executable()
	bin, _ = filepath.EvalSymlinks(bin)
	if s.Model != "opus" || len(s.Hooks) != 5 || len(s.Hooks["Stop"]) != 2 || s.Hooks["Stop"][0].Hooks[0].Command != "notify-send done" {
		t.Fatalf("settings:\n%s", b)
	}
	want := "COAL_HOME='" + filepath.Join(dir, "coal home") + "' " + shellQuote(bin) + " hook stop"
	if got := s.Hooks["Stop"][1].Hooks[0].Command; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
	if s.Hooks["PostToolUse"][0].Matcher != "Write|Edit|MultiEdit|NotebookEdit" || s.Hooks["SessionEnd"][0].Hooks[0].Timeout != 5 {
		t.Errorf("settings:\n%s", b)
	}
	if _, err := os.Stat(settings + ".bak"); err != nil {
		t.Error(err)
	}

	// A binary that moved is replaced, not added again, and the first
	// .bak, the settings from before COAL, is kept.
	if err := editSettings(settings, "/old/place/coal", "", true); err != nil {
		t.Fatal(err)
	}
	if err := run("adapter", "install", "claude-code", "--settings", settings); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(settings)
	if n := strings.Count(string(b), " hook "); n != 5 || strings.Contains(string(b), "/old/place") {
		t.Errorf("after a move, %d coal hooks:\n%s", n, b)
	}
	if bak, _ := os.ReadFile(settings + ".bak"); string(bak) != orig {
		t.Errorf(".bak was overwritten:\n%s", bak)
	}

	if err := run("adapter", "uninstall", "claude-code", "--settings", settings); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(settings)
	var got, wantv any
	json.Unmarshal(b, &got)
	json.Unmarshal([]byte(orig), &wantv)
	gb, _ := json.Marshal(got)
	wb, _ := json.Marshal(wantv)
	if !bytes.Equal(gb, wb) {
		t.Errorf("after uninstall:\n%s\nwant\n%s", gb, wb)
	}
}

func TestSessionEndOnlyQueues(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	home := filepath.Join(base, "coal")
	// An event already waiting must still be waiting after session-end:
	// with the lock free, any drain would have folded it.
	if err := queue.Enqueue(home, &capture.Event{Kind: capture.KindPrompt, SessionRef: "claude-code:session:x"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(base, "budget")
	if out, _ := hook(t, home, project, "session-end", "session-end.json"); out != "" {
		t.Errorf("session-end printed %q", out)
	}
	if left, _ := filepath.Glob(filepath.Join(queue.Dir(home), "*.json")); len(left) != 2 {
		t.Errorf("queue holds %d events, want 2", len(left))
	}
	// With another process holding the lock, it still returns at once.
	f, err := os.OpenFile(filepath.Join(queue.Dir(home), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if _, took := hook(t, home, project, "session-end", "session-end.json"); took > hookBudget {
		t.Errorf("session-end took %v with the lock held", took)
	}
	if left, _ := filepath.Glob(filepath.Join(queue.Dir(home), "*.json")); len(left) != 3 {
		t.Errorf("queue holds %d events, want 3", len(left))
	}
	if _, err := os.Stat(filepath.Join(home, "index.db")); err == nil {
		t.Error("session-end opened the app")
	}
}

func TestHookSkipsSummarizerSessions(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("COAL_SUMMARIZER", "1")
	home, project := filepath.Join(base, "coal"), filepath.Join(base, "budget")
	os.MkdirAll(filepath.Join(project, ".git"), 0o755)
	os.WriteFile(filepath.Join(project, "budget.html"), []byte("x"), 0o644)
	for _, s := range [][2]string{
		{"session-start", "session-start.json"}, {"user-prompt-submit", "user-prompt-submit.json"},
		{"post-tool-use", "post-tool-use-write.json"}, {"stop", "stop.json"}, {"session-end", "session-end.json"},
	} {
		if out, _ := hook(t, home, project, s[0], s[1]); out != "" {
			t.Errorf("%s printed %q", s[0], out)
		}
	}
	if _, err := os.Stat(home); err == nil {
		t.Errorf("the data dir was touched: %s exists", home)
	}
	if _, err := os.Stat(filepath.Join(project, ".coal")); err == nil {
		t.Error("wrote a marker")
	}
}
