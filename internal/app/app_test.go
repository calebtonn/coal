package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

var op = store.Op{Tool: "test"}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, a *App, q string) []index.Result {
	t.Helper()
	rs, err := a.Search(q, index.SearchOpts{})
	must(t, err)
	return rs
}

func TestWritesKeepIndexInStep(t *testing.T) {
	home := t.TempDir()
	a, err := Open(home, store.WithHistory(false))
	must(t, err)
	defer a.Close()

	m := manifest.New("Household budget tracker", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	must(t, a.Create(m, op))
	if rs := find(t, a, "budgets"); len(rs) != 1 || rs[0].ID != m.ID || rs[0].ArtifactCount != 0 {
		t.Fatalf("after create: %+v", rs)
	}

	page := filepath.Join(t.TempDir(), "spending.html")
	must(t, os.WriteFile(page, []byte("<h1>Budget</h1>"), 0o600))
	_, err = a.RegisterArtifact(m.ID, store.ArtifactInput{Path: page, Title: "Spending page", Producer: manifest.Provenance{Tool: "test"}}, op)
	must(t, err)
	if rs := find(t, a, "spending"); len(rs) != 1 || rs[0].ArtifactCount != 1 || rs[0].Matched[0] != "artifacts" {
		t.Fatalf("after register: %+v", rs)
	}

	_, err = a.Update(m.ID, 0, func(m *manifest.Manifest) error {
		m.State.NextStep = "Add CSV export"
		return nil
	}, op)
	must(t, err)
	if rs := find(t, a, "csv"); len(rs) != 1 || rs[0].NextStep != "Add CSV export" {
		t.Fatalf("after update: %+v", rs)
	}

	_, err = a.Remove(m.ID, op)
	must(t, err)
	if rs := find(t, a, "budget"); len(rs) != 0 {
		t.Fatalf("after remove: %+v", rs)
	}
}

func TestOpenRebuildsEmptyIndex(t *testing.T) {
	home := t.TempDir()
	a, err := Open(home, store.WithHistory(false))
	must(t, err)
	must(t, a.Create(manifest.New("Garden planting calendar", manifest.SourceUser, manifest.Provenance{Tool: "test"}), op))
	must(t, a.Close())
	for _, sfx := range []string{"", "-wal", "-shm"} {
		os.Remove(filepath.Join(home, "index.db") + sfx)
	}
	must(t, os.WriteFile(filepath.Join(home, "manifests", "019a0000-0000-7000-8000-000000000000.json"), []byte("{"), 0o600))

	b, err := Open(home, store.WithHistory(false))
	must(t, err)
	defer b.Close()
	if rs := find(t, b, "garden"); len(rs) != 1 {
		t.Fatalf("not rebuilt: %+v", rs)
	}
	n, skipped, err := b.Reindex()
	if err != nil || n != 1 || skipped != 1 {
		t.Fatalf("Reindex = %d, %d, %v", n, skipped, err)
	}
}

func TestIndexErrorIsLoggedNotReturned(t *testing.T) {
	home := t.TempDir()
	a, err := Open(home, store.WithHistory(false))
	must(t, err)
	a.Index.Close() // every index call now fails
	m := manifest.New("budget", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	if err := a.Create(m, op); err != nil {
		t.Fatalf("Create returned the index error: %v", err)
	}
	if _, err := a.Get(m.ID); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(home, "logs", "coal.log"))
	if !strings.Contains(string(log), "index: upsert "+m.ID) {
		t.Fatalf("log:\n%s", log)
	}
}
