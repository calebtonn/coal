package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const examplePath = "../../spec/examples/budget-tracker.manifest.json"

func readExample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mutate decodes the example, applies fn, and re-encodes it.
func mutate(t *testing.T, fn func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readExample(t), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSchemaCompiles(t *testing.T) {
	if _, err := compiled(); err != nil {
		t.Fatal(err)
	}
}

func TestExampleValidates(t *testing.T) {
	m, err := Parse(readExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rev != 3 || len(m.Artifacts) != 1 || len(m.Artifacts[0].Versions) != 1 {
		t.Fatalf("unexpected decode: rev %d, artifacts %d", m.Rev, len(m.Artifacts))
	}
}

func TestNewRoundTrip(t *testing.T) {
	m := New("Household budget tracker as a single HTML page", SourceUser, Provenance{Tool: "coal-cli"})
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("}\n")) || !bytes.Contains(b, []byte("\n  \"version\": \"0.1\"")) {
		t.Fatalf("not two-space indented with trailing newline:\n%s", b)
	}
	if !bytes.Contains(b, []byte(`"artifacts": []`)) {
		t.Fatalf("artifacts should be an empty array:\n%s", b)
	}
	if err := Validate(b); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m.ID || got.Rev != 1 || got.Status != StatusActive || got.Intent.Source != SourceUser {
		t.Fatalf("round trip changed fields: %+v", got)
	}
}

func TestNilArtifactsMarshalAsArray(t *testing.T) {
	m := New("x", SourceModel, Provenance{Tool: "t"})
	m.Artifacts = nil
	b, _ := m.Marshal()
	if err := Validate(b); err != nil {
		t.Fatal(err)
	}
}

func TestTokenSessionRefFails(t *testing.T) {
	for _, ref := range []string{"Bearer abc", "sk-abc", "ghp_abc", "xoxb-1", "AKIAABC"} {
		b := mutate(t, func(m map[string]any) {
			m["provenance"].(map[string]any)["sessionRef"] = ref
		})
		err := Validate(b)
		if err == nil || !strings.Contains(err.Error(), "/provenance/sessionRef: looks like an auth token") {
			t.Fatalf("%q: want sessionRef error, got %v", ref, err)
		}
	}
}

func TestNonV7IDFails(t *testing.T) {
	b := mutate(t, func(m map[string]any) {
		m["id"] = "019a3f2c-8e4b-4d31-9a5c-2f6b1c8d4e70"
	})
	if err := Validate(b); err == nil || !strings.Contains(err.Error(), "/id") {
		t.Fatalf("want /id error, got %v", err)
	}
}

func TestUnknownTopLevelFieldsPassAndSurvive(t *testing.T) {
	b := mutate(t, func(m map[string]any) {
		m["futureField"] = map[string]any{"a": 1}
	})
	m, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`"futureField": {`)) {
		t.Fatalf("unknown field dropped on rewrite:\n%s", out)
	}
	if err := Validate(out); err != nil {
		t.Fatal(err)
	}
}

func TestTimestampWithoutZoneFails(t *testing.T) {
	b := mutate(t, func(m map[string]any) {
		m["createdAt"] = "2026-11-01T02:14:09"
	})
	if err := Validate(b); err == nil || !strings.Contains(err.Error(), "/createdAt") {
		t.Fatalf("want /createdAt error, got %v", err)
	}
}

func TestErrorsOnePerLine(t *testing.T) {
	b := mutate(t, func(m map[string]any) {
		m["status"] = "bogus"
		m["rev"] = 0
	})
	err := Validate(b)
	if err == nil {
		t.Fatal("want error")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "/rev: ") || !strings.HasPrefix(lines[1], "/status: ") {
		t.Fatalf("want two lines for /rev and /status, got:\n%s", err)
	}
}

func TestNewIDIsV7AndSorts(t *testing.T) {
	prev := NewID()
	for range 1000 {
		id := NewID()
		u, err := uuid.Parse(id)
		if err != nil || u.Version() != 7 || id != strings.ToLower(id) {
			t.Fatalf("bad id %q: %v", id, err)
		}
		if id <= prev {
			t.Fatalf("%s does not sort after %s", id, prev)
		}
		prev = id
	}
}

func TestNowEndsInZ(t *testing.T) {
	if n := Now(); !strings.HasSuffix(n, "Z") || len(n) != len("2026-11-04T00:00:00Z") {
		t.Fatalf("bad timestamp %q", n)
	}
}

func TestArtifactAgentTraceExtRoundTrips(t *testing.T) {
	ref := map[string]any{"uri": "https://example.com/traces/42.json", "hash": "sha256:" + strings.Repeat("ab", 32)}
	b := mutate(t, func(m map[string]any) {
		m["artifacts"].([]any)[0].(map[string]any)["ext"] = map[string]any{"dev.agent-trace": ref}
	})
	m, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := m.Artifacts[0].Ext["dev.agent-trace"]
	if !ok {
		t.Fatal("ext not decoded onto the artifact")
	}
	out, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(out)
	if err != nil {
		t.Fatalf("rewritten manifest invalid: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(again.Artifacts[0].Ext["dev.agent-trace"], &got); err != nil || got["uri"] != ref["uri"] || got["hash"] != ref["hash"] {
		t.Fatalf("round trip gave %s (%v), first read %s", again.Artifacts[0].Ext["dev.agent-trace"], err, raw)
	}

	bad := mutate(t, func(m map[string]any) {
		m["artifacts"].([]any)[0].(map[string]any)["ext"] = map[string]any{"dev.agent-trace": "not an object"}
	})
	if err := Validate(bad); err == nil || !strings.Contains(err.Error(), "/artifacts/0/ext") {
		t.Fatalf("want an /artifacts/0/ext error, got %v", err)
	}
}
