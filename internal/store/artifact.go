package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/calebtonn/coal/internal/manifest"
)

// ArtifactInput describes one body to register. Set Path or Content.
type ArtifactInput struct {
	Path                   string
	Content                []byte
	URI, Title, Type, MIME string
	Producer               manifest.Provenance
}

// fileTypes lists the extensions COAL classifies, each with its artifact
// type and MIME. init registers every MIME with the mime package, so the
// host's mime.types never decides them (#22): left to the host, .md is
// text/plain on macOS and .ts is an MPEG transport stream. Every text
// format gets a text/ type, so readers treat it as text; JSON and XML
// keep the application/ types readers already know. The mime package
// adds charset=utf-8 to each text/ type.
var fileTypes = []struct{ typ, mime, exts string }{
	{"html", "text/html", "html htm"},
	{"doc", "text/markdown", "md markdown"},
	{"doc", "text/plain", "txt"},
	{"doc", "text/x-rst", "rst"},
	{"data", "application/json", "json"},
	{"data", "text/csv", "csv"},
	{"data", "text/yaml", "yaml yml"},
	{"data", "application/xml", "xml"},
	{"data", "text/tab-separated-values", "tsv"},
	{"image", "image/png", "png"},
	{"image", "image/jpeg", "jpg jpeg"},
	{"image", "image/gif", "gif"},
	{"image", "image/svg+xml", "svg"},
	{"image", "image/webp", "webp"},
	{"code", "text/x-go", "go"},
	{"code", "text/javascript", "js mjs cjs"},
	{"code", "text/typescript", "ts tsx"},
	{"code", "text/jsx", "jsx"},
	{"code", "text/x-python", "py"},
	{"code", "text/x-ruby", "rb"},
	{"code", "text/x-rust", "rs"},
	{"code", "text/x-java", "java"},
	{"code", "text/x-kotlin", "kt"},
	{"code", "text/x-c", "c h"},
	{"code", "text/x-c++", "cc cpp hpp"},
	{"code", "text/x-csharp", "cs"},
	{"code", "text/x-swift", "swift"},
	{"code", "text/x-shellscript", "sh bash zsh"},
	{"code", "text/css", "css"},
	{"code", "text/x-scss", "scss"},
	{"code", "text/x-sql", "sql"},
	{"code", "text/x-php", "php"},
	{"code", "text/x-lua", "lua"},
	{"code", "text/x-svelte", "svelte"},
	{"code", "text/x-vue", "vue"},
	{"code", "text/x-toml", "toml"},
}

var typeByExt = map[string]string{}

func init() {
	for _, f := range fileTypes {
		for e := range strings.FieldsSeq(f.exts) {
			typeByExt["."+e] = f.typ
			if err := mime.AddExtensionType("."+e, f.mime); err != nil {
				panic(err)
			}
		}
	}
}

// RegisterArtifact records a body on a manifest. Identity is (manifest,
// uri), where a new body appends a version, or the hash when there is no
// uri. Possible secrets are recorded with skipped set and never copied.
func (s *Store) RegisterArtifact(manifestID string, in ArtifactInput, op Op) (*manifest.Artifact, error) {
	// Check the manifest first, so a bad id never leaves an orphan body.
	if err := checkID(manifestID); err != nil {
		return nil, err
	}
	if _, err := os.Stat(s.manifestPath(manifestID)); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, manifestID)
	} else if err != nil {
		return nil, err
	}
	// Work out the body, or why it is not copied. skipSource is what the
	// blind hash covers when there is no body to read.
	body, skipped, realName := in.Content, "", ""
	var skipSource io.Reader
	if len(body) > MaxBody {
		skipped = "too large"
	}
	if in.Path != "" {
		abs, err := filepath.Abs(in.Path)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, err
		}
		realName = filepath.Base(real)
		if skipped = sensitiveLocation(abs, real); skipped != "" {
			skipSource = strings.NewReader("path:" + real) // never read the file
		} else if st, err := os.Stat(real); err != nil {
			return nil, err
		} else if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", in.Path)
		} else if st.Size() > MaxBody {
			skipped = "too large"
			f, err := os.Open(real)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			skipSource = f
		} else if body, err = os.ReadFile(real); err != nil {
			return nil, err
		}
		if in.URI == "" {
			in.URI = (&url.URL{Scheme: "file", Path: abs}).String()
		}
		if in.Title == "" {
			in.Title = filepath.Base(abs)
		}
	}
	in.URI = CanonicalURI(in.URI)
	if u, err := url.Parse(in.URI); err == nil && in.Title == "" && strings.Trim(u.Path, "/") != "" {
		in.Title = path.Base(u.Path)
	}
	in.Title = or(in.Title, "untitled")
	ext := strings.ToLower(filepath.Ext(or(in.Path, in.Title)))
	in.Type = or(in.Type, or(typeByExt[ext], "other"))
	if in.MIME == "" {
		in.MIME = guessMIME(ext, body)
	}
	if _, _, err := mime.ParseMediaType(in.MIME); err != nil {
		return nil, fmt.Errorf("artifact mime %q is not a media type: %w", in.MIME, err)
	}
	in.Producer.Tool = or(in.Producer.Tool, or(op.Tool, "coal"))
	op.Action = or(op.Action, "register artifact "+in.Title)

	if skipped == "" {
		skipped = secretRule(in, body, realName)
	}
	v := manifest.ArtifactVersion{CreatedAt: manifest.Now(), Producer: in.Producer, Skipped: skipped}
	if skipped == "" {
		sum := sha256.Sum256(body)
		hexHash := hex.EncodeToString(sum[:])
		if err := s.storeBody(hexHash, body); err != nil {
			return nil, err
		}
		size := int64(len(body))
		v.Hash, v.Size = "sha256:"+hexHash, &size
	} else {
		// No real hash or size for a body that was not copied.
		if skipSource == nil {
			skipSource = bytes.NewReader(body)
		}
		h, err := s.blindHash(skipSource)
		if err != nil {
			return nil, err
		}
		v.Hash = h
	}

	var out manifest.Artifact
	_, err := s.Update(manifestID, 0, func(m *manifest.Manifest) error {
		for i := range m.Artifacts {
			a := &m.Artifacts[i]
			if in.URI != "" && CanonicalURI(a.URI) == in.URI {
				if a.Hash == v.Hash {
					out = *a
					return ErrNoChange
				}
				if len(a.Versions) == 0 {
					a.Versions = []manifest.ArtifactVersion{{Hash: a.Hash, Size: a.Size, CreatedAt: a.CreatedAt, Producer: a.Producer, Skipped: a.Skipped}}
				}
				a.Versions = append(a.Versions, v)
				a.Hash, a.Size, a.CreatedAt, a.Producer, a.Skipped = v.Hash, v.Size, v.CreatedAt, v.Producer, v.Skipped
				out = *a
				return nil
			}
			if in.URI == "" && a.URI == "" && a.Hash == v.Hash {
				out = *a
				return ErrNoChange
			}
		}
		out = manifest.Artifact{
			Hash: v.Hash, Title: in.Title, Type: in.Type, MIME: in.MIME, URI: in.URI,
			Size: v.Size, CreatedAt: v.CreatedAt, Producer: v.Producer,
			Versions: []manifest.ArtifactVersion{v}, Skipped: skipped,
		}
		m.Artifacts = append(m.Artifacts, out)
		return nil
	}, op)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CanonicalURI is the form artifact URIs are stored and compared in, so
// one file reached through two spellings is one artifact: scheme and host
// lowercased, the path decoded and re-encoded one way, and a trailing
// slash dropped. Use it for every lookup by uri. A string that does not
// parse as a URL is returned trimmed.
func CanonicalURI(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return raw
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	u.RawPath = "" // re-encode from the decoded Path
	if len(u.Path) > 1 {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}

func guessMIME(ext string, body []byte) string {
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	sample := body[:min(len(body), 8192)]
	for i := 0; i < 3 && len(sample) < len(body) && !utf8.Valid(sample); i++ {
		sample = sample[:len(sample)-1]
	}
	if !bytes.ContainsRune(sample, 0) && utf8.Valid(sample) {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// storeBody writes artifacts/<hex> unless it is already there.
func (s *Store) storeBody(hexHash string, body []byte) error {
	p := filepath.Join(s.dir, "artifacts", hexHash)
	if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeAtomic(p, body)
}

var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ArtifactPath returns the body path for "sha256:<hex>" or "<hex>", or "" if invalid.
func (s *Store) ArtifactPath(hash string) string {
	h := strings.TrimPrefix(hash, "sha256:")
	if !hexPattern.MatchString(h) {
		return ""
	}
	return filepath.Join(s.dir, "artifacts", h)
}

// ReadArtifact returns a stored body.
func (s *Store) ReadArtifact(hash string) ([]byte, error) {
	if p := s.ArtifactPath(hash); p != "" {
		return os.ReadFile(p)
	}
	return nil, fmt.Errorf("%q is not a sha256 artifact hash", hash)
}
