package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calebtonn/coal/internal/paths"
)

func TestInitHomeIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "coal")
	if err := initHome(dir); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte("{\"version\":\"0.1\",\"kept\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initHome(dir); err != nil {
		t.Fatal(err)
	}
	for _, sub := range paths.Subdirs {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err != nil || !fi.IsDir() {
			t.Fatalf("missing %s: %v", sub, err)
		}
	}
	b, _ := os.ReadFile(cfg)
	if string(b) != "{\"version\":\"0.1\",\"kept\":true}\n" {
		t.Fatalf("config.json was overwritten: %s", b)
	}
}
