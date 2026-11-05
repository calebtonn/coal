// Package index keeps a SQLite FTS5 index of Intent Manifests. It is
// derived data: Rebuild recreates it from manifests alone.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"
	"time"

	"modernc.org/sqlite" // also registers the "sqlite" driver

	"github.com/calebtonn/coal/internal/manifest"
)

// schemaVersion changes whenever the tables below change. A mismatch
// drops and recreates everything, since the index is derived.
const schemaVersion = "2"

const schema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS docs(
	id TEXT PRIMARY KEY, summary TEXT, source TEXT, status TEXT,
	last_activity_at TEXT, updated_at TEXT, artifact_count INTEGER, next_step TEXT, rev INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(
	id UNINDEXED, summary, goal, tags, artifacts, next_step, state_summary,
	tokenize='porter unicode61');
`

// Index is an open index.db. It is safe for concurrent use, and several
// processes may open the same file: WAL and a busy timeout cover that.
type Index struct {
	db *sql.DB
}

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	ix := &Index{db: db}
	if err := ix.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open index %s: %w", path, err)
	}
	return ix, nil
}

func (ix *Index) migrate() error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var v string
	err = tx.QueryRow(`SELECT value FROM meta WHERE key = 'schema'`).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "no such table") {
		return err
	}
	if v != schemaVersion {
		for _, t := range []string{"meta", "docs", "fts"} {
			if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES('schema', ?)`, schemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// IsCorrupt reports whether err from Open means the file is not a usable
// database (SQLITE_CORRUPT or SQLITE_NOTADB), as opposed to a busy or
// permission error that a retry or the user should handle.
func IsCorrupt(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	code := se.Code() & 0xff // primary code without the extended bits
	return code == 11 || code == 26
}

// SetAside renames path and its -wal and -shm files to
// <name>.broken.<stamp>, so a fresh index can be created in its place.
// Nothing is deleted. It returns the new names.
func SetAside(path string) ([]string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	var moved []string
	for _, sfx := range []string{"", "-wal", "-shm"} {
		dst := path + ".broken." + stamp + sfx
		err := os.Rename(path+sfx, dst)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return moved, err
		}
		moved = append(moved, dst)
	}
	return moved, nil
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// Count returns the number of indexed manifests.
func (ix *Index) Count() (int, error) {
	var n int
	err := ix.db.QueryRow(`SELECT count(*) FROM docs`).Scan(&n)
	return n, err
}

// IDs returns every indexed manifest id in sorted order.
func (ix *Index) IDs() ([]string, error) {
	rows, err := ix.db.Query(`SELECT id FROM docs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Upsert adds or replaces one manifest. It does nothing when the index
// already holds a newer rev, so a slow writer cannot replace a newer row.
func (ix *Index) Upsert(m *manifest.Manifest) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsert(tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes one manifest. Deleting an id that is not there is not an error.
func (ix *Index) Delete(id string) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := remove(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Rebuild clears the index and adds ms, in one transaction.
func (ix *Index) Rebuild(ms []*manifest.Manifest) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM docs; DELETE FROM fts;`); err != nil {
		return err
	}
	for _, m := range ms {
		if err := upsert(tx, m); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func remove(tx *sql.Tx, id string) error {
	if _, err := tx.Exec(`DELETE FROM fts WHERE rowid IN (SELECT rowid FROM docs WHERE id = ?)`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM docs WHERE id = ?`, id)
	return err
}

func upsert(tx *sql.Tx, m *manifest.Manifest) error {
	var rev int
	err := tx.QueryRow(`SELECT coalesce(rev, 0) FROM docs WHERE id = ?`, m.ID).Scan(&rev)
	if err == nil && rev > m.Rev {
		return nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := remove(tx, m.ID); err != nil {
		return err
	}
	source := m.Intent.Source
	if source == "" {
		source = manifest.SourceModel // the schema default
	}
	res, err := tx.Exec(`INSERT INTO docs(id, summary, source, status, last_activity_at, updated_at, artifact_count, next_step, rev)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Intent.Summary, source, m.Status, utcSeconds(m.State.LastActivityAt), utcSeconds(m.UpdatedAt),
		len(m.Artifacts), m.State.NextStep, m.Rev)
	if err != nil {
		return err
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	var titles []string
	for _, a := range m.Artifacts {
		if a.Title != "" {
			titles = append(titles, a.Title)
		}
	}
	_, err = tx.Exec(`INSERT INTO fts(rowid, id, summary, goal, tags, artifacts, next_step, state_summary)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, rowid, m.ID,
		clean(m.Intent.Summary), clean(m.Intent.Goal), clean(strings.Join(m.Intent.Tags, " ")),
		clean(strings.Join(titles, "\n")), clean(m.State.NextStep), clean(m.State.Summary))
	return err
}

// utcSeconds normalises an RFC 3339 timestamp to UTC with second
// precision, so ORDER BY on the text sorts by time. Unparseable values
// are kept as they are.
func utcSeconds(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format(time.RFC3339)
}

// Snippet markers. They are private-use characters, stripped from indexed
// text by clean, so after HTML-escaping a snippet they are the only places
// <b> and </b> can come from.
const (
	markOpen  = "\ue000"
	markClose = "\ue001"
)

var cleaner = strings.NewReplacer(markOpen, "", markClose, "")

func clean(s string) string { return cleaner.Replace(s) }

var unmark = strings.NewReplacer(markOpen, "<b>", markClose, "</b>")

// safeSnippet HTML-escapes FTS5 snippet output and turns the markers into
// <b> and </b>, so <b> is the only markup in the result.
func safeSnippet(s string) string { return unmark.Replace(html.EscapeString(s)) }
