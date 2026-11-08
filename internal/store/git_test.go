package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
)

func needGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestHistoryIsManifestsOnly(t *testing.T) {
	needGit(t)
	s, m := setup(t, WithHistory(true))
	must(t, s.WriteSession("claude-code:session:abc", map[string]string{"cwd": "/x"}))
	must(t, os.MkdirAll(filepath.Join(s.Dir(), "tmp"), 0o700))
	must(t, os.WriteFile(filepath.Join(s.Dir(), "tmp", "a.html"), []byte("x"), 0o600))
	_, err := s.Update(m.ID, 0, func(m *manifest.Manifest) error { m.State.NextStep = "n"; return nil }, Op{})
	must(t, err)
	tracked, err := s.git("ls-files")
	must(t, err)
	if strings.TrimSpace(tracked) != "manifests/"+m.ID+".json" {
		t.Fatalf("tracked:\n%s", tracked)
	}
	gi, _ := os.ReadFile(filepath.Join(s.Dir(), ".gitignore"))
	for _, l := range []string{"sessions/", "tmp/"} {
		if !strings.Contains(string(gi), l+"\n") {
			t.Errorf(".gitignore lacks %s:\n%s", l, gi)
		}
	}
	if hp, _ := s.git("config", "--local", "core.hooksPath"); strings.TrimSpace(hp) != os.DevNull {
		t.Errorf("core.hooksPath = %q", hp)
	}
}

func TestOldHistoryStopsTrackingSessions(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	s, err := Open(dir, WithHistory(true))
	must(t, err)
	// Recreate a data directory from before the change: sessions/ tracked,
	// and the old .gitignore.
	must(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("artifacts/\nindex.db*\nqueue/\nlogs/\n*.lock\n*.tmp"), 0o600))
	must(t, s.WriteSession("ref", map[string]string{"a": "b"}))
	_, err = s.git("add", "-A", ".gitignore", "sessions")
	must(t, err)
	_, err = s.git("commit", "-q", "-m", "old layout")
	must(t, err)

	s2, err := Open(dir, WithHistory(true))
	must(t, err)
	if out, _ := s2.git("ls-files", "sessions"); strings.TrimSpace(out) != "" {
		t.Fatalf("sessions still tracked: %s", out)
	}
	if _, err := os.Stat(s2.SessionPath("ref")); err != nil {
		t.Fatalf("session file removed from disk: %v", err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "*.tmp\nsessions/\ntmp/\n") {
		t.Errorf(".gitignore:\n%s", gi)
	}
	// Opening again changes nothing more.
	before, _ := s2.git("rev-list", "--count", "HEAD")
	_, err = Open(dir, WithHistory(true))
	must(t, err)
	if after, _ := s2.git("rev-list", "--count", "HEAD"); after != before {
		t.Errorf("second open committed again: %s then %s", before, after)
	}
}

func TestGitIgnoresHooksAndInjectedConfig(t *testing.T) {
	needGit(t)
	hooks := t.TempDir()
	ran := filepath.Join(t.TempDir(), "hook-ran")
	hook := "#!/bin/sh\ntouch " + ran + "\n"
	for _, h := range []string{"post-commit", "pre-commit", "reference-transaction"} {
		must(t, os.WriteFile(filepath.Join(hooks, h), []byte(hook), 0o700))
	}
	s, m := setup(t, WithHistory(true))
	// Point hooks at the evil directory three ways: local config, the
	// repo's own hooks dir, and GIT_CONFIG_* in the environment.
	_, err := s.git("config", "core.hooksPath", hooks)
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(s.Dir(), ".git", "hooks"), 0o700))
	must(t, os.WriteFile(filepath.Join(s.Dir(), ".git", "hooks", "post-commit"), []byte(hook), 0o700))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooks)
	t.Setenv("GIT_DIR", t.TempDir())
	_, err = s.Update(m.ID, 0, func(m *manifest.Manifest) error { m.State.NextStep = "n"; return nil }, Op{Tool: "t", Action: "x"})
	must(t, err)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a git hook ran")
	}
	if log, _ := s.git("log", "-1", "--format=%s"); strings.TrimSpace(log) != "t: x on "+m.ID {
		t.Fatalf("commit missing: %q", log)
	}
	for _, kv := range GitEnv() {
		if strings.HasPrefix(kv, "GIT_CONFIG") || strings.HasPrefix(kv, "GIT_DIR=") {
			t.Errorf("GitEnv kept %s", kv)
		}
	}
}
