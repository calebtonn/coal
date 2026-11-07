package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/manifest"
)

func newManifest(t *testing.T, s *Store, summary string) *manifest.Manifest {
	t.Helper()
	m := manifest.New(summary, manifest.SourceUser, manifest.Provenance{Tool: "test"})
	must(t, s.Create(m, Op{Tool: "hook", Action: "create"}))
	return m
}

func commitCount(t *testing.T, s *Store) int {
	t.Helper()
	out, err := s.git("rev-list", "--count", "HEAD")
	if err != nil {
		return 0 // no commits yet
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestDeferredHistory(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	s, err := Open(dir, WithDeferredHistory())
	must(t, err)
	if !s.Deferred() {
		t.Fatal("not deferred")
	}
	a := newManifest(t, s, "budget tracker")
	b := newManifest(t, s, "garden plan")
	_, err = s.Update(a.ID, 0, func(m *manifest.Manifest) error { m.State.NextStep = "csv"; return nil }, Op{})
	must(t, err)
	pending := filepath.Join(dir, pendingFile)
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("no pending marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("a deferred store set up git")
	}

	// A long quiet window: the writes are too recent.
	if ok, err := s.CommitPending("", time.Hour); ok || err != nil {
		t.Fatalf("CommitPending(1h) = %v, %v", ok, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("marker cleared without a commit")
	}

	// Quiet 0: one commit holding both manifests, marker gone.
	ok, err := s.CommitPending("", 0)
	if !ok || err != nil {
		t.Fatalf("CommitPending(0) = %v, %v", ok, err)
	}
	if n := commitCount(t, s); n != 1 {
		t.Fatalf("%d commits, want 1", n)
	}
	msg, _ := s.git("log", "-1", "--format=%s")
	files, _ := s.git("show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(msg, a.ID) || !strings.Contains(msg, b.ID) || !strings.Contains(files, a.ID) || !strings.Contains(files, b.ID) {
		t.Errorf("commit %q with files:\n%s", msg, files)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Errorf("marker still there: %v", err)
	}
	if ok, err := s.CommitPending("", 0); ok || err != nil {
		t.Errorf("second CommitPending = %v, %v", ok, err)
	}

	// A marker with nothing new to commit is cleared without a commit.
	s.markPending()
	if ok, err := s.CommitPending("explicit", 0); ok || err != nil || commitCount(t, s) != 1 {
		t.Errorf("empty pending: %v, %v", ok, err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("empty marker not cleared")
	}

	// An explicit message is used as given.
	_, err = s.Update(b.ID, 0, func(m *manifest.Manifest) error { m.Status = "paused"; return nil }, Op{})
	must(t, err)
	if ok, err := s.CommitPending("daemon: flush", 0); !ok || err != nil {
		t.Fatalf("explicit: %v, %v", ok, err)
	}
	if msg, _ := s.git("log", "-1", "--format=%s"); strings.TrimSpace(msg) != "daemon: flush" {
		t.Errorf("message %q", msg)
	}
}

func TestDeferredRunsNoGit(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	must(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"$@\" >> "+calls+"\nexit 1\n"), 0o700))
	t.Setenv("PATH", bin)
	s, err := Open(t.TempDir(), WithDeferredHistory())
	must(t, err)
	m := newManifest(t, s, "budget tracker")
	_, err = s.RegisterArtifact(m.ID, ArtifactInput{Content: []byte("<h1>x</h1>"), Title: "a.html"}, Op{})
	must(t, err)
	if b, err := os.ReadFile(calls); err == nil {
		t.Fatalf("git ran from a deferred store:\n%s", b)
	}
}

func TestNoGitOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	s, err := Open(dir, WithDeferredHistory())
	must(t, err)
	for range 3 {
		newManifest(t, s, "budget tracker")
	}
	if _, err := os.Stat(filepath.Join(dir, pendingFile)); !os.IsNotExist(err) {
		t.Errorf("pending marker without git: %v", err)
	}
	if ok, err := s.CommitPending("", 0); ok || err != nil {
		t.Errorf("CommitPending without git = %v, %v", ok, err)
	}
	plain, err := Open(dir)
	must(t, err)
	if ok, err := plain.CommitPending("", 0); ok || err != nil {
		t.Errorf("non-deferred without git = %v, %v", ok, err)
	}
}

func TestBatchMessage(t *testing.T) {
	var b strings.Builder
	for i := range 7 {
		b.WriteString(" M manifests/id" + string(rune('0'+i)) + ".json\n")
	}
	if got := batchMessage(b.String()); got != "coal: batched writes on id0, id1, id2, id3, id4 and 2 more" {
		t.Errorf("seven: %q", got)
	}
	if got := batchMessage("D  manifests/gone.json\n?? manifests/new.json\n"); got != "coal: batched writes on gone, new" {
		t.Errorf("two: %q", got)
	}
	if got := batchMessage(""); got != "coal: batched writes" {
		t.Errorf("none: %q", got)
	}
}
