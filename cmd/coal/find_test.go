package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/index"
)

func coal(t *testing.T, home string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := rootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--home", home}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("coal %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func removeIndex(home string) {
	for _, sfx := range []string{"", "-wal", "-shm"} {
		os.Remove(filepath.Join(home, "index.db") + sfx)
	}
}

func TestFindReindex(t *testing.T) {
	home, page := t.TempDir(), filepath.Join(t.TempDir(), "budget.html")
	os.WriteFile(page, []byte("<h1>Budget</h1>"), 0o600)
	id := strings.TrimSpace(coal(t, home, "add", "--intent", "Household budget tracker as a <single> HTML page", "--artifact", page))
	// Remove the index the first add created: the next write must not
	// start a new index that hides this manifest.
	removeIndex(home)
	other := strings.TrimSpace(coal(t, home, "add", "--intent", "Garden planting calendar", "--status", "paused"))

	out := coal(t, home, "find", "budgets")
	if !strings.HasPrefix(out, shortID(id, []string{id, other})+" ") || strings.Contains(out, other) {
		t.Fatalf("find budgets:\n%s", out)
	}
	// Non-TTY: <b> markers dropped, escaped text unescaped.
	if !strings.Contains(out, "Household budget tracker as a <single> HTML page") || strings.Contains(out, "<b>") ||
		!strings.Contains(out, "1 artifact ") && !strings.Contains(out, "1 artifact\n") {
		t.Fatalf("find output:\n%s", out)
	}

	var rs []map[string]any
	if err := json.Unmarshal([]byte(coal(t, home, "find", "--json", "budg")), &rs); err != nil || len(rs) != 1 {
		t.Fatalf("json: %v %v", rs, err)
	}
	for _, k := range []string{"id", "summary", "source", "status", "lastActivityAt", "updatedAt", "artifactCount", "nextStep", "snippet", "matched"} {
		if _, ok := rs[0][k]; !ok {
			t.Errorf("json result lacks %q", k)
		}
	}
	if _, ok := rs[0]["rank"]; ok {
		t.Error("json result has rank, which is not in the API shape")
	}
	if rs[0]["snippet"] != "Household <b>budget</b> tracker as a &lt;single&gt; HTML page" {
		t.Errorf("snippet %v", rs[0]["snippet"])
	}

	if out := coal(t, home, "find", "--status", "paused"); !strings.HasPrefix(out, shortID(other, []string{id, other})+" ") {
		t.Errorf("find --status paused:\n%s", out)
	}

	// Delete the index; find rebuilds it, and so does reindex.
	removeIndex(home)
	if out := coal(t, home, "find", "budget"); !strings.Contains(out, "Household budget") {
		t.Errorf("find after deleting index:\n%s", out)
	}
	os.WriteFile(filepath.Join(home, "manifests", "019a0000-0000-7000-8000-000000000000.json"), []byte("{"), 0o600)
	if out := coal(t, home, "reindex"); !strings.Contains(out, "indexed 2 manifests, skipped 1 as corrupt") {
		t.Errorf("reindex:\n%s", out)
	}

	coal(t, home, "rm", id)
	if out := coal(t, home, "find", "budget"); strings.Contains(out, "Household") {
		t.Errorf("removed manifest still found:\n%s", out)
	}
}

func TestPrintResults(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	rs := []index.Result{
		{ID: "019a1111-2222-7333-8444-555555555555", Summary: strings.Repeat("long summary ", 10), Status: "active",
			LastActivityAt: "2026-11-04T09:00:00Z", ArtifactCount: 2, Snippet: "a <b>budget</b> &amp; more"},
		{ID: "019a2222-2222-7333-8444-555555555555", Summary: "short", Status: "paused", LastActivityAt: "2026-11-01T12:00:00Z", ArtifactCount: 1},
	}
	var plain, tty bytes.Buffer
	all := []string{rs[0].ID, rs[1].ID}
	printResults(&plain, rs, all, false, now)
	printResults(&tty, rs, all, true, now)
	lines := strings.Split(strings.TrimRight(plain.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines:\n%s", plain.String())
	}
	if !strings.HasPrefix(lines[0], "019a1111  long summary") || !strings.Contains(lines[0], "…  active  3h ago  2 artifacts") {
		t.Errorf("row 1: %q", lines[0])
	}
	if lines[1] != "          a budget & more" {
		t.Errorf("snippet line: %q", lines[1])
	}
	// Columns line up: status starts at the same offset in both rows.
	if col := func(l, w string) int { return len([]rune(l[:strings.Index(l, w)])) }; col(lines[0], "active") != col(lines[2], "paused") {
		t.Errorf("misaligned:\n%s", plain.String())
	}
	if !strings.Contains(lines[2], "3d ago  1 artifact") {
		t.Errorf("row 2: %q", lines[2])
	}
	if !strings.Contains(tty.String(), "a \x1b[1mbudget\x1b[22m & more") {
		t.Errorf("tty snippet: %q", tty.String())
	}
}

func TestShortID(t *testing.T) {
	all := []string{"019a1111-2222-7333-8444-555555555555", "019a1111-2299-7333-8444-555555555555", "019b0000-0000-7000-8000-000000000000"}
	for id, want := range map[string]string{all[0]: "019a1111-222", all[1]: "019a1111-229", all[2]: "019b0000"} {
		if got := shortID(id, all); got != want {
			t.Errorf("shortID(%s) = %s, want %s", id, got, want)
		}
	}
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for ts, want := range map[string]string{
		"2026-11-04T11:59:30Z": "just now",
		"2026-11-04T11:15:00Z": "45m ago",
		"2026-11-03T12:00:00Z": "1d ago",
		"2026-08-04T12:00:00Z": "3mo ago",
		"2023-11-04T12:00:00Z": "3y ago",
		"bad":                  "bad",
	} {
		if got := relTime(ts, now); got != want {
			t.Errorf("relTime(%s) = %s, want %s", ts, got, want)
		}
	}
}
