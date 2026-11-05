package store

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitignoreLines keep everything but manifests/ out of the history.
var gitignoreLines = []string{"artifacts/", "index.db*", "queue/", "logs/", "sessions/", "tmp/", "*.lock", "*.tmp"}

// initGit makes the data directory a git repository once, with a local
// identity and no hooks. Without git, history is turned off and one
// warning is logged. A data directory from before sessions/ left the
// history gets the new ignore lines and stops tracking sessions/ once;
// the files stay on disk.
func (s *Store) initGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		s.history = false
		s.Log("warning: git not found on PATH; manifest history is off")
		return nil
	}
	unlock, err := lock(filepath.Join(s.dir, ".git.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	added, err := s.ensureGitignore()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(s.dir, ".git")); err == nil {
		if added {
			s.untrackSessions()
		}
		return nil
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "coal"},
		{"config", "user.email", "coal@localhost"}, {"config", "commit.gpgsign", "false"},
		{"config", "core.hooksPath", os.DevNull}} {
		if _, err := s.git(args...); err != nil {
			return fmt.Errorf("set up history in %s: %w", s.dir, err)
		}
	}
	return nil
}

// ensureGitignore appends any missing line and reports whether it did.
func (s *Store) ensureGitignore() (bool, error) {
	p := filepath.Join(s.dir, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	have := map[string]bool{}
	for l := range strings.Lines(string(data)) {
		have[strings.TrimSpace(l)] = true
	}
	var missing []string
	for _, l := range gitignoreLines {
		if !have[l] {
			missing = append(missing, l)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	data = append(data, strings.Join(missing, "\n")+"\n"...)
	return true, writeAtomic(p, data)
}

// untrackSessions removes sessions/ from the index of an existing history.
func (s *Store) untrackSessions() {
	s.git("config", "core.hooksPath", os.DevNull)
	if out, err := s.git("ls-files", "sessions"); err != nil || strings.TrimSpace(out) == "" {
		return
	}
	if _, err := s.git("rm", "--cached", "-r", "-q", "sessions"); err == nil {
		_, err = s.git("commit", "-q", "--no-verify", "-m", "coal: stop tracking sessions/ (scratch, not history)")
		if err != nil {
			s.Log("history: %v", err)
		}
	}
}

// GitEnv is the environment for git without variables that could point it
// at another repository or inject configuration: the GIT_DIR family and
// every GIT_CONFIG* variable (GLOBAL, SYSTEM, COUNT, KEY_n, VALUE_n,
// PARAMETERS).
func GitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "GIT_CONFIG"):
		case k == "GIT_DIR", k == "GIT_WORK_TREE", k == "GIT_INDEX_FILE", k == "GIT_OBJECT_DIRECTORY",
			k == "GIT_ALTERNATE_OBJECT_DIRECTORIES", k == "GIT_COMMON_DIR", k == "GIT_NAMESPACE":
		default:
			env = append(env, kv)
		}
	}
	return env
}

// GitArgs prefixes args with the options every COAL git call uses: run
// in dir, and never run hooks.
func GitArgs(dir string, args ...string) []string {
	return append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull}, args...)
}

// git runs git in the data directory with GitEnv and GitArgs.
func (s *Store) git(args ...string) (string, error) {
	cmd := exec.Command("git", GitArgs(s.dir, args...)...)
	cmd.Env = GitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// commit records manifests/ under .git.lock, skipping it when nothing
// changed. Failures are logged: the write already landed. Writers that
// finish close together can share one commit.
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
	if _, err = s.git("add", "-A", "manifests"); err == nil {
		if _, err = s.git("diff", "--cached", "--quiet"); err == nil {
			return
		}
		_, err = s.git("commit", "-q", "--no-verify", "-m", msg)
	}
	if err != nil {
		s.Log("history: %v", err)
	}
}
