package drift

import (
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	backtickSpan = regexp.MustCompile("`([^`]+)`")
	codeSpan     = regexp.MustCompile(`(?s)<code>(.*?)</code>`)
	tag          = regexp.MustCompile(`<[^>]+>`)
	cmdToken     = regexp.MustCompile(`^[a-z][a-z-]*$`)
)

// commandFiles is the set scanned for `coal <cmd>` references.
func commandFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"README.md", "CONTRIBUTING.md", "CLAUDE.md"}
	files = append(files, docFiles(t)...)
	return append(files, "site/index.html")
}

// commandTokens returns the command tokens of a code span: after `coal`,
// every token matching ^[a-z][a-z-]*$ up to the first that does not. Flags,
// arguments and placeholders end the list.
func commandTokens(span string) []string {
	f := strings.Fields(span)
	if len(f) == 0 || f[0] != "coal" {
		return nil
	}
	var toks []string
	for _, w := range f[1:] {
		if !cmdToken.MatchString(w) {
			break
		}
		toks = append(toks, w)
	}
	return toks
}

// parseCommands returns the names in a cobra "Available Commands:" block.
func parseCommands(help string) []string {
	var cmds []string
	in := false
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "Available Commands:") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.TrimSpace(l) == "" {
			break
		}
		if f := strings.Fields(l); len(f) > 0 {
			cmds = append(cmds, f[0])
		}
	}
	return cmds
}

// commandTree runs `coal help` and `coal <cmd> --help` one level down and
// returns each command's subcommands (none for a leaf). help and
// completion, cobra's built-ins, are left out.
func commandTree(t *testing.T) map[string][]string {
	t.Helper()
	out, err := runCoal(t, "help")
	if err != nil {
		t.Fatalf("coal help: %v\n%s", err, out)
	}
	top := parseCommands(out)
	if len(top) == 0 {
		t.Fatalf("coal help printed no Available Commands block; update parseCommands in internal/drift/commands_test.go:\n%s", out)
	}
	tree := map[string][]string{}
	for _, cmd := range top {
		if cmd == "help" || cmd == "completion" {
			continue
		}
		out, err := runCoal(t, cmd, "--help")
		if err != nil {
			t.Fatalf("coal %s --help: %v\n%s", cmd, err, out)
		}
		tree[cmd] = parseCommands(out)
	}
	return tree
}

// span is one piece of code text found in a doc, with the line it starts on.
type span struct {
	text string
	line int
}

// markdownSpans returns the code spans of a Markdown file: inline
// backtick spans, and inside a fenced ``` block every line whose first
// token is `coal` or a path ending in `/coal` (as in `.sandbox/bin/coal
// init`), read as a command line.
func markdownSpans(lines []string) []span {
	var out []span
	fenced := false
	for i, l := range lines {
		if strings.HasPrefix(l, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			f := strings.Fields(l)
			if len(f) > 0 && (f[0] == "coal" || strings.HasSuffix(f[0], "/coal")) {
				out = append(out, span{text: "coal " + strings.Join(f[1:], " "), line: i + 1})
			}
			continue
		}
		for _, m := range backtickSpan.FindAllStringSubmatch(l, -1) {
			out = append(out, span{text: m[1], line: i + 1})
		}
	}
	return out
}

// htmlSpans returns the <code> spans of an HTML file, matched over the
// whole text so a span broken across lines is still read, with inner tags
// stripped and entities unescaped.
func htmlSpans(text string) []span {
	var out []span
	for _, m := range codeSpan.FindAllStringSubmatchIndex(text, -1) {
		inner := html.UnescapeString(tag.ReplaceAllString(text[m[2]:m[3]], ""))
		out = append(out, span{text: inner, line: lineAt(text, m[0])})
	}
	return out
}

// checkCommands: every `coal <cmd>` in the docs and on the site names a
// command the binary has.
func checkCommands(t *testing.T) {
	c := known(t, "commands")
	tree := commandTree(t)
	names := slices.Sorted(func(yield func(string) bool) {
		for k := range tree {
			if !yield(k) {
				return
			}
		}
	})
	refs := map[string]bool{}
	for _, f := range commandFiles(t) {
		var spans []span
		if strings.HasSuffix(f, ".html") {
			spans = htmlSpans(mustRead(t, f))
		} else {
			spans = markdownSpans(fileLines(f))
		}
		for _, s := range spans {
			toks := commandTokens(s.text)
			if len(toks) == 0 {
				continue
			}
			cmd := toks[0]
			if cmd == "help" || cmd == "completion" {
				continue
			}
			subs, ok := tree[cmd]
			if !ok {
				c.Errorf("%s:%d: `coal %s` names a command that does not exist (coal help lists: %s); fix the doc, or add the command in cmd/coal", f, s.line, cmd, strings.Join(names, ", "))
				continue
			}
			ref := cmd
			if len(subs) > 0 && len(toks) > 1 {
				if !slices.Contains(subs, toks[1]) {
					c.Errorf("%s:%d: `coal %s %s` names a subcommand that does not exist (coal %s has: %s); fix the doc, or add the subcommand in cmd/coal", f, s.line, cmd, toks[1], cmd, strings.Join(subs, ", "))
					continue
				}
				ref += " " + toks[1]
			}
			refs[ref] = true
		}
	}
	if len(refs) < minCommandRefs {
		c.Errorf("found %d distinct coal command references in the docs (floor %d); update commandFiles or the code-span regexps in internal/drift/commands_test.go", len(refs), minCommandRefs)
	}
}
