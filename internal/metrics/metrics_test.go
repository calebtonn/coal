package metrics

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecordFormat(t *testing.T) {
	home := t.TempDir()
	Record(home, Find, map[string]any{"tool": "claude-code", "at": "forged", "event": "forged"})
	b, err := os.ReadFile(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]string
	if err := json.Unmarshal(b, &line); err != nil {
		t.Fatal(err)
	}
	if line["event"] != Find || line["tool"] != "claude-code" || line["at"] == "forged" || len(line) != 3 {
		t.Errorf("line %s", b)
	}
	// Same bytes as the MCP server's writer: keys sorted, one line.
	want, _ := json.Marshal(map[string]string{"at": line["at"], "event": Find, "tool": "claude-code"})
	if string(b) != string(want)+"\n" {
		t.Errorf("got %q, want %q", b, want)
	}
}

func TestRecordNeverFails(t *testing.T) {
	home := filepath.Join(t.TempDir(), "file")
	os.WriteFile(home, nil, 0o600) // logs/ cannot be created under a file
	Record(home, Rename, nil)
}

func TestRecordConcurrent(t *testing.T) {
	home := t.TempDir()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { Record(home, ResumeShown, map[string]any{"manifest": "x"}) })
	}
	wg.Wait()
	f, _ := os.Open(Path(home))
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); n++ {
		if !json.Valid(sc.Bytes()) {
			t.Errorf("bad line %q", sc.Text())
		}
	}
	if n != 50 {
		t.Errorf("%d lines", n)
	}
}

func TestSummarize(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "logs"), 0o700)
	fixture := `{"at":"2026-10-01T00:00:00Z","event":"coal_find","tool":"old"}
{"at":"2026-11-01T00:00:00Z","event":"coal_find","tool":"claude-code"}
{"at":"2026-11-02T00:00:00Z","event":"coal_find","tool":"claude-code"}
{"at":"2026-11-02T00:00:00Z","event":"coal_find","tool":"cursor"}
{"at":"2026-11-02T00:00:00Z","event":"coal_find"}
{"at":"2026-11-03T00:00:00Z","event":"resume_shown"}
{"at":"2026-11-03T00:00:00Z","event":"resume_shown"}
{"at":"2026-11-03T00:00:00Z","event":"resume_shown"}
{"at":"2026-11-03T00:00:00Z","event":"resume_shown"}
{"at":"2026-11-03T00:00:00Z","event":"resume_acted"}
{"at":"2026-11-03T00:00:00Z","event":"resume_acted"}
{"at":"2026-11-03T00:00:00Z","event":"resume_acted"}
{"at":"2026-11-03T00:00:00Z","event":"session_attached"}
{"at":"2026-11-03T00:00:00Z","event":"manifest_created"}
{"at":"2026-11-03T00:00:00Z","event":"manifest_created"}
{"at":"2026-11-03T00:00:00Z","event":"merge"}
not json

{"at":"yesterday","event":"rename"}
`
	os.WriteFile(Path(home), []byte(fixture), 0o600)
	s, err := Summarize(home, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if s.Counts[Find] != 4 || s.FindTools["claude-code"] != 2 || s.FindTools["cursor"] != 1 || s.FindTools["unknown"] != 1 || s.FindTools["old"] != 0 {
		t.Errorf("finds %v %v", s.Counts, s.FindTools)
	}
	if r := s.ResumeActed(); r != (Ratio{3, 4}) {
		t.Errorf("resume %v", r)
	}
	if r := s.Attached(); r != (Ratio{1, 3}) {
		t.Errorf("attached %v", r)
	}
	if s.Bad != 2 || len(s.Others()) != 1 || s.Others()[0] != "merge" {
		t.Errorf("bad %d others %v", s.Bad, s.Others())
	}
	empty, err := Summarize(t.TempDir(), time.Time{})
	if err != nil || len(empty.Counts) != 0 || empty.Attached().Total != 0 {
		t.Errorf("missing file: %+v %v", empty, err)
	}
}
