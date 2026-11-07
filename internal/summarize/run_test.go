package summarize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script writes an executable shell script under t.TempDir and returns
// its path. Nothing here calls a model or the network.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-summarizer")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

const okBody = `cat > "$1"
printf 'Here you go.\n**Summary:** Added a spending chart by category.\nNext step: Add CSV export\n'`

func TestRunPassesTranscriptOnStdin(t *testing.T) {
	early := user("/w", "EARLY-PROMPT-NOT-SENT")
	late := user("/w", "Add a spending chart by category please") + assistant("/w", "m1", text("Done."))
	tr := write(t, early+strings.Repeat(assistant("/w", "pad", text(strings.Repeat("x", 100))), 50)+late)
	stdin := filepath.Join(t.TempDir(), "stdin")
	cfg := &Config{Command: []string{script(t, okBody), stdin}, TimeoutSeconds: 5, MaxTranscriptBytes: int64(len(late) + 50)}
	summary, next, err := Run(context.Background(), cfg, tr)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Added a spending chart by category." || next != "Add CSV export" {
		t.Errorf("summary %q, next %q", summary, next)
	}
	got, _ := os.ReadFile(stdin)
	if !strings.HasPrefix(string(got), Prompt) || !strings.Contains(string(got), "Add a spending chart") || strings.Contains(string(got), "EARLY-PROMPT") {
		t.Errorf("stdin:\n%.300s", got)
	}
	if rest := strings.TrimPrefix(string(got), Prompt); !strings.HasPrefix(rest, "{") {
		t.Errorf("tail did not start on a whole line: %.80q", rest)
	}
}

func TestRunFailures(t *testing.T) {
	tr := write(t, user("/w", "a prompt with enough words"))
	cases := map[string]struct {
		body    string
		timeout int
	}{
		"exit 1":      {"echo 'Summary: should not count'; exit 1", 5},
		"timeout":     {"sleep 10; echo 'Summary: too late'", 1},
		"unparseable": {"echo 'I could not do that.'", 5},
	}
	for name, c := range cases {
		cfg := &Config{Command: []string{script(t, c.body)}, TimeoutSeconds: c.timeout, MaxTranscriptBytes: 1000}
		start := time.Now()
		summary, _, err := Run(context.Background(), cfg, tr)
		if err == nil || summary != "" {
			t.Errorf("%s: got %q, %v", name, summary, err)
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s took %s", name, took)
		}
		if name == "unparseable" && !errors.Is(err, ErrNoResult) {
			t.Errorf("unparseable: %v", err)
		}
		if strings.Contains(err.Error(), "a prompt with enough words") {
			t.Errorf("%s: error carries transcript text: %v", name, err)
		}
	}
}

func TestParseOutput(t *testing.T) {
	long := strings.Repeat("word ", 100)
	s, n := parseOutput([]byte("noise\n- Summary: " + long + "\nNext step: none\nSummary: second\n"))
	if len([]rune(s)) > 300 || !strings.HasPrefix(s, "word") || n != "" {
		t.Errorf("%q %q", s, n)
	}
	if s, n = parseOutput([]byte("next step: Ship it\n")); s != "" || n != "Ship it" {
		t.Errorf("%q %q", s, n)
	}
}

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	if c, err := LoadConfig(home); c != nil || err != nil {
		t.Errorf("missing file: %v %v", c, err)
	}
	for body, want := range map[string]bool{
		`{"version":"0.1"}`:                     false,
		`{"summarizer":null}`:                   false,
		`{"summarizer":false}`:                  false,
		`{"summarizer":{"command":["claude"]}}`: true,
	} {
		os.WriteFile(filepath.Join(home, "config.json"), []byte(body), 0o600)
		c, err := LoadConfig(home)
		if err != nil || (c != nil) != want {
			t.Errorf("%s: %+v %v", body, c, err)
		}
		if c != nil && (c.TimeoutSeconds != DefaultTimeoutSeconds || c.MaxTranscriptBytes != DefaultMaxTranscriptBytes) {
			t.Errorf("defaults: %+v", c)
		}
	}
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"summarizer":{"command":[]}}`), 0o600)
	if _, err := LoadConfig(home); err == nil {
		t.Error("an empty command should be an error")
	}
}
