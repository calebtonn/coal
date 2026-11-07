package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
)

// resumeFixture creates a budget manifest anchored at a fresh project
// directory and returns the data dir and the project dir.
func resumeFixture(t *testing.T, next string, questions ...string) (home, project string, m *manifest.Manifest) {
	t.Helper()
	home, project = t.TempDir(), t.TempDir()
	a := openTestApp(t, home)
	m = manifest.New("Household budget tracker as a single HTML page", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	m.Contexts = []manifest.Context{{Kind: marker.KindPath, Key: project}}
	m.State.NextStep, m.State.OpenQuestions = next, questions
	must(t, a.Create(m, testOp))
	a.Close()
	return home, project, m
}

// stubExit records the exit code instead of exiting.
func stubExit(t *testing.T) *int {
	t.Helper()
	code := -1
	exit = func(c int) { code = c }
	t.Cleanup(func() { exit = os.Exit })
	return &code
}

func TestResumePrompt(t *testing.T) {
	_, _, m := resumeFixture(t, "Add CSV export.", "Which currency?", "Monthly or weekly totals")
	got := resumePrompt(m)
	want := "Resume my work on: Household budget tracker as a single HTML page. " +
		"Last time we left off at: Add CSV export. " +
		"Open questions: Which currency?; Monthly or weekly totals. " +
		"Start by telling me in one sentence where I left off and ask whether to continue."
	if got != want {
		t.Fatalf("prompt:\n%s\nwant:\n%s", got, want)
	}

	m.State.NextStep, m.State.OpenQuestions = "", nil
	if got := resumePrompt(m); strings.Contains(got, "Last time") || strings.Contains(got, "Open questions") {
		t.Fatalf("missing parts not omitted: %s", got)
	}

	m.Intent.Summary = strings.Repeat("budget ", 400) + "\x1b[31m"
	m.State.NextStep = "line one\nline two"
	for range 20 {
		m.State.OpenQuestions = append(m.State.OpenQuestions, strings.Repeat("q", 300))
	}
	got = resumePrompt(m)
	if n := len([]rune(got)); n > maxResumePrompt {
		t.Fatalf("prompt is %d runes", n)
	}
	if strings.ContainsAny(got, "\n\x1b") || strings.Contains(strings.ToLower(got), "memory") {
		t.Fatalf("prompt is not plain one-line text: %q", got)
	}
	if !strings.Contains(got, "line one line two") || !strings.HasSuffix(got, "ask whether to continue.") {
		t.Fatalf("prompt lost its parts: %q", got)
	}
}

func TestResumePrintAndNoMatch(t *testing.T) {
	home, project, _ := resumeFixture(t, "Add CSV export")
	t.Chdir(filepath.Join(project))
	code := stubExit(t)
	out, err := runCoal(home, "", "resume", "--print")
	must(t, err)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "claude" || !strings.Contains(lines[1], "Last time we left off at: Add CSV export.") {
		t.Fatalf("--print output:\n%s", out)
	}
	if *code != -1 {
		t.Fatalf("--print exited %d", *code)
	}

	// A subdirectory matches the same record.
	sub := filepath.Join(project, "src")
	must(t, os.Mkdir(sub, 0o700))
	t.Chdir(sub)
	if out, err = runCoal(home, "", "resume", "--print"); err != nil || !strings.HasPrefix(out, "claude\n") {
		t.Fatalf("subdir: %v %s", err, out)
	}

	t.Chdir(t.TempDir())
	out, err = runCoal(home, "", "resume", "--print")
	if err != nil || *code != 1 || strings.TrimSpace(out) != noRecord {
		t.Fatalf("no match: code %d err %v out %q", *code, err, out)
	}

	*code = -1
	out, _ = runCoal(home, "", "resume", "--tool", "cursor")
	if *code != 2 || !strings.Contains(out, `unknown --tool "cursor"`) {
		t.Fatalf("unknown tool: code %d out %q", *code, out)
	}
}

// TestResumeExecsClaude runs the built binary so syscall.Exec really
// replaces the process, with a stand-in claude that records its argv.
func TestResumeExecsClaude(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	home, project, m := resumeFixture(t, "Add CSV export", "Which currency?")
	bin := filepath.Join(t.TempDir(), "coal")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fake := t.TempDir()
	argvFile := filepath.Join(fake, "argv")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + argvFile + "\n"
	must(t, os.WriteFile(filepath.Join(fake, "claude"), []byte(script), 0o700))

	cmd := exec.Command(bin, "resume")
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "COAL_HOME="+home, "PATH="+fake+":/usr/bin:/bin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("coal resume: %v\n%s", err, out)
	}
	argv, err := os.ReadFile(argvFile)
	must(t, err)
	if got := strings.TrimSuffix(string(argv), "\n"); got != resumePrompt(m) {
		t.Fatalf("claude got argv %q", got)
	}
	metricsLog, err := os.ReadFile(filepath.Join(home, "logs", "metrics.jsonl"))
	if err != nil || !strings.Contains(string(metricsLog), `"event":"resume_launch"`) || !strings.Contains(string(metricsLog), m.ID) {
		t.Fatalf("metrics: %v %s", err, metricsLog)
	}
}
