package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// register writes body to dir/name and registers it, returning skipped.
func register(t *testing.T, s *Store, id, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	must(t, os.MkdirAll(filepath.Dir(p), 0o700))
	must(t, os.WriteFile(p, []byte(body), 0o600))
	a, err := s.RegisterArtifact(id, ArtifactInput{Path: p}, Op{})
	must(t, err)
	return a.Skipped
}

func TestDeniedNames(t *testing.T) {
	s, m := setup(t)
	dir := t.TempDir()
	for _, name := range []string{".netrc", ".npmrc", ".pypirc", ".envrc", "prod.env", ".git-credentials", "hosts.yml",
		"id_ecdsa", "id_dsa.pub", "vault.kdbx", ".pgpass", "release.jks", "app.keystore"} {
		if got := register(t, s, m.ID, dir, name, "harmless text"); !strings.Contains(got, "file name matches") {
			t.Errorf("%s: skipped %q", name, got)
		}
	}
	if got := register(t, s, m.ID, dir, "environment.md", "harmless"); got != "" {
		t.Errorf("environment.md skipped: %q", got)
	}
}

func TestTokenRules(t *testing.T) {
	s, m := setup(t)
	dir := t.TempDir()
	r := func(c string, n int) string { return strings.Repeat(c, n) }
	for rule, body := range map[string]string{
		"GitHub token":   "token: gho_" + r("a", 24) + "\nand ghs_" + r("B", 20),
		"npm token":      "//registry.npmjs.org/:_authToken=npm_" + r("x", 36),
		"PyPI token":     "password = pypi-" + r("A", 60),
		"GitLab token":   "glpat-" + r("z", 20),
		"Slack token":    "SLACK_APP=xapp-1-A0123456789-abcdef",
		"Google API key": "key=AIza" + r("9", 35),
		"JWT":            "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnopqrstu",
		"netrc password": "machine example.com login me password hunter2-secret\n",
	} {
		if got := register(t, s, m.ID, dir, rule+".txt", "notes\n"+body+"\n"); got != "possible secret: "+rule {
			t.Errorf("%s: skipped %q", rule, got)
		}
	}
	if got := register(t, s, m.ID, dir, "standalone.txt", "default\npassword s3cr3tvalue\n"); got != "possible secret: netrc password" {
		t.Errorf("standalone password line: %q", got)
	}
	for _, prose := range []string{"Reset your password here.\npassword reset flow\n", "ghs_short", "eyJ alone"} {
		if got := register(t, s, m.ID, dir, "prose.md", prose); got != "" {
			t.Errorf("prose %q skipped: %q", prose, got)
		}
	}
	// The whole body is scanned, not just the start.
	late := strings.Repeat("x", 3<<20) + "\nAKIAABCDEFGHIJKLMNOP\n"
	if got := register(t, s, m.ID, dir, "late.txt", late); got != "possible secret: AWS access key" {
		t.Errorf("token after 3 MiB: %q", got)
	}
}

func TestTooLarge(t *testing.T) {
	s, m := setup(t)
	big := strings.Repeat("a", MaxBody+1)
	if got := register(t, s, m.ID, t.TempDir(), "big.txt", big); got != "too large" {
		t.Errorf("file over the cap: %q", got)
	}
	a, err := s.RegisterArtifact(m.ID, ArtifactInput{Content: []byte(big), Title: "big2.txt"}, Op{})
	must(t, err)
	if a.Skipped != "too large" || a.Size != nil {
		t.Errorf("content over the cap: %+v", a)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Dir(), "artifacts")); len(entries) != 0 {
		t.Errorf("%d bodies stored", len(entries))
	}
}

func TestSymlinksAndSensitiveDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, m := setup(t)
	dir := t.TempDir()

	// An innocent link name pointing at .env is caught by the target's name.
	must(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("X=1"), 0o600))
	must(t, os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "notes.txt")))
	a, err := s.RegisterArtifact(m.ID, ArtifactInput{Path: filepath.Join(dir, "notes.txt")}, Op{})
	must(t, err)
	if !strings.Contains(a.Skipped, "matches .env") || !strings.HasSuffix(a.URI, "/notes.txt") {
		t.Errorf("link to .env: %+v", a)
	}

	// Anything under ~/.ssh, directly or through a link, is never read.
	must(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	must(t, os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host x"), 0o600))
	must(t, os.Symlink(filepath.Join(home, ".ssh", "config"), filepath.Join(dir, "readme.md")))
	for _, p := range []string{filepath.Join(home, ".ssh", "config"), filepath.Join(dir, "readme.md")} {
		a, err := s.RegisterArtifact(m.ID, ArtifactInput{Path: p}, Op{})
		must(t, err)
		if a.Skipped != "sensitive location: ~/.ssh" || a.Size != nil {
			t.Errorf("%s: %+v", p, a)
		}
	}
	if got := register(t, s, m.ID, filepath.Join(home, ".config", "gh"), "hosts.txt", "x"); got != "sensitive location: ~/.config/gh" {
		t.Errorf("~/.config/gh: %q", got)
	}
	a, err = s.RegisterArtifact(m.ID, ArtifactInput{Path: "/proc/self/status"}, Op{})
	must(t, err)
	if a.Skipped != "sensitive location: /proc" {
		t.Errorf("/proc: %+v", a)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Dir(), "artifacts")); len(entries) != 0 {
		t.Errorf("%d bodies stored", len(entries))
	}
}

func TestBlindHashSalt(t *testing.T) {
	s, m := setup(t)
	in := ArtifactInput{Content: []byte("AKIAABCDEFGHIJKLMNOP"), Title: "a.txt"}
	a1, err := s.RegisterArtifact(m.ID, in, Op{})
	must(t, err)
	cfg, _ := os.ReadFile(filepath.Join(s.Dir(), "config.json"))
	var c map[string]any
	must(t, json.Unmarshal(cfg, &c))
	salt, _ := c[saltKey].(string)
	if len(salt) != 64 || c["version"] != "0.1" {
		t.Fatalf("config.json: %s", cfg)
	}
	// A second store on the same directory reuses the salt, so the same
	// skipped body gets the same hash and stays one artifact.
	s2, err := Open(s.Dir(), WithHistory(false))
	must(t, err)
	a2, err := s2.RegisterArtifact(m.ID, in, Op{})
	must(t, err)
	got, _ := s.Get(m.ID)
	if a1.Hash != a2.Hash || len(got.Artifacts) != 1 {
		t.Errorf("hashes %s %s, %d artifacts", a1.Hash, a2.Hash, len(got.Artifacts))
	}
	for _, name := range []string{"coal.log"} {
		if b, _ := os.ReadFile(filepath.Join(s.Dir(), "logs", name)); bytes.Contains(b, []byte(salt)) {
			t.Errorf("salt in %s", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(s.Dir(), "manifests", m.ID+".json")); bytes.Contains(b, []byte(salt)) {
		t.Error("salt in the manifest")
	}
}
