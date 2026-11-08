package drift

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// docFiles returns docs/**/*.md. Drift reports under docs/drift/ quote the
// text they flag, so they are left out; the folder's README stays in.
func docFiles(t *testing.T) []string {
	t.Helper()
	return walkFiles(t, "docs", func(rel string) bool {
		if !strings.HasSuffix(rel, ".md") {
			return false
		}
		if strings.HasPrefix(rel, "docs/drift/") && rel != "docs/drift/README.md" {
			return false
		}
		return true
	})
}

// proseFiles is the house-rules set: the top-level Markdown, the docs, the
// site and the UI sources, without tests and generated files.
func proseFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"README.md", "CONTRIBUTING.md", "CLAUDE.md", "site/index.html"}
	files = append(files, docFiles(t)...)
	files = append(files, walkFiles(t, "web/src", func(rel string) bool {
		if strings.HasPrefix(rel, "web/src/generated/") || strings.HasPrefix(rel, "web/src/test/") {
			return false
		}
		if strings.HasSuffix(rel, ".test.ts") || strings.HasSuffix(rel, "_test.go") {
			return false
		}
		return strings.HasSuffix(rel, ".svelte") || strings.HasSuffix(rel, ".ts")
	})...)
	return files
}

// Dashes that are not used in this repo's prose. U+2212 (minus) and U+2012
// (figure dash) are not dashes used as punctuation and are never flagged.
var dashes = []struct {
	r    rune
	name string
}{
	{'—', "em-dash (U+2014)"},
	{'–', "en-dash (U+2013)"},
}

var (
	memoryWord   = regexp.MustCompile(`(?i)\bmemory\b`)
	quotedMemory = regexp.MustCompile(`(?i)"memory"`)
)

// mentionsMemory reports whether a line uses the word outside straight
// double quotes. A line that cites ADR-008 and says what the rule is
// ("record of work", or the word in quotes) is stating the rule; a line
// that only cites the ADR is not exempt, so a sentence cannot call COAL a
// memory by appending the reference.
func mentionsMemory(line string) bool {
	if strings.Contains(line, "ADR-008") && (strings.Contains(line, "record of work") || quotedMemory.MatchString(line)) {
		return false
	}
	return memoryWord.MatchString(quotedMemory.ReplaceAllString(line, ""))
}

// checkHouseRules: no em-dash or en-dash, and the word "memory" only where
// allow.txt says it is about something other than COAL (ADR-008).
func checkHouseRules(t *testing.T) {
	c := known(t, "house-rules")
	files := proseFiles(t)
	if len(files) < minHouseRuleFiles {
		c.Errorf("found %d files to scan for house-rules (floor %d); update proseFiles in internal/drift/prose_test.go", len(files), minHouseRuleFiles)
	}
	for _, f := range files {
		for i, l := range fileLines(f) {
			for _, d := range dashes {
				if strings.ContainsRune(l, d.r) {
					c.Errorf("%s:%d: %s: use a comma, a colon or a hyphen", f, i+1, d.name)
				}
			}
			if mentionsMemory(l) && !allowed(t, f, l) {
				c.Errorf("%s:%d: the word \"memory\" (ADR-008: COAL is a record of work); reword it, or add a line to %s quoting the sentence if it is about another product or about machine resources", f, i+1, allowFile)
			}
		}
	}
}

// stackWords are from the TypeScript stack that ADR-018 retired. ADR-005
// and ADR-018 in docs/06 and docs/poc are history and are not scanned.
var stackWords = []struct {
	re   *regexp.Regexp
	word string
}{
	{regexp.MustCompile(`(?i)\bpnpm\b`), "pnpm"},
	{regexp.MustCompile(`\bFastify\b`), "Fastify"},
	{regexp.MustCompile(`\bZod\b`), "Zod"},
	{regexp.MustCompile(`\bTypeScript monorepo\b`), "TypeScript monorepo"},
	{regexp.MustCompile(`(?i)\bnpm install\b`), "npm install"},
	{regexp.MustCompile(`(?i)\bnpx\b`), "npx"},
}

var stackWordFiles = []string{
	"README.md",
	"CONTRIBUTING.md",
	"CLAUDE.md",
	"docs/02-SPEC.md",
	"docs/03-ARCHITECTURE.md",
	"docs/04-INTENT-MANIFEST.md",
	"docs/05-MVP-TASKS.md",
	"site/index.html",
}

func checkStackWords(t *testing.T) {
	c := known(t, "stack-words")
	for _, f := range stackWordFiles {
		lines := fileLines(f)
		if lines == nil {
			c.Errorf("%s: cannot be read; update stackWordFiles in internal/drift/prose_test.go if the file moved", f)
			continue
		}
		for i, l := range lines {
			for _, w := range stackWords {
				if w.re.MatchString(l) && !allowed(t, f, l) {
					c.Errorf("%s:%d: %q is from the retired TypeScript stack (ADR-018 superseded ADR-005); describe the Go implementation, or add a line to %s quoting the sentence if it records what was retired", f, i+1, w.word, allowFile)
				}
			}
		}
	}
}

// flaggedWord reports whether a line has a word the prose checks flag.
func flaggedWord(line string) bool {
	if mentionsMemory(line) {
		return true
	}
	for _, w := range stackWords {
		if w.re.MatchString(line) {
			return true
		}
	}
	return false
}

// allow.txt: one entry per line, `<repo path>\t<exact substring>`. A line
// in that file containing the substring may use a flagged word. A line
// with a path alone allows the whole file.
type allowEntry struct {
	path, substr string
	line         int
}

var allowOnce = sync.OnceValues(func() ([]allowEntry, error) {
	var entries []allowEntry
	for i, l := range fileLines(allowFile) {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, "\t")
		e := allowEntry{path: f[0], line: i + 1}
		switch len(f) {
		case 1:
		case 2:
			e.substr = f[1]
		default:
			return nil, &allowError{allowFile, i + 1, "a line is <path>\\t<exact substring>, or a path alone for a whole file"}
		}
		if e.path == "" || (len(f) == 2 && e.substr == "") {
			return nil, &allowError{allowFile, i + 1, "the path and the substring must not be empty"}
		}
		entries = append(entries, e)
	}
	return entries, nil
})

type allowError struct {
	file string
	line int
	msg  string
}

func (e *allowError) Error() string { return fmt.Sprintf("%s:%d: %s", e.file, e.line, e.msg) }

func allowEntries(t *testing.T) []allowEntry {
	t.Helper()
	entries, err := allowOnce()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// allowed reports whether allow.txt lets line in path use a flagged word.
// An entry whose substring is shorter than minAllowSubstr exempts nothing;
// checkAllow reports it.
func allowed(t *testing.T, path, line string) bool {
	t.Helper()
	for _, e := range allowEntries(t) {
		if e.path != path {
			continue
		}
		if e.substr == "" || (len(e.substr) >= minAllowSubstr && strings.Contains(line, e.substr)) {
			return true
		}
	}
	return false
}

// minAllowSubstr is the shortest substring an allow.txt entry may quote. A
// shorter one is a fragment that matches far more than the sentence it is
// meant to allow.
const minAllowSubstr = 12

// checkAllow: every allow.txt entry quotes a sentence (not a fragment) and
// still matches a line in its file that has a flagged word, so the list
// cannot keep exemptions for text that is gone, and an entry cannot exempt
// lines the checks would not flag.
func checkAllow(t *testing.T) {
	c := known(t, "allow")
	for _, e := range allowEntries(t) {
		lines := fileLines(e.path)
		if lines == nil {
			c.Errorf("%s:%d: %s does not exist: remove the entry", allowFile, e.line, e.path)
			continue
		}
		if e.substr == "" {
			if !slices.ContainsFunc(lines, flaggedWord) {
				c.Errorf("%s:%d: allow.txt entry for %s matches nothing: remove it", allowFile, e.line, e.path)
			}
			continue
		}
		if len(e.substr) < minAllowSubstr {
			c.Errorf("%s:%d: the substring %q must quote the sentence, not a fragment (at least %d characters)", allowFile, e.line, e.substr, minAllowSubstr)
			continue
		}
		matched, flagged := false, false
		for _, l := range lines {
			if !strings.Contains(l, e.substr) {
				continue
			}
			matched = true
			if flaggedWord(l) {
				flagged = true
				break
			}
		}
		switch {
		case !matched:
			c.Errorf("%s:%d: allow.txt entry for %s matches nothing: remove it", allowFile, e.line, e.path)
		case !flagged:
			c.Errorf("%s:%d: allow.txt entry for %s matches lines that use no flagged word: quote the sentence with the word, or remove the entry", allowFile, e.line, e.path)
		}
	}
}
