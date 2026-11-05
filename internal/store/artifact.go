package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

var typeByExt = map[string]string{}

func init() {
	for t, exts := range map[string]string{
		"html":  "html htm",
		"doc":   "md markdown txt rst",
		"data":  "json csv yaml yml xml tsv",
		"image": "png jpg jpeg gif svg webp",
		"code": "go js mjs cjs ts tsx jsx py rb rs java kt c h cc cpp hpp cs swift " +
			"sh bash zsh css scss sql php lua svelte vue toml",
	} {
		for e := range strings.FieldsSeq(exts) {
			typeByExt["."+e] = t
		}
	}
}

// RegisterArtifact records a body on a manifest. Identity is (manifest,
// uri), where a new body appends a version, or the hash when there is no
// uri. Possible secrets are recorded with skipped set and never copied.
func (s *Store) RegisterArtifact(manifestID string, in ArtifactInput, op Op) (*manifest.Artifact, error) {
	body := in.Content
	if in.Path != "" {
		abs, err := filepath.Abs(in.Path)
		if err != nil {
			return nil, err
		}
		if body, err = os.ReadFile(abs); err != nil {
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
	in.Producer.Tool = or(in.Producer.Tool, or(op.Tool, "coal"))
	op.Action = or(op.Action, "register artifact "+in.Title)

	sum := sha256.Sum256(body)
	hexHash := hex.EncodeToString(sum[:])
	size := int64(len(body))
	skipped := secretRule(in, body)
	if skipped == "" {
		if err := s.storeBody(hexHash, body); err != nil {
			return nil, err
		}
	}
	v := manifest.ArtifactVersion{Hash: "sha256:" + hexHash, Size: &size, CreatedAt: manifest.Now(), Producer: in.Producer, Skipped: skipped}

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

// deniedNames are file name patterns that are never copied.
var deniedNames = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "credentials*", "*.p12", "*.pfx"}

// tokenRules are body patterns that look like credentials. The rule
// name is recorded; the matched text never is.
var tokenRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"sk- key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[abps]-`)},
	{"GitHub token", regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{20,}|github_pat_)`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}`)},
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

// secretRule returns a skipped note, or "" when the artifact looks safe.
func secretRule(in ArtifactInput, body []byte) string {
	names := []string{filepath.Base(in.Path), path.Base(in.Title)}
	if u, err := url.Parse(in.URI); err == nil {
		names = append(names, path.Base(u.Path))
	}
	for _, n := range names {
		for _, pat := range deniedNames {
			if ok, _ := path.Match(pat, strings.ToLower(n)); ok {
				return "possible secret: file name matches " + pat
			}
		}
	}
	sample := body[:min(len(body), 1<<20)] // first 1 MB
	for _, r := range tokenRules {
		if r.re.Match(sample) {
			return "possible secret: " + r.name
		}
	}
	return ""
}
