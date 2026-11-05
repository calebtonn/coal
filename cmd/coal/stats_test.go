package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/metrics"
)

func TestStats(t *testing.T) {
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, "logs"), 0o700))
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	var b strings.Builder
	line := func(d time.Duration, event, extra string) {
		fmt.Fprintf(&b, `{"at":%q,"event":%q%s}`+"\n", at(d), event, extra)
	}
	line(30*24*time.Hour, "coal_find", `,"tool":"old"`)
	line(time.Hour, "coal_find", `,"tool":"claude-code"`)
	line(time.Hour, "coal_find", `,"tool":"claude-code"`)
	line(2*time.Hour, "coal_find", `,"tool":"cursor"`)
	for range 4 {
		line(time.Hour, "resume_shown", "")
	}
	for range 3 {
		line(time.Hour, "resume_acted", "")
	}
	line(time.Hour, "session_attached", "")
	line(time.Hour, "manifest_created", "")
	line(time.Hour, "manifest_created", "")
	line(time.Hour, "rename", `,"manifest":"x"`)
	must(t, os.WriteFile(metrics.Path(home), []byte(b.String()), 0o600))

	out := coal(t, home, "stats")
	for _, want := range []string{
		"Last 7 days",
		"  coal_find              3\n    claude-code          2\n    cursor               1\n  resume_shown",
		"resume_shown           4", "resume_acted           3", "rename                 1", "purge                  0",
		"resume acted on / shown           3 / 4 (75%)",
		"attached / (attached + created)   1 / 3 (33%)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old") {
		t.Errorf("event outside the window counted:\n%s", out)
	}
	if out := coal(t, home, "stats", "--days", "0"); !strings.Contains(out, "All time") || !strings.Contains(out, "old") {
		t.Errorf("all time:\n%s", out)
	}
	if out := coal(t, t.TempDir(), "stats"); !strings.Contains(out, "no data") {
		t.Errorf("empty:\n%s", out)
	}
}
