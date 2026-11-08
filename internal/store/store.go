// Package store keeps Intent Manifests as JSON files with locks, rev
// compare-and-swap, atomic writes, a trash, and a local git history.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/paths"
)

var (
	ErrNotFound = errors.New("manifest not found")
	ErrExists   = errors.New("manifest already exists")
	ErrConflict = errors.New("rev conflict") // matched by *ConflictError
	// ErrNoChange, returned by an Update callback, skips the write.
	ErrNoChange = errors.New("no change")
)

// ConflictError reports that the manifest moved past the rev the caller read.
type ConflictError struct {
	ID                string
	Expected, Current int
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("manifest %s is at rev %d, not %d; re-read and try again", e.ID, e.Current, e.Expected)
}

func (e *ConflictError) Unwrap() error { return ErrConflict }

// Op names the writer; the commit message is "<tool>: <action> on <id>".
type Op struct {
	Tool   string
	Action string
}

func (o Op) message(id string) string {
	return fmt.Sprintf("%s: %s on %s", or(o.Tool, "coal"), or(o.Action, "write"), id)
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Option configures Open.
type Option func(*Store)

// WithHistory turns the git history on or off. It is on by default.
func WithHistory(on bool) Option { return func(s *Store) { s.history = on } }

// Store is one data directory, safe for concurrent goroutines and processes.
type Store struct {
	dir      string
	history  bool
	deferred bool

	saltMu  sync.Mutex
	saltVal []byte
}

// Open ensures the data directory layout and returns a store for it.
func Open(dir string, opts ...Option) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{dir: abs, history: true}
	for _, o := range opts {
		o(s)
	}
	for _, sub := range paths.Subdirs {
		if err := os.MkdirAll(filepath.Join(abs, sub), 0o700); err != nil {
			return nil, err
		}
	}
	if err := writeIfMissing(filepath.Join(abs, "config.json"), "{\"version\":\"0.1\"}\n"); err != nil {
		return nil, err
	}
	switch {
	case s.history && s.deferred:
		s.findGit() // no git process: setup waits for CommitPending
	case s.history:
		return s, s.initGit()
	}
	return s, nil
}

// Dir returns the data directory.
func (s *Store) Dir() string { return s.dir }

func writeIfMissing(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	return errors.Join(err, f.Close())
}

// Log appends one line to logs/coal.log. Failures are ignored.
func (s *Store) Log(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(s.dir, "logs", "coal.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err == nil {
		fmt.Fprintf(f, "%s %s\n", manifest.Now(), fmt.Sprintf(format, args...))
		f.Close()
	}
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%q is not a manifest id (lowercase UUIDv7)", id)
	}
	return nil
}

func (s *Store) manifestPath(id string) string {
	return filepath.Join(s.dir, "manifests", id+".json")
}

// lock takes an exclusive flock on path and returns the unlock func.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// locked runs fn under manifests/.<id>.lock and commits if it succeeds.
func (s *Store) locked(id string, op Op, fn func() error) error {
	if err := checkID(id); err != nil {
		return err
	}
	unlock, err := lock(filepath.Join(s.dir, "manifests", "."+id+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err := fn(); err != nil {
		return err
	}
	s.commit(op.message(id))
	return nil
}

// writeAtomic writes a temp file in the same directory and renames it.
func writeAtomic(path string, data []byte) error {
	dir, base := filepath.Split(path)
	f, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func (s *Store) read(id string) (*manifest.Manifest, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.manifestPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return nil, err
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("manifests/%s.json: %w", id, err)
	}
	if m.ID != id {
		return nil, fmt.Errorf("manifests/%s.json holds id %q; the file name and the id must match", id, m.ID)
	}
	return m, nil
}

// write sets rev and updatedAt, validates, and writes; the caller holds the lock.
func (s *Store) write(m *manifest.Manifest, rev int) error {
	m.Rev, m.UpdatedAt = rev, manifest.Now()
	data, err := m.Marshal()
	if err != nil {
		return err
	}
	if err := manifest.Validate(data); err != nil {
		return fmt.Errorf("manifest %s is not valid:\n%w", m.ID, err)
	}
	return writeAtomic(s.manifestPath(m.ID), data)
}

// Create writes a new manifest at rev 1.
func (s *Store) Create(m *manifest.Manifest, op Op) error {
	return s.locked(m.ID, op, func() error {
		if _, err := os.Stat(s.manifestPath(m.ID)); err == nil {
			return fmt.Errorf("%w: %s", ErrExists, m.ID)
		}
		return s.write(m, 1)
	})
}

// Get reads one manifest.
func (s *Store) Get(id string) (*manifest.Manifest, error) { return s.read(id) }

// List returns valid manifests in id order; bad files are reported in errs.
func (s *Store) List() ([]*manifest.Manifest, []error) {
	ids, err := s.ids()
	if err != nil {
		return nil, []error{err}
	}
	var out []*manifest.Manifest
	var errs []error
	for _, id := range ids {
		if m, err := s.read(id); err != nil {
			errs = append(errs, err)
		} else {
			out = append(out, m)
		}
	}
	return out, errs
}

// ids returns the base names of manifests/*.json in sorted order.
func (s *Store) ids() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "manifests"))
	var ids []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && !strings.HasPrefix(name, ".") {
			ids = append(ids, name)
		}
	}
	return ids, err
}

// Resolve turns a unique id prefix of at least 8 characters into an id.
func (s *Store) Resolve(prefix string) (string, error) {
	prefix = strings.ToLower(prefix)
	if len(prefix) < 8 {
		return "", fmt.Errorf("id prefix %q is too short; give at least 8 characters", prefix)
	}
	ids, err := s.ids()
	ids = slices.DeleteFunc(ids, func(id string) bool { return !strings.HasPrefix(id, prefix) })
	switch {
	case err != nil:
		return "", err
	case len(ids) == 0:
		return "", fmt.Errorf("%w: %s", ErrNotFound, prefix)
	case len(ids) > 1:
		return "", fmt.Errorf("id prefix %s matches %d manifests", prefix, len(ids))
	}
	return ids[0], nil
}

// Update re-reads under the lock, checks expectedRev (0 skips the check),
// applies fn and writes with rev incremented.
func (s *Store) Update(id string, expectedRev int, fn func(*manifest.Manifest) error, op Op) (*manifest.Manifest, error) {
	var m *manifest.Manifest
	err := s.locked(id, op, func() error {
		var err error
		if m, err = s.read(id); err != nil {
			return err
		}
		if expectedRev > 0 && m.Rev != expectedRev {
			return &ConflictError{ID: id, Expected: expectedRev, Current: m.Rev}
		}
		rev := m.Rev
		if err := fn(m); err != nil {
			if errors.Is(err, ErrNoChange) {
				if m, err = s.read(id); err == nil {
					err = ErrNoChange
				}
			}
			return err
		}
		m.ID = id
		return s.write(m, rev+1)
	})
	if err != nil && !errors.Is(err, ErrNoChange) {
		return nil, err
	}
	return m, nil
}

// Remove moves the manifest to trash/<id>.<timestamp>.json and returns that path.
func (s *Store) Remove(id string, op Op) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dst := filepath.Join(s.dir, "trash", id+"."+stamp+".json")
	err := s.locked(id, op, func() error {
		err := os.Rename(s.manifestPath(id), dst)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return dst, nil
}

var unsafeRef = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SessionPath returns sessions/<ref>.json with ref reduced to
// [A-Za-z0-9._-]; other characters become "_".
func (s *Store) SessionPath(ref string) string {
	return filepath.Join(s.dir, "sessions", unsafeRef.ReplaceAllString(ref, "_")+".json")
}

// ReadSession decodes the scratch file for ref into v. A missing file
// returns an error matching fs.ErrNotExist.
func (s *Store) ReadSession(ref string, v any) error {
	data, err := os.ReadFile(s.SessionPath(ref))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteSession writes the scratch file for ref atomically. It does not
// commit; the next manifest write picks it up.
func (s *Store) WriteSession(ref string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.SessionPath(ref), append(data, '\n'))
}
