package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
)

func TestAddShowRm(t *testing.T) {
	home, page := t.TempDir(), filepath.Join(t.TempDir(), "budget.html")
	os.WriteFile(page, []byte("<h1>Budget</h1>"), 0o600)
	run := func(args ...string) string {
		var out bytes.Buffer
		root := rootCmd()
		root.SetOut(&out)
		root.SetArgs(append([]string{"--home", home}, args...))
		if err := root.Execute(); err != nil {
			t.Fatalf("coal %v: %v", args, err)
		}
		return strings.TrimSpace(out.String())
	}
	id := run("add", "--intent", "Household budget tracker", "--artifact", page)
	var m manifest.Manifest
	if err := json.Unmarshal([]byte(run("show", id[:8])), &m); err != nil || m.ID != id || m.Intent.Source != "user" ||
		len(m.Artifacts) != 1 || len(m.Artifacts[0].Versions) != 1 {
		t.Fatalf("unexpected manifest %+v: %v", m, err)
	}
	if dst := run("rm", id); filepath.Base(filepath.Dir(dst)) != "trash" {
		t.Fatalf("rm moved to %q", dst)
	}
}
