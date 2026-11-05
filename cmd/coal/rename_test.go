package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
)

// runCoal runs the CLI with stdin and returns combined output and the error.
func runCoal(home, stdin string, args ...string) (string, error) {
	var out bytes.Buffer
	root := rootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"--home", home}, args...))
	err := root.Execute()
	return out.String(), err
}

func openTestApp(t *testing.T, home string) *app.App {
	t.Helper()
	a, err := app.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

var testOp = store.Op{Tool: "test"}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRename(t *testing.T) {
	home := t.TempDir()
	a := openTestApp(t, home)
	m := manifest.New("index.html", manifest.SourceModel, manifest.Provenance{Tool: "test"})
	must(t, a.Create(m, testOp))
	a.Close()

	coal(t, home, "rename", m.ID[:13], "  Household budget tracker  ")
	a = openTestApp(t, home)
	got, err := a.Get(m.ID)
	must(t, err)
	if got.Intent.Summary != "Household budget tracker" || got.Intent.Source != manifest.SourceUser {
		t.Errorf("intent %+v", got.Intent)
	}
	if rs, _ := a.Search("budget", index.SearchOpts{}); len(rs) != 1 || rs[0].Source != "user" {
		t.Errorf("index not updated: %+v", rs)
	}
	s, _ := metrics.Summarize(home, time.Time{})
	if s.Counts[metrics.Rename] != 1 {
		t.Errorf("rename metric %v", s.Counts)
	}
	b, _ := os.ReadFile(metrics.Path(home))
	if !strings.Contains(string(b), `"previousSource":"model"`) {
		t.Errorf("metrics %s", b)
	}
	// Renaming to the same text writes nothing and counts nothing.
	coal(t, home, "rename", m.ID, "Household budget tracker")
	if s, _ := metrics.Summarize(home, time.Time{}); s.Counts[metrics.Rename] != 1 {
		t.Errorf("no-op rename counted")
	}

	for _, bad := range []string{"   ", strings.Repeat("x", 201)} {
		if _, err := runCoal(home, "", "rename", m.ID, bad); err == nil || !strings.Contains(err.Error(), "1 to 200") {
			t.Errorf("summary %q: %v", bad, err)
		}
	}
	if _, err := runCoal(home, "", "rename", m.ID, strings.Repeat("é", 200)); err != nil {
		t.Errorf("200 characters rejected: %v", err)
	}
}

func TestRenameAmbiguous(t *testing.T) {
	home := t.TempDir()
	a := openTestApp(t, home)
	for _, id := range []string{"019a1111-2222-7333-8444-555555555555", "019a1111-2222-7333-8444-666666666666"} {
		m := manifest.New("summary "+id[len(id)-4:], manifest.SourceUser, manifest.Provenance{Tool: "test"})
		m.ID = id
		must(t, a.Create(m, testOp))
	}
	_, err := runCoal(home, "", "rename", "019a1111-2222", "New name")
	if err == nil || !strings.Contains(err.Error(), "matches 2 manifests") ||
		!strings.Contains(err.Error(), "019a1111-2222-7333-8444-555555555555  summary 5555") ||
		!strings.Contains(err.Error(), "019a1111-2222-7333-8444-666666666666  summary 6666") {
		t.Fatalf("err %v", err)
	}
	coal(t, home, "rename", "019a1111-2222-7333-8444-6", "New name")
	if m, _ := a.Get("019a1111-2222-7333-8444-666666666666"); m.Intent.Summary != "New name" {
		t.Errorf("summary %q", m.Intent.Summary)
	}
}
