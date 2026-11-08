package drift

import (
	"encoding/json"
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	siteFile   = "site/index.html"
	readmeFile = "README.md"
)

var (
	// readmePhaseHeader is the header row of the README phases table. Rows
	// are read from the line after it to the first line that is not a
	// table row, so another numbered table elsewhere in the README is not
	// mistaken for a phase.
	readmePhaseHeader = regexp.MustCompile(`^\| Phase \| Name \|`)
	// readmePhaseRow matches a row of the README phases table: | N | Name | ...
	readmePhaseRow = regexp.MustCompile(`^\|\s*([0-9]+)\s*\|\s*([^|]+?)\s*\|`)
	// Site phases are class="stratum" blocks with the name in an <h3>.
	stratumStart = regexp.MustCompile(`class="stratum"`)
	stratumName  = regexp.MustCompile(`<h3>([^<]*)</h3>`)
)

// readmePhases returns the phase names in the README phases table and the
// line of each. Only the table under readmePhaseHeader is read.
func readmePhases(lines []string) (names []string, lineNos []int, headerFound bool) {
	in := false
	for i, l := range lines {
		if !in {
			if readmePhaseHeader.MatchString(l) {
				in, headerFound = true, true
			}
			continue
		}
		if !strings.HasPrefix(l, "|") {
			break
		}
		if m := readmePhaseRow.FindStringSubmatch(l); m != nil {
			names = append(names, m[2])
			lineNos = append(lineNos, i+1)
		}
	}
	return names, lineNos, headerFound
}

// checkPhases: the phases on the site are a prefix of the README table,
// same order and names. The README may list more (a proposed phase).
func checkPhases(t *testing.T) {
	c := known(t, "phases")
	readme, readmeLines, headerFound := readmePhases(fileLines(readmeFile))
	if !headerFound {
		c.Errorf("found no phases table header (| Phase | Name |) in %s; update the selector readmePhaseHeader in internal/drift/site_test.go", readmeFile)
		return
	}
	if len(readme) < minReadmePhases {
		c.Errorf("found %d phases in %s (floor %d); update the selector readmePhaseRow in internal/drift/site_test.go", len(readme), readmeFile, minReadmePhases)
		return
	}
	site := mustRead(t, siteFile)
	starts := stratumStart.FindAllStringIndex(site, -1)
	var names []string
	var siteLines []int
	for i, s := range starts {
		end := len(site)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		// A block without an <h3> keeps its place with a placeholder name,
		// so the phases after it are still compared at the right positions.
		name := "<no h3>"
		if m := stratumName.FindStringSubmatchIndex(site[s[0]:end]); m != nil {
			name = html.UnescapeString(site[s[0]+m[2] : s[0]+m[3]])
		} else {
			c.Errorf("%s:%d: a class=\"stratum\" block has no <h3>; update the selector stratumName in internal/drift/site_test.go", siteFile, lineAt(site, s[0]))
		}
		names = append(names, name)
		siteLines = append(siteLines, lineAt(site, s[0]))
	}
	if len(names) < minSitePhases {
		c.Errorf("found %d phases in %s; update the selector stratumStart in internal/drift/site_test.go", len(names), siteFile)
		return
	}
	for i, n := range names {
		if i >= len(readme) {
			c.Errorf("%s:%d: the site lists phase %d (%s) but the README table has %d phases; the site shows a prefix of the README's phases", siteFile, siteLines[i], i+1, n, len(readme))
			break
		}
		if n == "<no h3>" {
			continue // already reported above
		}
		if n != readme[i] {
			c.Errorf("%s:%d: phase %d is %q on the site but %q in %s:%d; the site lists a prefix of the README's phases, same order and names", siteFile, siteLines[i], i+1, n, readme[i], readmeFile, readmeLines[i])
		}
	}
}

var (
	excerptFigure = regexp.MustCompile(`<figure[^>]*aria-label="Example Intent Manifest, an excerpt"[^>]*>`)
	preBlock      = regexp.MustCompile(`(?s)<pre[^>]*>(.*?)</pre>`)
	schemaHref    = regexp.MustCompile(`href="(https://coalmine\.io/schema/[^"]*)"`)
)

// checkSiteFacts: the manifest excerpt on the site uses only fields the
// schema defines, its enum values are real, and the schema URL it shows,
// in the excerpt and in the section's link, is the schema's $id.
func checkSiteFacts(t *testing.T) {
	c := known(t, "site-facts")
	site := mustRead(t, siteFile)
	fig := excerptFigure.FindStringIndex(site)
	if fig == nil {
		c.Errorf("%s: no <figure aria-label=\"Example Intent Manifest, an excerpt\">; restore the figure or update the selector excerptFigure in internal/drift/site_test.go", siteFile)
		return
	}
	figLine := lineAt(site, fig[0])
	rest := site[fig[1]:]
	end := strings.Index(rest, "</figure>")
	if end < 0 {
		c.Errorf("%s:%d: the excerpt figure has no </figure>", siteFile, figLine)
		return
	}
	pm := preBlock.FindStringSubmatch(rest[:end])
	if pm == nil {
		c.Errorf("%s:%d: the excerpt figure has no <pre>; update the selector preBlock in internal/drift/site_test.go", siteFile, figLine)
		return
	}
	text := html.UnescapeString(tag.ReplaceAllString(pm[1], ""))
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		c.Errorf("%s:%d: the manifest excerpt is not valid JSON once the tags are stripped (%v); fix the excerpt", siteFile, figLine, err)
		return
	}
	schema := schemaDoc(t)
	id, _ := schema["$id"].(string)
	if s, _ := doc["$schema"].(string); s != id {
		c.Errorf("%s:%d: the excerpt's $schema is %q but %s has $id %q; update the excerpt", siteFile, figLine, s, schemaFile, id)
	}
	secStart := strings.LastIndex(site[:fig[0]], "<section")
	secEnd := strings.Index(site[fig[1]:], "</section>")
	if secStart < 0 || secEnd < 0 {
		c.Errorf("%s:%d: the excerpt figure is not inside a <section>; update the section lookup in internal/drift/site_test.go", siteFile, figLine)
	} else {
		section := site[secStart : fig[1]+secEnd]
		hrefs := schemaHref.FindAllStringSubmatchIndex(section, -1)
		if len(hrefs) == 0 {
			c.Errorf("%s:%d: the section with the excerpt has no link to %sschema/...; add the schema link or update schemaHref in internal/drift/site_test.go", siteFile, lineAt(site, secStart), siteBase)
		}
		for _, h := range hrefs {
			if got := section[h[2]:h[3]]; got != id {
				c.Errorf("%s:%d: the schema link points at %s but %s has $id %s; update the link", siteFile, lineAt(site, secStart+h[0]), got, schemaFile, id)
			}
		}
	}
	walkKeys(c, figLine, schema, schema, doc, "")
}

// walkKeys resolves every key path in value against the schema node: object
// keys through properties, arrays through items, $ref through $defs.
// Anything under ext is free-form and skipped. A string value is checked
// against the node's enum when it has one.
func walkKeys(c *check, line int, root map[string]any, node any, value any, path string) {
	schema := deref(root, node)
	switch v := value.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		for _, k := range slices.Sorted(mapKeys(v)) {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if k == "ext" {
				continue
			}
			child, ok := props[k]
			if !ok {
				c.Errorf("%s:%d: the manifest excerpt uses %s, which %s does not define; fix the excerpt, or add the field to the schema in the same PR", siteFile, line, p, schemaFile)
				continue
			}
			walkKeys(c, line, root, child, v[k], p)
		}
	case []any:
		items, ok := schema["items"]
		if !ok {
			c.Errorf("%s:%d: the manifest excerpt has an array at %s but %s has no items for it; fix the excerpt", siteFile, line, path, schemaFile)
			return
		}
		for _, el := range v {
			walkKeys(c, line, root, items, el, path+"[]")
		}
	case string:
		if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, any(v)) {
			c.Errorf("%s:%d: the manifest excerpt sets %s to %q, which is not in the schema enum %v; use one of them", siteFile, line, path, v, enum)
		}
	}
}

// deref follows local $ref chains into $defs and returns the schema node as
// a map, empty when the node is not an object.
func deref(root map[string]any, node any) map[string]any {
	for range 8 {
		m, ok := node.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		name, ok := strings.CutPrefix(ref, "#/$defs/")
		if !ok {
			return m
		}
		defs, _ := root["$defs"].(map[string]any)
		node = defs[name]
	}
	return map[string]any{}
}

func mapKeys(m map[string]any) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
