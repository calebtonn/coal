// Package app ties the store and the search index together. Every writer
// (CLI, daemon, MCP server, capture) goes through it so the index follows
// the store. The manifest files are the truth: an index error after a
// successful store write is logged, not returned, and coal reindex repairs it.
package app

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

// App is an open data directory: the store and its index.db.
type App struct {
	Store *store.Store
	Index *index.Index
}

// Open opens the store at home and its index. Pass
// store.WithDeferredHistory() from hooks, so no write runs git. An empty index is rebuilt
// from manifests/, so a new or deleted index.db never hides manifests. A
// corrupt index.db is renamed to index.db.broken.<ts> and replaced.
func Open(home string, opts ...store.Option) (*App, error) {
	s, err := store.Open(home, opts...)
	if err != nil {
		return nil, err
	}
	// Non-hook callers commit what hooks left pending (ADR-014). A
	// failure is logged: the writes are safe on disk and the next flush
	// retries.
	if !s.Deferred() {
		if _, err := s.CommitPending("", 0); err != nil {
			s.Log("history: committing pending writes: %v", err)
		}
	}
	path := filepath.Join(s.Dir(), "index.db")
	ix, err := index.Open(path)
	if err != nil && index.IsCorrupt(err) {
		moved, merr := index.SetAside(path)
		if merr != nil {
			return nil, errors.Join(err, merr)
		}
		s.Log("index: %v; moved %v aside and starting a fresh index", err, moved)
		ix, err = index.Open(path)
	}
	if err != nil {
		return nil, err
	}
	a := &App{Store: s, Index: ix}
	if err := a.rebuildIfEmpty(); err != nil {
		ix.Close()
		return nil, err
	}
	return a, nil
}

// indexLock takes a flock on <home>/.index.lock: exclusive for a rebuild,
// shared for a single upsert or delete. A rebuild lists manifests and then
// replaces the whole index, so a write that lands between those two steps
// would be lost; the shared lock makes such a write wait until the rebuild
// is done, and then apply on top of it.
func (a *App) indexLock(exclusive bool) (func(), error) {
	f, err := os.OpenFile(filepath.Join(a.Store.Dir(), ".index.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

func (a *App) rebuildIfEmpty() error {
	if n, err := a.Index.Count(); err != nil || n > 0 {
		return err
	}
	unlock, err := a.indexLock(true)
	if err != nil {
		return err
	}
	defer unlock()
	if n, err := a.Index.Count(); err != nil || n > 0 { // another process rebuilt it
		return err
	}
	_, _, err = a.rebuild()
	return err
}

// Close closes the index.
func (a *App) Close() error { return a.Index.Close() }

func (a *App) upsert(m *manifest.Manifest) {
	unlock, err := a.indexLock(false)
	if err == nil {
		err = a.Index.Upsert(m)
		unlock()
	}
	if err != nil {
		a.Store.Log("index: upsert %s failed: %v; run coal reindex", m.ID, err)
	}
}

// Create writes a new manifest and indexes it.
func (a *App) Create(m *manifest.Manifest, op store.Op) error {
	if err := a.Store.Create(m, op); err != nil {
		return err
	}
	a.upsert(m)
	return nil
}

// Update applies fn through the store's rev check and reindexes the result.
func (a *App) Update(id string, expectedRev int, fn func(*manifest.Manifest) error, op store.Op) (*manifest.Manifest, error) {
	m, err := a.Store.Update(id, expectedRev, fn, op)
	if err != nil {
		return m, err
	}
	a.upsert(m)
	return m, nil
}

// RegisterArtifact registers an artifact and reindexes its manifest.
func (a *App) RegisterArtifact(id string, in store.ArtifactInput, op store.Op) (*manifest.Artifact, error) {
	art, err := a.Store.RegisterArtifact(id, in, op)
	if err != nil {
		return art, err
	}
	if m, err := a.Store.Get(id); err != nil {
		a.Store.Log("index: reading %s after registering an artifact failed: %v; run coal reindex", id, err)
	} else {
		a.upsert(m)
	}
	return art, nil
}

// Remove moves a manifest to the trash and drops it from the index. It
// returns the trash path.
func (a *App) Remove(id string, op store.Op) (string, error) {
	dst, err := a.Store.Remove(id, op)
	if err != nil {
		return dst, err
	}
	unlock, err := a.indexLock(false)
	if err == nil {
		err = a.Index.Delete(id)
		unlock()
	}
	if err != nil {
		a.Store.Log("index: delete %s failed: %v; run coal reindex", id, err)
	}
	return dst, nil
}

// Search runs an index search.
func (a *App) Search(q string, opts index.SearchOpts) ([]index.Result, error) {
	return a.Index.Search(q, opts)
}

// Get reads one manifest from the store.
func (a *App) Get(id string) (*manifest.Manifest, error) { return a.Store.Get(id) }

// Resolve turns an id or unique id prefix into an id.
func (a *App) Resolve(prefix string) (string, error) { return a.Store.Resolve(prefix) }

// Reindex rebuilds the index from manifests/ alone. Files that do not
// parse or validate are skipped and logged.
func (a *App) Reindex() (indexed, skipped int, err error) {
	unlock, err := a.indexLock(true)
	if err != nil {
		return 0, 0, err
	}
	defer unlock()
	return a.rebuild()
}

// rebuild lists and rebuilds; the caller holds the exclusive index lock.
func (a *App) rebuild() (indexed, skipped int, err error) {
	ms, errs := a.Store.List()
	for _, e := range errs {
		a.Store.Log("reindex: skipped: %v", e)
	}
	return len(ms), len(errs), a.Index.Rebuild(ms)
}
