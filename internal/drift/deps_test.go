package drift

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// allowedModules is the fixed stack from ADR-018, as CLAUDE.md's Stack
// section lists it, plus the two small libraries the code used when it was
// adopted: google/uuid for UUIDv7 and golang.org/x/text for validation
// messages. A new direct dependency goes through an ADR that supersedes
// ADR-018, and only then onto this list.
var allowedModules = []string{
	"github.com/spf13/cobra",                   // CLI
	"github.com/modelcontextprotocol/go-sdk",   // MCP
	"modernc.org/sqlite",                       // derived index, pure Go
	"github.com/santhosh-tekuri/jsonschema/v6", // schema validation
	"github.com/google/uuid",                   // UUIDv7 ids
	"golang.org/x/text",                        // validation message formatting
}

// checkDeps: go.mod's direct requires are a subset of allowedModules, and a
// version CLAUDE.md names for a module is the one in go.mod.
//
// A require is direct when it stands in the first require block or on a
// line of its own. go mod tidy puts indirect requires in a second block,
// so the `// indirect` comment is not trusted: a hand-written comment on a
// new dependency in the first block does not hide it from this check.
func checkDeps(t *testing.T) {
	c := known(t, "deps")
	lines := fileLines("go.mod")
	if lines == nil {
		t.Fatal("go.mod: cannot be read")
	}
	claude := readFile("CLAUDE.md")
	in := false
	block := 0
	direct := 0
	for i, l := range lines {
		s := strings.TrimSpace(l)
		var mod, ver string
		switch {
		case strings.HasPrefix(s, "require ("):
			in = true
			block++
			continue
		case in && s == ")":
			in = false
			continue
		case in:
			if block > 1 {
				continue
			}
			if f := strings.Fields(s); len(f) >= 2 {
				mod, ver = f[0], f[1]
			}
		case strings.HasPrefix(s, "require "):
			if f := strings.Fields(s); len(f) >= 3 {
				mod, ver = f[1], f[2]
			}
		}
		if mod == "" {
			continue
		}
		direct++
		if !slices.Contains(allowedModules, mod) {
			c.Errorf("go.mod:%d: %s is a direct dependency outside the fixed stack (ADR-018); a new dependency needs an ADR that supersedes ADR-018, then a line in allowedModules in internal/drift/deps_test.go (if it is only indirect, run go mod tidy, which moves it to the second require block)", i+1, mod)
		}
		pinned := regexp.MustCompile("`" + regexp.QuoteMeta(mod) + "` (v[0-9][^ ,;)]*)")
		if m := pinned.FindStringSubmatch(claude); m != nil && m[1] != ver {
			c.Errorf("go.mod:%d: %s is %s but CLAUDE.md names %s; update CLAUDE.md in the same PR as the bump", i+1, mod, ver, m[1])
		}
	}
	if direct == 0 {
		c.Errorf("go.mod: found no direct requires; update the parser in internal/drift/deps_test.go")
	}
}
