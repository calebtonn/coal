package drift

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
)

const (
	schemaFile  = "spec/intent-manifest.schema.json"
	buildScript = "site/build.sh"
	siteBase    = "https://coalmine.io/"
	markerFile  = "internal/marker/marker.go"
)

// schemaDoc returns the parsed schema.
func schemaDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(mustRead(t, schemaFile)), &doc); err != nil {
		t.Fatalf("%s: %v", schemaFile, err)
	}
	return doc
}

// schemaID returns the schema's $id.
func schemaID(t *testing.T) string {
	t.Helper()
	id, _ := schemaDoc(t)["$id"].(string)
	if id == "" {
		t.Fatalf("%s: no $id", schemaFile)
	}
	return id
}

// checkSchemaManifest: the schema's $id is the URL site/build.sh publishes
// it at, and the constant the code writes as $schema is the same URL. The
// script is read, not run, so the check needs nothing beyond go.
func checkSchemaManifest(t *testing.T) {
	c := known(t, "schema-published/manifest")
	id := schemaID(t)
	script := mustRead(t, buildScript)
	re := regexp.MustCompile(`(?m)^cp "\$root/spec/intent-manifest\.schema\.json" "\$out/([^"]+)"`)
	m := re.FindStringSubmatchIndex(script)
	if m == nil {
		c.Errorf("%s: no line copies %s into the site; add `cp \"$root/%s\" \"$out/<path under the $id>\"`, or update the regexp in internal/drift/schema_test.go", buildScript, schemaFile, schemaFile)
		return
	}
	published := siteBase + script[m[2]:m[3]]
	if id != published {
		c.Errorf("%s: $id is %s but %s:%d publishes the file at %s; change one so they agree", schemaFile, id, buildScript, lineAt(script, m[0]), published)
	}
	if manifest.SchemaURL != id {
		c.Errorf("internal/manifest/manifest.go: SchemaURL is %s but %s has $id %s; change the constant", manifest.SchemaURL, schemaFile, id)
	}
}

// checkSchemaMarker: the .coal marker's $schema URL names a file under
// spec/ that site/build.sh publishes at that path. Known failure #27.
func checkSchemaMarker(t *testing.T) {
	c := known(t, "schema-published/marker")
	url := marker.SchemaURL
	line := 0
	for i, l := range fileLines(markerFile) {
		if strings.Contains(l, "SchemaURL =") {
			line = i + 1
			break
		}
	}
	path, ok := strings.CutPrefix(url, siteBase)
	if !ok {
		c.Errorf("%s:%d: SchemaURL is %s, which is not under %s; the site publishes schemas under %sschema/", markerFile, line, url, siteBase, siteBase)
		return
	}
	script := mustRead(t, buildScript)
	re := regexp.MustCompile(`(?m)^cp "\$root/([^"]+)" "\$out/` + regexp.QuoteMeta(path) + `"`)
	m := re.FindStringSubmatch(script)
	if m == nil {
		c.Errorf("%s:%d: SchemaURL is %s but %s has no cp line publishing a file at %s; add the marker schema under spec/ and a cp line in %s (#27)", markerFile, line, url, buildScript, path, buildScript)
		return
	}
	if !exists(m[1]) {
		c.Errorf("%s: copies %s to %s, but %s does not exist; add the file", buildScript, m[1], path, m[1])
	}
}

// checkExamples: every file in spec/examples validates against the schema.
func checkExamples(t *testing.T) {
	c := known(t, "examples")
	files, _ := filepath.Glob(abs("spec/examples/*.json"))
	if len(files) == 0 {
		c.Errorf("spec/examples: no *.json files; restore the example manifests or update the glob in internal/drift/schema_test.go")
		return
	}
	for _, f := range files {
		rel := "spec/examples/" + filepath.Base(f)
		b, err := os.ReadFile(f)
		if err != nil {
			c.Errorf("%s: %v", rel, err)
			continue
		}
		if err := manifest.Validate(b); err != nil {
			c.Errorf("%s: does not validate against %s:\n    %s\n  fix the example, or if the schema changed on purpose, update the example in the same PR", rel, schemaFile, strings.ReplaceAll(err.Error(), "\n", "\n    "))
		}
	}
}
