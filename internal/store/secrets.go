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
	"*credentials*", "*.p12", "*.pfx", ".netrc", "_netrc", ".npmrc", ".pypirc", ".git-credentials",
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
	{"sk- key", skCandidate, false}, // further checked by isSKKey
	{"Slack token", regexp.MustCompile(`\b(xox[abps]-|xapp-[A-Za-z0-9-]{10,})`), false},
	{"GitHub token", regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_)`), false},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), false},
	{"npm token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}`), false},
	{"PyPI token", regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{50,}`), false},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}`), false},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{40,}`), true},
	// A netrc line names a machine or login next to the password; a file
	// named .netrc or _netrc is refused by name.
	{"netrc password", regexp.MustCompile(`(?m)^.*\b(machine|login)\s+\S+.*\bpassword\s+\S+`), false},
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), false},
}

// skCandidate finds sk- runs; isSKKey decides which are keys. A key is
// either a known prefix (sk-proj-, sk-ant-, sk-live-, sk-test-, sk-or-,
// sk-svcacct-) followed by 20 or more token characters, or a run of 20 or
// more with a digit and both cases. The character before "sk-" must not
// be a letter, digit, '-', '_' or '.', so CSS classes like .sk-circle and
// ids like task-sk-2025 do not count.
var (
	skCandidate = regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)
	skKnown     = regexp.MustCompile(`^sk-(proj|ant|live|test|or|svcacct)-[A-Za-z0-9_-]{20,}`)
)

func isSKKey(body []byte, loc []int) bool {
	if i := loc[0]; i > 0 {
		c := body[i-1]
		if c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			return false
		}
	}
	run := body[loc[0]:loc[1]]
	if skKnown.Match(run) {
		return true
	}
	var digit, upper, lower bool
	for _, c := range run[3:] {
		digit = digit || c >= '0' && c <= '9'
		upper = upper || c >= 'A' && c <= 'Z'
		lower = lower || c >= 'a' && c <= 'z'
	}
	return digit && upper && lower
}

// ruleMatches reports whether token rule i fires on body.
func ruleMatches(i int, body []byte) bool {
	r := tokenRules[i]
	if r.re != skCandidate {
		return r.re.Match(body)
	}
	for _, loc := range r.re.FindAllIndex(body, -1) {
		if isSKKey(body, loc) {
			return true
		}
	}
	return false
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
	for i, r := range tokenRules {
		if r.notInTests && inTests {
			continue
		}
		if ruleMatches(i, body) {
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
	if len(data) > 0 && json.Unmarshal(data, &cfg) != nil {
		// Leave a config.json we cannot parse alone and keep the salt in
		// its own file instead.
		b, err := s.fileSalt(filepath.Join(s.dir, ".salt"))
		if err == nil {
			s.saltVal = b
		}
		return b, err
	}
	raw, present := cfg[saltKey]
	if b, ok := decodeSalt(raw); ok {
		s.saltVal = b
		return b, nil
	}
	if present {
		s.Log("config.json: %s was malformed; replaced it with a new salt", saltKey)
	}
	b, err := newSalt()
	if err != nil {
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

// fileSalt reads or creates a salt file holding 64 hex characters; the
// caller holds .config.lock. writeAtomic creates it with mode 0600.
func (s *Store) fileSalt(p string) ([]byte, error) {
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		raw, _ := json.Marshal(strings.TrimSpace(string(data)))
		if b, ok := decodeSalt(raw); ok {
			return b, nil
		}
		s.Log("%s was malformed; replaced it with a new salt", filepath.Base(p))
	}
	b, err := newSalt()
	if err != nil {
		return nil, err
	}
	return b, writeAtomic(p, []byte(hex.EncodeToString(b)+"\n"))
}

func decodeSalt(raw json.RawMessage) ([]byte, bool) {
	var h string
	if raw == nil || json.Unmarshal(raw, &h) != nil {
		return nil, false
	}
	b, err := hex.DecodeString(h)
	return b, err == nil && len(b) == 32
}

func newSalt() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}
