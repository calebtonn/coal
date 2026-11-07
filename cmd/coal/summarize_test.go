package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/summarize"
)

func TestQueueDrainRunsSummarizeJobs(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	id := strings.TrimSpace(coal(t, home, "add", "--intent", "Household budget tracker as a single HTML page"))
	fake := filepath.Join(dir, "fake")
	os.WriteFile(fake, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'Summary: Added a chart.\\nNext step: Add CSV export\\n'\n"), 0o700)
	// Jobs only read transcripts under $CLAUDE_CONFIG_DIR/projects.
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	os.MkdirAll(filepath.Join(dir, "projects", "-w"), 0o700)
	tr := filepath.Join(dir, "projects", "-w", "s1.jsonl")
	os.WriteFile(tr, []byte(`{"type":"user","cwd":"/w","message":{"content":"add a chart please now"}}`+"\n"), 0o600)
	cfg, _ := json.Marshal(map[string]any{"version": "0.1", "summarizer": map[string]any{"command": []string{fake}, "timeoutSeconds": 5}})
	os.WriteFile(filepath.Join(home, "config.json"), cfg, 0o600)
	os.MkdirAll(summarize.Dir(home), 0o700)
	job, _ := json.Marshal(summarize.Job{ManifestID: id, SessionRef: "claude-code:session:s1", TranscriptPath: tr, StartedAt: "2026-11-06T10:00:00Z"})
	os.WriteFile(filepath.Join(summarize.Dir(home), "s1.json"), job, 0o600)

	if out := coal(t, home, "queue", "drain"); !strings.Contains(out, "summarized 1 sessions") {
		t.Fatalf("drain:\n%s", out)
	}
	if out := coal(t, home, "show", id); !strings.Contains(out, `"nextStep": "Add CSV export"`) {
		t.Errorf("show:\n%s", out)
	}
}
