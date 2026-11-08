package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
)

func TestPurge(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	page := filepath.Join(dir, "budget.html")
	must(t, os.WriteFile(page, []byte("<p>token sk-ish</p>"), 0o600))
	a := openTestApp(t, home)
	m1 := manifest.New("Budget tracker", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	m2 := manifest.New("Another page", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	must(t, a.Create(m1, testOp))
	must(t, a.Create(m2, testOp))
	in := store.ArtifactInput{Path: page, Producer: manifest.Provenance{Tool: "test"}}
	v1, err := a.RegisterArtifact(m1.ID, in, testOp)
	must(t, err)
	_, err = a.RegisterArtifact(m2.ID, in, testOp)
	must(t, err)
	must(t, os.WriteFile(page, []byte("<p>clean</p>"), 0o600))
	_, err = a.RegisterArtifact(m1.ID, in, testOp)
	must(t, err)
	hash := v1.Hash
	hexHash := strings.TrimPrefix(hash, "sha256:")
	body := a.Store.ArtifactPath(hash)
	tmpCopy := filepath.Join(home, "tmp", hexHash[:8]+"-budget.html")
	must(t, os.MkdirAll(filepath.Dir(tmpCopy), 0o700))
	must(t, os.WriteFile(tmpCopy, []byte("copy"), 0o600))
	a.Close()

	out, err := runCoal(home, "no\n", "purge", hash)
	if err == nil || !strings.Contains(out, body) || !strings.Contains(out, m1.ID) || !strings.Contains(out, m2.ID) ||
		!strings.Contains(out, "versions [1]") || !strings.Contains(out, tmpCopy) {
		t.Fatalf("refusal: %v\n%s", err, out)
	}
	if _, err := os.Stat(body); err != nil {
		t.Fatal("body removed without yes")
	}

	coal(t, home, "purge", hexHash, "--yes")
	for _, p := range []string{body, tmpCopy} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	a = openTestApp(t, home)
	got1, err := a.Get(m1.ID) // Get validates, so the marked manifest is still valid
	must(t, err)
	art := got1.Artifacts[0]
	if art.Versions[0].Skipped != "purged" || art.Versions[1].Skipped != "" || art.Skipped != "" {
		t.Errorf("m1 artifact %+v", art)
	}
	got2, err := a.Get(m2.ID)
	must(t, err)
	if got2.Artifacts[0].Skipped != "purged" || got2.Artifacts[0].Versions[0].Skipped != "purged" {
		t.Errorf("m2 artifact %+v", got2.Artifacts[0])
	}
	if ms, _ := a.Store.List(); len(ms) != 2 {
		t.Errorf("%d manifests left", len(ms))
	}
	log, _ := os.ReadFile(filepath.Join(home, "logs", "coal.log"))
	if !strings.Contains(string(log), "purge "+hash) {
		t.Errorf("coal.log:\n%s", log)
	}
	if s, _ := metrics.Summarize(home, time.Time{}); s.Counts[metrics.Purge] != 1 {
		t.Errorf("purge metric %v", s.Counts)
	}
	if _, err := runCoal(home, "", "purge", hash, "--yes"); err != nil {
		t.Errorf("second purge of a marked hash should be a harmless no-op: %v", err)
	}
	if _, err := runCoal(home, "", "purge", "abc", "--yes"); err == nil {
		t.Error("short hash accepted")
	}
}
