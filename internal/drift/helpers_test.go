package drift

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The tests below exercise the parsers and matchers the checks rely on, so
// a green run of TestDrift means the repo is clean and not that a selector
// matched nothing.

func TestCommandTokens(t *testing.T) {
	tests := []struct {
		span string
		want []string
	}{
		{"coal find budget", []string{"find", "budget"}},
		{"coal find --json", []string{"find"}},
		{"coal", nil},
		{"coal --home /x serve", nil},
		{"coal <cmd> --help", nil},
		{"coal import claude-code", []string{"import", "claude-code"}},
		{"coal hook session-start", []string{"hook", "session-start"}},
		{"coal find \"budget\"", []string{"find"}},
		{"coal_find", nil},
		{"make coal", nil},
	}
	for _, tt := range tests {
		if got := commandTokens(tt.span); !slices.Equal(got, tt.want) {
			t.Errorf("commandTokens(%q) = %q, want %q", tt.span, got, tt.want)
		}
	}
}

func TestParseCommands(t *testing.T) {
	help := "Usage:\n  coal [command]\n\nAvailable Commands:\n  add         Record\n  find        Search\n\nFlags:\n  -h, --help\n"
	if got := parseCommands(help); !slices.Equal(got, []string{"add", "find"}) {
		t.Errorf("parseCommands = %q", got)
	}
	if got := parseCommands("Usage:\n  coal hook <x>\n\nFlags:\n"); got != nil {
		t.Errorf("leaf help parsed as %q", got)
	}
}

func TestMentionsMemory(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"COAL is a record of work, not a memory.", true},
		{"Daemon idle memory under 100 MB.", true},
		{"an in-memory transport", true},
		{"No \"memory\" in any copy", false},
		{"Do not use the word \"memory\" in UI copy (ADR-008).", false},
		{"COAL is a record of work, not a memory (ADR-008)", false},
		{"COAL is the memory for your agent, see ADR-008.", true}, // citing the ADR alone is not the rule
		{"Memory is not a word we use", true},
		{"memorable, memories, OpenMemory", false},
	}
	for _, tt := range tests {
		if got := mentionsMemory(tt.line); got != tt.want {
			t.Errorf("mentionsMemory(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestStackWords(t *testing.T) {
	tests := []struct {
		line string
		want string // the word flagged, "" for none
	}{
		{"run pnpm install", "pnpm"},
		{"a Fastify server", "Fastify"},
		{"the Zod schema", "Zod"},
		{"ADR-005: TypeScript monorepo", "TypeScript monorepo"},
		{"TypeScript files are captured as .ts", ""},
		{"then npm install it", "npm install"},
		{"npx vitest", "npx"},
		{"the npm registry", ""},
		{"zodiac signs", ""},
	}
	for _, tt := range tests {
		var got string
		for _, w := range stackWords {
			if w.re.MatchString(tt.line) {
				got = w.word
				break
			}
		}
		if got != tt.want {
			t.Errorf("stack word in %q = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestParseADRs(t *testing.T) {
	lines := []string{
		"# Decisions",
		"",
		"## ADR-001: One",
		"",
		"**Status.** Accepted.",
		"",
		"**Decision.** Do the thing.",
		"",
		"**In the code.** `go.mod` and",
		"`internal/drift` keep it.",
		"",
		"## ADR-002: Two",
		"",
		"**Status.** Proposed 2026-11-06.",
	}
	adrs := parseADRs(lines)
	if len(adrs) != 2 || adrs[0].num != "ADR-001" || adrs[0].line != 3 || adrs[1].num != "ADR-002" || adrs[1].line != 12 {
		t.Fatalf("parseADRs = %+v", adrs)
	}
	code, line := adrs[0].paragraph("**In the code.**")
	if line != 9 || code != "**In the code.** `go.mod` and `internal/drift` keep it." {
		t.Errorf("paragraph = %q at %d", code, line)
	}
	if code, _ := adrs[1].paragraph("**In the code.**"); code != "" {
		t.Errorf("ADR-002 paragraph = %q, want none", code)
	}
}

func TestCheckCodePath(t *testing.T) {
	tests := []struct {
		tok  string
		want string // substring of the failure, "" for a pass
	}{
		{"go.mod", ""},
		{"internal/drift", ""},
		{"internal/drift/", ""},
		{"LICENSE", ""},
		{"internal/marker/marker.go:24", ""},
		{"internal/marker/marker.go:999999", "has only"},
		{"internal/drift:3", "is a directory"},
		{"internal/nope", "does not exist"},
		{"internal/{a,b}", "not a repo path"},
		{"~/.coal/manifests", "not a repo path"},
		{"coal_find", "does not exist"}, // a symbol name is read as a path
	}
	for _, tt := range tests {
		c := &check{t: t, id: "adr-code-locations"}
		checkCodePath(c, 1, "ADR-000", tt.tok)
		switch {
		case tt.want == "" && len(c.msgs) > 0:
			t.Errorf("checkCodePath(%q) failed: %s", tt.tok, c.msgs[0])
		case tt.want != "" && (len(c.msgs) == 0 || !strings.Contains(c.msgs[0], tt.want)):
			t.Errorf("checkCodePath(%q) = %q, want a failure containing %q", tt.tok, c.msgs, tt.want)
		}
	}
}

func TestWalkKeys(t *testing.T) {
	schema := schemaDoc(t)
	tests := []struct {
		doc  string
		want string // substring of the single failure, "" for a pass
	}{
		{`{"id":"x","intent":{"summary":"s","source":"user"},"status":"active"}`, ""},
		{`{"artifacts":[{"hash":"h","versions":[{"hash":"h","producer":{"tool":"t"}}]}]}`, ""},
		{`{"contexts":[{"kind":"path","key":"/x"}]}`, ""},
		{`{"ext":{"anything":{"goes":1}}}`, ""},
		{`{"artifacts":[{"ext":{"dev.agent-trace":[1]}}]}`, ""},
		{`{"nextStep":"x"}`, "uses nextStep"},
		{`{"state":{"nextSteps":"x"}}`, "uses state.nextSteps"},
		{`{"artifacts":[{"versions":[{"size":1,"modifiedAt":"x"}]}]}`, "artifacts[].versions[].modifiedAt"},
		{`{"status":"open"}`, `"open", which is not in the schema enum`},
		{`{"intent":{"source":"agent"}}`, "intent.source"},
	}
	for _, tt := range tests {
		doc := parseJSON(t, tt.doc)
		c := &check{t: t, id: "site-facts"}
		walkKeys(c, 1, schema, schema, doc, "")
		switch {
		case tt.want == "" && len(c.msgs) > 0:
			t.Errorf("walkKeys(%s) failed: %s", tt.doc, strings.Join(c.msgs, "; "))
		case tt.want != "" && (len(c.msgs) != 1 || !strings.Contains(c.msgs[0], tt.want)):
			t.Errorf("walkKeys(%s) = %q, want one failure containing %q", tt.doc, c.msgs, tt.want)
		}
	}
}

func TestClassify(t *testing.T) {
	entries := map[string]knownEntry{"x": {id: "x", issue: "#1", note: "n", line: 5}}
	tests := []struct {
		id       string
		msgs     []string
		logs     int
		errs     int
		errsHave string
	}{
		{"x", []string{"a", "b"}, 2, 0, ""},
		{"x", nil, 0, 1, "known failure x now passes: remove it from internal/drift/known.txt (line 5)"},
		{"y", []string{"a"}, 0, 1, "a"},
		{"y", nil, 0, 0, ""},
	}
	for _, tt := range tests {
		logs, errs := classify(entries, tt.id, tt.msgs)
		if len(logs) != tt.logs || len(errs) != tt.errs {
			t.Errorf("classify(%q, %q) = %d logs, %d errs; want %d, %d", tt.id, tt.msgs, len(logs), len(errs), tt.logs, tt.errs)
			continue
		}
		if tt.errsHave != "" && errs[0] != tt.errsHave {
			t.Errorf("classify(%q, %q) err = %q, want %q", tt.id, tt.msgs, errs[0], tt.errsHave)
		}
	}
}

func TestReadmePhaseRow(t *testing.T) {
	if m := readmePhaseRow.FindStringSubmatch("| 1 | Record | MVP, in progress | A record |"); m == nil || m[2] != "Record" {
		t.Errorf("readmePhaseRow = %q", m)
	}
	if m := readmePhaseRow.FindStringSubmatch("| Phase | Name | Status |"); m != nil {
		t.Errorf("header row matched: %q", m)
	}
	if m := readmePhaseRow.FindStringSubmatch("|---|---|---|---|"); m != nil {
		t.Errorf("rule row matched: %q", m)
	}
}

func TestReadmePhases(t *testing.T) {
	lines := []string{
		"| N | Thing |", // a numbered table before the phases table is not read
		"|---|---|",
		"| 1 | Foo |",
		"",
		"| Phase | Name | Status |",
		"|---|---|---|",
		"| 1 | Record | MVP |",
		"| 2 | Reach | Not started |",
		"",
		"| 3 | Not a phase |", // after the table ends
	}
	names, lineNos, found := readmePhases(lines)
	if !found || !slices.Equal(names, []string{"Record", "Reach"}) || !slices.Equal(lineNos, []int{7, 8}) {
		t.Errorf("readmePhases = %q at %v, found %v", names, lineNos, found)
	}
	if _, _, found := readmePhases(lines[:3]); found {
		t.Error("readmePhases found a header in a table that has none")
	}
}

func TestMarkdownSpans(t *testing.T) {
	lines := []string{
		"Run `coal find budget` or `make build`.",
		"```",
		"coal init",
		".sandbox/bin/coal find budget   # a comment",
		"export COAL_HOME=$PWD/.sandbox/coal",
		"`coal nope` is not a span inside a fence",
		"```",
		"Then `coal show <id>`.",
	}
	got := markdownSpans(lines)
	want := []span{
		{"coal find budget", 1},
		{"make build", 1},
		{"coal init", 3},
		{"coal find budget # a comment", 4},
		{"coal show <id>", 8},
	}
	if !slices.Equal(got, want) {
		t.Errorf("markdownSpans = %v, want %v", got, want)
	}
}

func TestHTMLSpans(t *testing.T) {
	text := "<p><code>coal find</code> and\n<code>coal\nserve</code> and <code>a &lt;b&gt; <em>c</em></code></p>"
	got := htmlSpans(text)
	want := []span{{"coal find", 1}, {"coal\nserve", 2}, {"a <b> c", 3}}
	if !slices.Equal(got, want) {
		t.Errorf("htmlSpans = %v, want %v", got, want)
	}
	if toks := commandTokens(want[1].text); !slices.Equal(toks, []string{"serve"}) {
		t.Errorf("commandTokens of a span broken across lines = %q", toks)
	}
}

func parseJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return doc
}
