// Package drift is a test-only package. Its checks read the repo from the
// package directory and fail when a document and the code disagree. It is
// run by make test and make check and adds no code to the coal binary.
// docs/07-DRIFT.md explains the sources of truth and how to read a failure.
//
// Each check has a stable id and runs as one subtest of TestDrift. A check
// reports through the value known(t, id) returns, so a failure that
// known.txt lists under an issue is logged instead of failing, and a listed
// check that passes fails until its line is removed.
package drift

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Floors. A check that finds fewer than these has a broken selector or a
// broken file list, not a clean repo, and fails saying which selector to fix.
const (
	minCommandRefs    = 20 // distinct `coal <cmd>` references across the docs
	minSitePhases     = 1  // phases found in site/index.html
	minReadmePhases   = 4  // phases found in the README phases table
	minHouseRuleFiles = 10 // files scanned by house-rules
	minADRHeaders     = 15 // `## ADR-` headers in docs/06-DECISIONS.md
)

const (
	knownFile = "internal/drift/known.txt"
	allowFile = "internal/drift/allow.txt"
)

// checkIDs lists every check id. known refuses any other id, so known.txt
// cannot name a check that does not exist.
var checkIDs = []string{
	"commands",
	"deps",
	"schema-published/manifest",
	"schema-published/marker",
	"examples",
	"adr-code-locations",
	"house-rules",
	"stack-words",
	"allow",
	"phases",
	"site-facts",
	"known",
}

// repoRoot is the absolute path of the repo, two levels above this package.
var repoRoot string

func TestMain(m *testing.M) {
	root, err := filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, "drift: repo root:", err)
		os.Exit(2)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		fmt.Fprintf(os.Stderr, "drift: %s is not the repo root (no go.mod); run the tests from internal/drift\n", root)
		os.Exit(2)
	}
	repoRoot = root
	code := m.Run()
	if binDir != "" {
		os.RemoveAll(binDir)
	}
	os.Exit(code)
}

func TestDrift(t *testing.T) {
	t.Run("commands", checkCommands)
	t.Run("deps", checkDeps)
	t.Run("schema-published", func(t *testing.T) {
		t.Run("manifest", checkSchemaManifest)
		t.Run("marker", checkSchemaMarker)
	})
	t.Run("examples", checkExamples)
	t.Run("adr-code-locations", checkADRCodeLocations)
	t.Run("house-rules", checkHouseRules)
	t.Run("stack-words", checkStackWords)
	t.Run("allow", checkAllow)
	t.Run("phases", checkPhases)
	t.Run("site-facts", checkSiteFacts)
	t.Run("known", checkKnown)
}

// check collects the failures of one check id and reports them when the
// subtest ends, through known.txt.
type check struct {
	t    *testing.T
	id   string
	msgs []string
}

// known returns the reporter for check id. Every check calls it first and
// reports through Errorf; the report is written in a t.Cleanup.
func known(t *testing.T, id string) *check {
	t.Helper()
	if !slices.Contains(checkIDs, id) {
		t.Fatalf("check id %q is not in checkIDs in internal/drift/drift_test.go; add it there", id)
	}
	c := &check{t: t, id: id}
	t.Cleanup(c.report)
	return c
}

// Errorf records one failure. The message names the file and line and says
// what to change.
func (c *check) Errorf(format string, args ...any) {
	c.t.Helper()
	c.msgs = append(c.msgs, fmt.Sprintf(format, args...))
}

func (c *check) report() {
	logs, errs := classify(knownEntries(c.t), c.id, c.msgs)
	for _, m := range logs {
		c.t.Log(m)
	}
	for _, m := range errs {
		c.t.Error(m)
	}
}

// classify sorts a check's failures into log lines and test errors
// according to known.txt.
func classify(entries map[string]knownEntry, id string, msgs []string) (logs, errs []string) {
	entry, listed := entries[id]
	switch {
	case listed && len(msgs) > 0:
		for _, m := range msgs {
			logs = append(logs, fmt.Sprintf("known failure (%s, %s, %s): %s", id, entry.issue, entry.note, m))
		}
	case listed:
		errs = append(errs, fmt.Sprintf("known failure %s now passes: remove it from %s (line %d)", id, knownFile, entry.line))
	default:
		errs = msgs
	}
	return logs, errs
}

type knownEntry struct {
	id, issue, note string
	line            int
}

var knownOnce = sync.OnceValues(func() (map[string]knownEntry, error) {
	entries := map[string]knownEntry{}
	for i, l := range fileLines(knownFile) {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("%s:%d: a line is <check id>\\t#<issue>\\t<note>, three tab-separated fields; this one has %d", knownFile, i+1, len(f))
		}
		if _, dup := entries[f[0]]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice; keep one line", knownFile, i+1, f[0])
		}
		entries[f[0]] = knownEntry{id: f[0], issue: f[1], note: f[2], line: i + 1}
	}
	return entries, nil
})

// knownEntries returns known.txt by check id, failing the test when the
// file does not parse.
func knownEntries(t *testing.T) map[string]knownEntry {
	t.Helper()
	entries, err := knownOnce()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// checkKnown validates known.txt itself: every line names a real check id
// and an issue number.
func checkKnown(t *testing.T) {
	c := known(t, "known")
	issue := regexp.MustCompile(`^#[0-9]+$`)
	for _, e := range knownEntries(t) {
		if !slices.Contains(checkIDs, e.id) {
			c.Errorf("%s:%d: %q is not a check id; the ids are %s", knownFile, e.line, e.id, strings.Join(checkIDs, ", "))
		}
		if !issue.MatchString(e.issue) {
			c.Errorf("%s:%d: the second field is %q; it must be the issue number, like #27", knownFile, e.line, e.issue)
		}
		if strings.TrimSpace(e.note) == "" {
			c.Errorf("%s:%d: the third field is empty; say in a few words what is known to fail", knownFile, e.line)
		}
	}
}

// abs returns the absolute path of a repo-relative path.
func abs(rel string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

// readFile returns a repo file's text, or "" when it cannot be read.
func readFile(rel string) string {
	b, err := os.ReadFile(abs(rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// mustRead returns a repo file's text and fails the test when it is missing.
func mustRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(abs(rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

// fileLines returns a repo file's lines, without newlines; nil when the
// file cannot be read.
func fileLines(rel string) []string {
	f, err := os.Open(abs(rel))
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// exists reports whether a repo-relative file or directory exists.
func exists(rel string) bool {
	_, err := os.Stat(abs(rel))
	return err == nil
}

// lineAt returns the 1-based line number of byte offset off in s.
func lineAt(s string, off int) int {
	if off > len(s) {
		off = len(s)
	}
	return 1 + strings.Count(s[:off], "\n")
}

// walkFiles returns the repo-relative paths under dir (itself repo-relative)
// for which keep returns true, sorted.
func walkFiles(t *testing.T, dir string, keep func(rel string) bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(abs(dir), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if keep(rel) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", dir, err)
	}
	slices.Sort(out)
	return out
}

// Building the binary once per test binary. The build is a cache hit after
// make build; only the link runs.
var (
	buildOnce sync.Once
	binDir    string
	binPath   string
	binErr    error
)

// coalBinary builds cmd/coal into a temp dir, once, and returns its path.
// go is found on PATH only.
func coalBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			binErr = fmt.Errorf("go is not on PATH; the drift checks build cmd/coal with it: %w", err)
			return
		}
		binDir, err = os.MkdirTemp("", "coal-drift-")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(binDir, "coal")
		cmd := exec.Command(goBin, "build", "-o", binPath, "./cmd/coal")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			binErr = fmt.Errorf("go build ./cmd/coal in %s: %v\n%s", repoRoot, err, out)
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

// runCoal runs the built binary with HOME and COAL_HOME in a temp dir, so it
// can never touch ~/.coal, and returns its combined output.
func runCoal(t *testing.T, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(coalBinary(t), args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "COAL_HOME="+filepath.Join(home, "coal"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
