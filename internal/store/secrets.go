package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxBody is the largest body COAL copies. Larger bodies are recorded
// with skipped "too large".
const MaxBody = 5 << 20

// deniedNames are file name patterns that are never copied.
var deniedNames = []string{
	".env", ".env.*", "*.env", ".envrc", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "id_ecdsa*", "id_dsa*",
	"*credentials*", "*.p12", "*.pfx", ".netrc", ".npmrc", ".pypirc", ".git-credentials",
	"*.kdbx", ".pgpass", "*.jks", "*.keystore",
}

// sensitiveDirs, under the home directory, and sensitiveRoots are never
// read at all: everything below them is skipped.
var (
	sensitiveDirs = []string{".ssh", ".aws", ".config/gh", ".kube", ".docker", ".gnupg", ".password-store",
		".mozilla", ".config/google-chrome", ".config/chromium", ".config/BraveSoftware", ".local/share/keyrings", ".claude"}
	sensitiveRoots = []string{"/etc", "/proc", "/sys"}
)

// tokenRules are body patterns that look like credentials. The rule
// name is recorded; the matched text never is.
// Rules with notInTests do not apply to test and fixture files, which
// often carry sample tokens on purpose.
var tokenRules = []struct {
	name       string
	re         *regexp.Regexp
	notInTests bool
}{
	{"sk- key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`), false},
	{"Slack token", regexp.MustCompile(`\b(xox[abps]-|xapp-[A-Za-z0-9-]{10,})`), false},
	{"GitHub token", regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_)`), false},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), false},
	{"npm token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}`), false},
	{"PyPI token", regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{50,}`), false},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}`), false},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), true},
	{"netrc password", regexp.MustCompile(`(?m)(\bmachine\s+\S+.*\s|^\s*)password\s+\S+\s*$`), false},
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), false},
}

// sensitiveLocation returns a skipped note when any of the paths lies in
// a credentials directory, or "".
func sensitiveLocation(paths ...string) string {
	home, _ := os.UserHomeDir()
	for _, p := range paths {
		for _, root := range sensitiveRoots {
			if under(p, root) {
				return "sensitive location: " + root
			}
		}
		if home == "" {
			continue
		}
		for _, d := range sensitiveDirs {
			if under(p, filepath.Join(home, d)) {
				return "sensitive location: ~/" + d
			}
		}
	}
	return ""
}

func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

// secretRule returns a skipped note, or "" when the artifact looks safe.
// names are extra file names to check, such as a symlink's target.
func secretRule(in ArtifactInput, body []byte, names ...string) string {
	names = append(names, filepath.Base(in.Path), path.Base(in.Title))
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
	inTests := isTestPath(in)
	for _, r := range tokenRules {
		if r.notInTests && inTests {
			continue
		}
		if r.re.Match(body) {
			return "possible secret: " + r.name
		}
	}
	return ""
}

var (
	testNames = []string{"*_test.*", "*.test.*", "*.spec.*"}
	testDirs  = map[string]bool{"testdata": true, "fixtures": true, "__fixtures__": true, "__tests__": true}
)

// isTestPath reports whether the artifact's path or uri names a test or
// fixture file: a base name like *_test.*, *.test.* or *.spec.*, or a
// directory segment named testdata, fixtures, __fixtures__ or __tests__.
func isTestPath(in ArtifactInput) bool {
	paths := []string{filepath.ToSlash(in.Path), in.Title}
	if u, err := url.Parse(in.URI); err == nil {
		paths = append(paths, u.Path)
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		segs := strings.Split(p, "/")
		for _, seg := range segs[:len(segs)-1] {
			if testDirs[seg] {
				return true
			}
		}
		for _, pat := range testNames {
			if ok, _ := path.Match(pat, segs[len(segs)-1]); ok {
				return true
			}
		}
	}
	return false
}

// blindHash returns "sha256:" + HMAC-SHA256(salt, r) for a body that is
// not copied, so the manifest never carries a fingerprint someone could
// check a guessed secret against.
func (s *Store) blindHash(r io.Reader) (string, error) {
	salt, err := s.salt()
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, salt)
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// saltKey holds the HMAC salt in config.json. config.json is outside the
// history, and the salt is never logged.
const saltKey = "skippedHashSalt"

// salt returns the data directory's 32-byte salt, creating it on first use.
func (s *Store) salt() ([]byte, error) {
	s.saltMu.Lock()
	defer s.saltMu.Unlock()
	if s.saltVal != nil {
		return s.saltVal, nil
	}
	unlock, err := lock(filepath.Join(s.dir, ".config.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	cfgPath := filepath.Join(s.dir, "config.json")
	cfg := map[string]json.RawMessage{}
	data, err := os.ReadFile(cfgPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, errors.New("config.json is not a JSON object; fix it before registering skipped artifacts")
		}
	}
	var hexSalt string
	if raw, ok := cfg[saltKey]; ok && json.Unmarshal(raw, &hexSalt) == nil {
		if b, err := hex.DecodeString(hexSalt); err == nil && len(b) == 32 {
			s.saltVal = b
			return b, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	cfg[saltKey], _ = json.Marshal(hex.EncodeToString(b))
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(cfgPath, append(out, '\n')); err != nil {
		return nil, err
	}
	s.saltVal = b
	return b, nil
}
