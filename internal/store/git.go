package store

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const gitignore = "artifacts/\nindex.db*\nqueue/\nlogs/\n*.lock\n*.tmp\n"

// initGit makes the data directory a git repository once, with a local
// identity. Without git, history is turned off and one warning is logged.
func (s *Store) initGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		s.history = false
		s.Log("warning: git not found on PATH; manifest history is off")
		return nil
	}
	if err := writeIfMissing(filepath.Join(s.dir, ".gitignore"), gitignore); err != nil {
		return err
	}
	unlock, err := lock(filepath.Join(s.dir, ".git.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(s.dir, ".git")); err == nil {
		return nil
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "coal"},
		{"config", "user.email", "coal@localhost"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := s.git(args...); err != nil {
			return fmt.Errorf("set up history in %s: %w", s.dir, err)
		}
	}
	return nil
}

// git runs git in the data directory without the caller's GIT_* location
// variables, so a hook environment cannot redirect it.
func (s *Store) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", s.dir}, args...)...)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k != "GIT_DIR" && k != "GIT_WORK_TREE" && k != "GIT_INDEX_FILE" && k != "GIT_OBJECT_DIRECTORY" && k != "GIT_COMMON_DIR" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// commit records manifests/ and sessions/ under .git.lock, skipping it when
// nothing changed. Failures are logged: the write already landed.
func (s *Store) commit(msg string) {
	if !s.history {
		return
	}
	unlock, err := lock(filepath.Join(s.dir, ".git.lock"))
	if err != nil {
		s.Log("history: %v", err)
		return
	}
	defer unlock()
	if _, err = s.git("add", "-A", "manifests", "sessions"); err == nil {
		if _, err = s.git("diff", "--cached", "--quiet"); err == nil {
			return
		}
		_, err = s.git("commit", "-q", "--no-verify", "-m", msg)
	}
	if err != nil {
		s.Log("history: %v", err)
	}
}
