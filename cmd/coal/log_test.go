package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

func TestLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	id := strings.TrimSpace(coal(t, home, "add", "--intent", "Household budget tracker"))
	coal(t, home, "rename", id, "Budget tracker, one page")

	out := coal(t, home, "log", id[:13])
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "coal-cli: rename on "+id) || !strings.Contains(lines[1], "coal-cli: add manifest on "+id) {
		t.Fatalf("log:\n%s", out)
	}
	if f := strings.Fields(lines[0]); len(f) < 3 || !strings.HasSuffix(f[1], "Z") {
		t.Errorf("want '<hash> <iso date> <subject>', got %q", lines[0])
	}
	if out := coal(t, home, "log", "--patch", id); !strings.Contains(out, "diff --git") || !strings.Contains(out, `+    "summary": "Budget tracker, one page"`) {
		t.Errorf("patch:\n%s", out)
	}
	// History outlives the manifest: a full id works after rm.
	coal(t, home, "rm", id)
	if out := coal(t, home, "log", id); len(strings.Split(strings.TrimSpace(out), "\n")) != 3 {
		t.Errorf("log after rm:\n%s", out)
	}

	off := t.TempDir()
	a, err := app.Open(off, store.WithHistory(false))
	must(t, err)
	m := manifest.New("no history", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	must(t, a.Create(m, testOp))
	a.Close()
	if _, err := runCoal(off, "", "log", m.ID); err == nil || !strings.Contains(err.Error(), "history is off") {
		t.Errorf("history off: %v", err)
	}
}
