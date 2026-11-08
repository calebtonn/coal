package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pendingFile marks writes that are on disk but not yet in the history.
const pendingFile = ".history-pending"

// WithDeferredHistory makes writes skip git (ADR-014: commits never run
// inside a hook). Each write touches <home>/.history-pending instead, and
// a later CommitPending, from the daemon tick or any non-hook command,
// records them. Open runs no git process in this mode either.
func WithDeferredHistory() Option { return func(s *Store) { s.deferred = true } }

// Deferred reports whether the store was opened with WithDeferredHistory.
func (s *Store) Deferred() bool { return s.deferred }

// markPending creates or refreshes the pending marker. Failures are
// logged: the write itself already landed.
func (s *Store) markPending() {
	if err := writeAtomic(filepath.Join(s.dir, pendingFile), nil); err != nil {
		s.Log("history: marking pending writes: %v", err)
	}
}

// CommitPending records deferred writes. Under .git.lock, when
// .history-pending exists and was last touched at least quiet ago, it
// commits manifests/ with msg, or with a default message naming up to
// five changed manifests, and clears the marker. It reports whether a
// commit was made. With history off, or git missing, it does nothing.
func (s *Store) CommitPending(msg string, quiet time.Duration) (bool, error) {
	if !s.history {
		return false, nil
	}
	pending := filepath.Join(s.dir, pendingFile)
	if _, err := os.Stat(pending); errors.Is(err, fs.ErrNotExist) {
		return false, nil // the common case, without taking the lock
	}
	unlock, err := lock(filepath.Join(s.dir, ".git.lock"))
	if err != nil {
		return false, err
	}
	defer unlock()
	st, err := os.Stat(pending)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if time.Since(st.ModTime()) < quiet {
		return false, nil
	}
	if err := s.initGitLocked(); err != nil {
		return false, err
	}
	// Claim the marker before adding: a write that lands from here on
	// makes a fresh marker and is picked up next time.
	claimed := pending + ".claimed"
	if err := os.Rename(pending, claimed); err != nil {
		return false, err
	}
	committed, err := s.commitClaimed(msg)
	if err != nil {
		// Put the claim back unless a newer marker already exists.
		if _, serr := os.Stat(pending); errors.Is(serr, fs.ErrNotExist) {
			os.Rename(claimed, pending)
		}
		return false, err
	}
	os.Remove(claimed)
	return committed, nil
}

func (s *Store) commitClaimed(msg string) (bool, error) {
	if _, err := s.git("add", "-A", "manifests"); err != nil {
		return false, err
	}
	if _, err := s.git("diff", "--cached", "--quiet", "--no-ext-diff"); err == nil {
		return false, nil // nothing changed since the last commit
	}
	if msg == "" {
		status, err := s.git("status", "--porcelain", "manifests")
		if err != nil {
			return false, err
		}
		msg = batchMessage(status)
	}
	if _, err := s.git("commit", "-q", "--no-verify", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// batchMessage names up to five manifests from git status --porcelain.
func batchMessage(status string) string {
	var ids []string
	for line := range strings.Lines(status) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		name := filepath.Base(f[len(f)-1])
		if id, ok := strings.CutSuffix(name, ".json"); ok {
			ids = append(ids, id)
		}
	}
	switch {
	case len(ids) == 0:
		return "coal: batched writes"
	case len(ids) > 5:
		return fmt.Sprintf("coal: batched writes on %s and %d more", strings.Join(ids[:5], ", "), len(ids)-5)
	}
	return "coal: batched writes on " + strings.Join(ids, ", ")
}
