package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/store"
)

// newEnv makes a git project, points HOME away, and changes into it.
func newEnv(t *testing.T) (home, project string) {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	home, project = filepath.Join(base, "coal"), filepath.Join(base, "budget")
	os.MkdirAll(filepath.Join(project, ".git"), 0o755)
	t.Chdir(project)
	return home, project
}

func coalErr(home string, args ...string) error {
	var out bytes.Buffer
	root := rootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--home", home}, args...))
	return root.Execute()
}

func TestCoalNew(t *testing.T) {
	home, project := newEnv(t)
	first := strings.TrimSpace(coal(t, home, "new", "--intent", "Household budget tracker"))
	active, ids, ok := marker.Read(project)
	if !ok || active != first || len(ids) != 1 {
		t.Fatalf("first marker: %s %v %v", active, ids, ok)
	}
	second := strings.TrimSpace(coal(t, home, "new", "--intent", "Savings goals page"))
	active, ids, _ = marker.Read(project)
	if active != second || strings.Join(ids, ",") != first+","+second {
		t.Fatalf("second marker: %s %v", active, ids)
	}
	b, _ := os.ReadFile(filepath.Join(project, ".coal"))
	if !strings.Contains(string(b), `"manifests"`) || !strings.Contains(string(b), marker.SchemaURL) {
		t.Errorf("marker: %s", b)
	}

	a, err := app.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	ms, _ := a.Store.List()
	a.Close()
	sub := filepath.Join(project, "src")
	os.MkdirAll(sub, 0o755)
	if got := marker.Match(sub, ms); got == nil || got.ID != second {
		t.Errorf("match after new: %v", got)
	}
	for _, m := range ms {
		if m.Intent.Source != manifest.SourceUser || len(m.Contexts) == 0 || m.Contexts[0].Key != project {
			t.Errorf("manifest %s: %+v %+v", m.ID, m.Intent, m.Contexts)
		}
	}

	// --switch makes an id from the list active and keeps the list.
	if got := strings.TrimSpace(coal(t, home, "new", "--switch", first[:13])); got != first {
		t.Errorf("switch printed %q", got)
	}
	active, ids, _ = marker.Read(project)
	if active != first || len(ids) != 2 {
		t.Errorf("after switch: %s %v", active, ids)
	}
	if err := coalErr(home, "new", "--switch", manifest.NewID()); err == nil {
		t.Error("switched to an id outside the list")
	}
	if err := coalErr(home, "new"); err == nil {
		t.Error("new without --intent ran")
	}
}

func TestCoalNewRefuses(t *testing.T) {
	home, project := newEnv(t)
	os.WriteFile(filepath.Join(project, ".coal"), []byte("my own notes"), 0o644)
	if err := coalErr(home, "new", "--intent", "Budget tracker page"); err == nil {
		t.Error("ran over a .coal that is not a marker")
	}
	if b, _ := os.ReadFile(filepath.Join(project, ".coal")); string(b) != "my own notes" {
		t.Errorf("user file changed: %q", b)
	}
	if files, _ := filepath.Glob(filepath.Join(home, "manifests", "*.json")); len(files) != 0 {
		t.Errorf("created %d manifests", len(files))
	}
	os.MkdirAll(os.Getenv("HOME"), 0o755)
	t.Chdir(os.Getenv("HOME"))
	if err := coalErr(home, "new", "--intent", "Budget tracker page"); err == nil {
		t.Error("ran in ~")
	}
}

func TestCoalNewMarkersNone(t *testing.T) {
	home, project := newEnv(t)
	coal(t, home, "init")
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"markers":"none"}`), 0o600)
	out := coal(t, home, "new", "--intent", "Household budget tracker")
	if _, err := os.Stat(filepath.Join(project, ".coal")); err == nil {
		t.Error("wrote .coal with markers none")
	}
	lines := strings.Fields(out)
	if id := lines[len(lines)-1]; len(id) != 36 || !strings.Contains(out, "markers are off") {
		t.Errorf("output %q", out)
	}
}

func TestCoalNewMarkersNoneUpdatesExisting(t *testing.T) {
	home, project := newEnv(t)
	first := strings.TrimSpace(coal(t, home, "new", "--intent", "Household budget tracker"))
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"markers":"none"}`), 0o600)
	out := coal(t, home, "new", "--intent", "Savings goals page")
	lines := strings.Fields(out)
	second := lines[len(lines)-1]
	active, ids, _ := marker.Read(project)
	if active != second || len(ids) != 2 || ids[0] != first {
		t.Errorf("marker: %s %v", active, ids)
	}
	if !strings.Contains(out, "already has a .coal marker") {
		t.Errorf("output does not say the marker was updated: %q", out)
	}
}

func TestPointMarkerRace(t *testing.T) {
	_, project := newEnv(t)
	captured, mine := manifest.NewID(), manifest.NewID()
	// coal new looked and saw no marker; then capture wrote one.
	if err := marker.Write(project, captured); err != nil {
		t.Fatal(err)
	}
	if _, err := pointMarker(project, mine, false, "project"); err != nil {
		t.Fatal(err)
	}
	active, ids, _ := marker.Read(project)
	if active != mine || len(ids) != 2 || ids[0] != captured {
		t.Errorf("after the race: %s %v", active, ids)
	}
}

func TestSwitchToProjectManifest(t *testing.T) {
	home, project := newEnv(t)
	first := strings.TrimSpace(coal(t, home, "new", "--intent", "Household budget tracker"))
	a, err := app.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(status, key string) string {
		m := manifest.New("Made elsewhere", manifest.SourceUser, cliProvenance)
		m.Status = status
		m.Contexts = []manifest.Context{{Kind: marker.KindPath, Key: key}}
		if err := a.Create(m, store.Op{Tool: "test"}); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	paused := mk(manifest.StatusPaused, project)
	done := mk(manifest.StatusDone, project)
	other := mk(manifest.StatusActive, filepath.Join(filepath.Dir(project), "other"))
	a.Close()

	if got := strings.TrimSpace(coal(t, home, "new", "--switch", paused)); got != paused {
		t.Errorf("switch printed %q", got)
	}
	active, ids, _ := marker.Read(project)
	if active != paused || len(ids) != 2 || ids[0] != first {
		t.Errorf("after switch: %s %v", active, ids)
	}
	for _, id := range []string{done, other} {
		if err := coalErr(home, "new", "--switch", id); err == nil {
			t.Errorf("switched to %s", id)
		}
	}
	if active, _, _ := marker.Read(project); active != paused {
		t.Errorf("a refused switch changed the marker to %s", active)
	}
}
