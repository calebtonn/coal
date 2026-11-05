package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/metrics"
)

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
		if took > 200*time.Millisecond {
			t.Errorf("%s took %v", s.name, took)
		}
	}
	out, took := hook(t, home, project, "session-start", "session-start-resume.json")
	if took > 200*time.Millisecond {
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
	if res.SystemMessage != `COAL: You left off at "add CSV export".` || res.HookSpecificOutput.HookEventName != "SessionStart" ||
		!strings.Contains(res.HookSpecificOutput.AdditionalContext, "budget.html") {
		t.Errorf("resume: %+v", res)
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

func TestHookNeverFails(t *testing.T) {
	home := t.TempDir()
	for _, in := range []string{"not json", "{}", strings.Repeat("x", maxPayload+10)} {
		var out bytes.Buffer
		root := rootCmd()
		root.SetIn(strings.NewReader(in))
		root.SetOut(&out)
		root.SetArgs([]string{"--home", home, "hook", "stop"})
		if err := root.Execute(); err != nil || out.Len() != 0 {
			t.Errorf("%.20q: err=%v out=%q", in, err, out.String())
		}
	}
	log, _ := os.ReadFile(filepath.Join(home, "logs", "coal.log"))
	if strings.Count(string(log), "hook stop") != 3 {
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
