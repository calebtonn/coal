// Package marker anchors intents to project directories (ADR-013): the
// .coal marker file, git root and remote discovery, and matching a
// session's working directory to a manifest (#4).
package marker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/google/uuid"
)

// FileName is the marker file name.
const FileName = ".coal"

// SchemaURL is the marker's $schema.
const SchemaURL = "https://coalmine.io/schema/coal-marker/0.1.json"

// Context kinds written and matched here.
const (
	KindPath = "path"
	KindRepo = "repo"
)

type markerFile struct {
	Schema    string   `json:"$schema"`
	Manifest  string   `json:"manifest"`
	Manifests []string `json:"manifests,omitempty"`
}

// resolve makes p absolute and resolves symlinks, falling back to the
// cleaned absolute path when p does not exist.
func resolve(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// ProjectRoot returns the git top level at or above cwd, found by
// walking up to a .git file or directory, or the resolved cwd when
// there is no repo. A repo whose top level is unanchored, such as a
// dotfiles repo at $HOME, is treated as no repo: the root is the
// resolved cwd when that is anchored itself.
func ProjectRoot(cwd string) string {
	start := resolve(cwd)
	for dir := start; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			if Unanchored(dir) && !Unanchored(start) {
				return start
			}
			return dir
		}
		if dir == filepath.Dir(dir) {
			return start
		}
	}
}

// commonDir returns the git directory holding config and info/ for the
// repo at root, following a .git file (worktrees, submodules) and its
// commondir. It is "" when root has no .git.
func commonDir(root string) string {
	dotgit := filepath.Join(root, ".git")
	fi, err := os.Stat(dotgit)
	if err != nil {
		return ""
	}
	gitdir := dotgit
	if !fi.IsDir() {
		b, err := os.ReadFile(dotgit)
		if err != nil {
			return ""
		}
		line, _, _ := strings.Cut(string(b), "\n")
		p, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		if !ok {
			return ""
		}
		gitdir = strings.TrimSpace(p)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(root, gitdir)
		}
	}
	if b, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common := strings.TrimSpace(string(b))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
		gitdir = common
	}
	if fi, err := os.Stat(gitdir); err != nil || !fi.IsDir() {
		return ""
	}
	return gitdir
}

// Unanchored reports whether dir is too broad to anchor an intent: the
// filesystem root, the home directory, or any directory above home.
func Unanchored(dir string) bool {
	dir = resolve(dir)
	if dir == filepath.Dir(dir) {
		return true
	}
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return false
	}
	return under(resolve(h), dir)
}

// OriginRemote returns the normalized url of remote "origin" for the
// repo at root, or "" when there is none.
func OriginRemote(root string) string {
	gd := commonDir(root)
	if gd == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(gd, "config"))
	if err != nil {
		return ""
	}
	in := false
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = strings.EqualFold(strings.Join(strings.Fields(line), " "), `[remote "origin"]`)
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "url") {
			return NormalizeRemote(strings.Trim(strings.TrimSpace(v), `"`))
		}
	}
	return ""
}

// NormalizeRemote turns git@host:owner/repo, ssh://git@host/owner/repo
// and https://host/owner/repo into host/owner/repo, with the host
// lowercased, any port and user dropped, and a .git suffix or trailing
// slash removed. Local paths are only trimmed.
func NormalizeRemote(url string) string {
	u := strings.TrimSpace(url)
	if u == "" {
		return ""
	}
	var host, path string
	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		if strings.EqualFold(scheme, "file") {
			return trimRepo(rest)
		}
		host, path, _ = strings.Cut(rest, "/")
		host = host[strings.LastIndex(host, "@")+1:]
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
	} else if i := strings.Index(u, ":"); i > 0 && !strings.Contains(u[:i], "/") {
		host, path = u[:i], u[i+1:]
		host = host[strings.LastIndex(host, "@")+1:]
	} else {
		return trimRepo(u)
	}
	path = trimRepo(strings.TrimLeft(path, "/"))
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}

func trimRepo(p string) string {
	p = strings.TrimRight(p, "/")
	return strings.TrimRight(strings.TrimSuffix(p, ".git"), "/")
}

// validID reports whether id is a UUIDv7.
func validID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.Version() == 7 && len(id) == 36
}

// Find returns the manifest id in the nearest valid .coal marker at or
// above cwd, and the directory holding it. Only regular files count, and
// nothing at or above the home directory is read, so the data directory
// $HOME/.coal or a stray file there never matches.
// Markers that do not parse or hold no UUIDv7 are skipped.
func Find(cwd string) (manifestID, markerDir string, ok bool) {
	walk(cwd, func(id, dir string) bool {
		manifestID, markerDir, ok = id, dir, true
		return false
	})
	return
}

// walk calls fn for each valid marker from cwd upward, nearest first,
// until fn returns false. It stops below the home directory and the
// filesystem root, so a .coal there is never read (ADR-013).
func walk(cwd string, fn func(id, dir string) bool) {
	for dir := resolve(cwd); ; dir = filepath.Dir(dir) {
		if Unanchored(dir) {
			return
		}
		if id := read(filepath.Join(dir, FileName)); id != "" && !fn(id, dir) {
			return
		}
		if dir == filepath.Dir(dir) {
			return
		}
	}
}

// read returns the active manifest id in a marker file, or "".
func read(path string) string {
	m, _ := readFile(path)
	return m.Manifest
}

// readFile decodes a marker. ok is false when the file is missing, not a
// regular file, too big, or holds no valid active id.
func readFile(path string) (markerFile, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 64<<10 {
		return markerFile{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return markerFile{}, false
	}
	var m markerFile
	if json.Unmarshal(b, &m) != nil || !validID(m.Manifest) {
		return markerFile{}, false
	}
	return m, true
}

// Read returns the marker at root/.coal: the active id and every id it
// lists, the active one included, without duplicates. ok is false when
// there is no valid marker there.
func Read(root string) (active string, ids []string, ok bool) {
	m, ok := readFile(filepath.Join(root, FileName))
	if !ok {
		return "", nil, false
	}
	return m.Manifest, listWith(m.Manifests, m.Manifest), true
}

// listWith returns ids with id appended, keeping order, dropping
// duplicates and anything that is not a UUIDv7.
func listWith(ids []string, id string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range append(append([]string{}, ids...), id) {
		if validID(x) && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// Activate rewrites root/.coal so active is the manifest id that
// matching uses, keeping every id the marker listed and adding active
// to the list (ADR-013). It is the one deliberate rewrite of a marker,
// done with a temp file and a rename. root must already hold a valid
// marker; an unreadable .coal is left alone.
func Activate(root, active string) error {
	if !validID(active) {
		return fmt.Errorf("marker: %q is not a UUIDv7", active)
	}
	path := filepath.Join(root, FileName)
	m, ok := readFile(path)
	if !ok {
		return fmt.Errorf("marker %s: missing or not a COAL marker; not rewriting it", path)
	}
	ids := listWith(listWith(m.Manifests, m.Manifest), active)
	b, _ := json.MarshalIndent(markerFile{Schema: SchemaURL, Manifest: active, Manifests: ids}, "", "  ")
	f, err := os.CreateTemp(root, ".coal-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Chmod(0o644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	return exclude(root)
}

// ErrOtherManifest means a marker already exists for another manifest.
var ErrOtherManifest = errors.New("marker points at another manifest")

// Write creates root/.coal for manifest id without replacing any
// existing file, and lists .coal in root/.git/info/exclude when root is
// a repo with a .git directory. Writing the same id again is a no-op.
func Write(root, id string) error {
	if !validID(id) {
		return fmt.Errorf("marker: %q is not a UUIDv7", id)
	}
	if Unanchored(root) {
		return fmt.Errorf("marker: %s is the home directory, above it, or the root; not writing a marker there", root)
	}
	path := filepath.Join(root, FileName)
	b, _ := json.MarshalIndent(markerFile{Schema: SchemaURL, Manifest: id}, "", "  ")
	// O_EXCL on the final name never replaces a marker someone else
	// wrote, and needs no hard links.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		if got := read(path); got != id {
			return fmt.Errorf("marker %s: %w (%q)", path, ErrOtherManifest, got)
		}
		return exclude(root)
	}
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	return exclude(root)
}

// exclude appends .coal to the repo's info/exclude unless a line already
// names it. For a worktree that is the main repo's info/exclude, which
// git reads for every worktree.
func exclude(root string) error {
	gd := commonDir(root)
	if gd == "" {
		return nil
	}
	info := filepath.Join(gd, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return err
	}
	path := filepath.Join(info, "exclude")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l == FileName || l == "/"+FileName {
			return nil
		}
	}
	add := FileName + "\n"
	if len(b) > 0 && b[len(b)-1] != '\n' {
		add = "\n" + add
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(add)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Contexts returns the contexts to write when a manifest is created
// from cwd: the project root as a path, and the origin remote as a repo
// when there is one.
func Contexts(cwd string) []manifest.Context {
	root := ProjectRoot(cwd)
	ctx := []manifest.Context{{Kind: KindPath, Key: root, Label: filepath.Base(root)}}
	if r := OriginRemote(root); r != "" {
		ctx = append(ctx, manifest.Context{Kind: KindRepo, Key: r})
	}
	return ctx
}
