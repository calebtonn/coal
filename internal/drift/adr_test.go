package drift

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const decisionsFile = "docs/06-DECISIONS.md"

var (
	adrHeader = regexp.MustCompile(`^## (ADR-[0-9]{3}): (.*)$`)
	adrToken  = regexp.MustCompile(`ADR-[0-9]{3}`)
	// pathToken is a repo path with an optional :N line suffix.
	pathToken = regexp.MustCompile(`^([A-Za-z0-9_.][A-Za-z0-9_./-]*)(?::([0-9]+))?$`)
)

// adr is one record: its header line (1-based) and the body lines after it,
// up to the next header.
type adr struct {
	num   string
	title string
	line  int
	body  []string
}

func parseADRs(lines []string) []adr {
	var adrs []adr
	for i, l := range lines {
		if m := adrHeader.FindStringSubmatch(l); m != nil {
			adrs = append(adrs, adr{num: m[1], title: m[2], line: i + 1})
			continue
		}
		if len(adrs) > 0 {
			adrs[len(adrs)-1].body = append(adrs[len(adrs)-1].body, l)
		}
	}
	return adrs
}

// paragraph returns the paragraph starting with prefix, joined into one
// line, and its first line number; "" when the record has none.
func (a adr) paragraph(prefix string) (string, int) {
	for i, l := range a.body {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		text := []string{l}
		for _, next := range a.body[i+1:] {
			if strings.TrimSpace(next) == "" {
				break
			}
			text = append(text, next)
		}
		return strings.Join(text, " "), a.line + 1 + i
	}
	return "", 0
}

// checkADRCodeLocations: every ADR has a Status; every Accepted ADR has an
// In the code paragraph whose backticked paths exist; every ADR-NNN token
// in the file names an ADR that exists.
func checkADRCodeLocations(t *testing.T) {
	c := known(t, "adr-code-locations")
	lines := fileLines(decisionsFile)
	adrs := parseADRs(lines)
	if len(adrs) < minADRHeaders {
		c.Errorf("found %d `## ADR-` headers in %s (floor %d); update adrHeader in internal/drift/adr_test.go", len(adrs), decisionsFile, minADRHeaders)
		return
	}
	have := map[string]bool{}
	for _, a := range adrs {
		if have[a.num] {
			c.Errorf("%s:%d: %s has two headers; renumber one", decisionsFile, a.line, a.num)
		}
		have[a.num] = true
	}
	for i, l := range lines {
		for _, tok := range adrToken.FindAllString(l, -1) {
			if !have[tok] {
				c.Errorf("%s:%d: %s names an ADR that has no `## %s:` header; fix the number or add the record", decisionsFile, i+1, tok, tok)
			}
		}
	}
	for _, a := range adrs {
		status, sline := a.paragraph("**Status.**")
		if status == "" {
			c.Errorf("%s:%d: %s has no **Status.** line; add `**Status.** Accepted.` (or Proposed, or Superseded by ADR-NNN) as the first paragraph after the heading", decisionsFile, a.line, a.num)
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(status, "**Status.**"))
		switch {
		case strings.HasPrefix(value, "Accepted"):
			code, cline := a.paragraph("**In the code.**")
			if code == "" {
				c.Errorf("%s:%d: %s is Accepted but has no **In the code.** paragraph; add one at the end of the record naming the packages, files or scripts that implement it, each in backticks", decisionsFile, a.line, a.num)
				continue
			}
			spans := backtickSpan.FindAllStringSubmatch(code, -1)
			if len(spans) == 0 {
				c.Errorf("%s:%d: %s In the code names no path in backticks; list the packages, files or scripts that implement it", decisionsFile, cline, a.num)
			}
			for _, m := range spans {
				checkCodePath(c, cline, a.num, m[1])
			}
		case strings.HasPrefix(value, "Proposed"), strings.HasPrefix(value, "Superseded"):
		default:
			c.Errorf("%s:%d: %s Status starts with %q; it must start with Accepted, Proposed or Superseded", decisionsFile, sline, a.num, firstWords(value, 4))
		}
	}
}

// checkCodePath checks one backticked token of an In the code paragraph.
func checkCodePath(c *check, line int, num, tok string) {
	const hint = "In the code paragraphs list repo paths only; name symbols in the Decision paragraph"
	m := pathToken.FindStringSubmatch(tok)
	if m == nil {
		c.Errorf("%s:%d: %s In the code has `%s`, which is not a repo path. %s", decisionsFile, line, num, tok, hint)
		return
	}
	p := strings.TrimSuffix(m[1], "/")
	info, err := os.Stat(abs(p))
	if err != nil {
		c.Errorf("%s:%d: %s In the code names `%s`, which does not exist in the repo. %s", decisionsFile, line, num, tok, hint)
		return
	}
	if m[2] == "" {
		return
	}
	n, _ := strconv.Atoi(m[2])
	if info.IsDir() {
		c.Errorf("%s:%d: %s In the code names `%s`, but %s is a directory and cannot have a line number; drop the :%d", decisionsFile, line, num, tok, p, n)
		return
	}
	if have := len(fileLines(p)); have < n {
		c.Errorf("%s:%d: %s In the code names `%s`, but %s has only %d lines; update the line number or drop it", decisionsFile, line, num, tok, p, have)
	}
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}
