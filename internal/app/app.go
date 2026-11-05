// Package app ties the store and the search index together. Every writer
// (CLI, daemon, MCP server, capture) goes through it so the index follows
// the store. The manifest files are the truth: an index error after a
// successful store write is logged, not returned, and coal reindex repairs it.
package app

import (
	"path/filepath"

	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

// App is an open data directory: the store and its index.db.
type App struct {
	Store *store.Store
	Index *index.Index
}

// Open opens the store at home and its index. An empty index is rebuilt
// from manifests/, so a new or deleted index.db never hides manifests.
func Open(home string, opts ...store.Option) (*App, error) {
	s, err := store.Open(home, opts...)
	if err != nil {
		return nil, err
	}
	ix, err := index.Open(filepath.Join(s.Dir(), "index.db"))
	if err != nil {
		return nil, err
	}
	a := &App{Store: s, Index: ix}
	n, err := ix.Count()
	if err == nil && n == 0 {
		_, _, err = a.Reindex()
	}
	if err != nil {
		ix.Close()
		return nil, err
	}
	return a, nil
}

// Close closes the index.
func (a *App) Close() error { return a.Index.Close() }

func (a *App) upsert(m *manifest.Manifest) {
	if err := a.Index.Upsert(m); err != nil {
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
	if err := a.Index.Delete(id); err != nil {
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
	ms, errs := a.Store.List()
	for _, e := range errs {
		a.Store.Log("reindex: skipped: %v", e)
	}
	return len(ms), len(errs), a.Index.Rebuild(ms)
}
