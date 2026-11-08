package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportClaudeCode(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	proj := filepath.Join(root, "budget")
	os.MkdirAll(filepath.Join(root, "projects", "-budget"), 0o700)
	os.MkdirAll(proj, 0o700)
	os.WriteFile(filepath.Join(proj, "index.html"), []byte("<h1>Budget</h1>"), 0o600)
	line := `{"type":"user","sessionId":"s1","cwd":"` + proj + `","timestamp":"2026-10-01T09:00:00Z","message":{"content":"Build a household budget tracker page"}}
{"type":"assistant","sessionId":"s1","cwd":"` + proj + `","timestamp":"2026-10-01T09:01:00Z","message":{"id":"m1","content":[{"type":"tool_use","name":"Write","input":{"file_path":"` + proj + `/index.html"}},{"type":"text","text":"Done.\n\nNext step: Add CSV export"}]}}
`
	os.WriteFile(filepath.Join(root, "projects", "-budget", "s1.jsonl"), []byte(line), 0o600)
	dir := filepath.Join(root, "projects")

	out := coal(t, home, "import", "claude-code", "--dir", dir)
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "summary (user): Build a household budget tracker page") || !strings.Contains(out, "next step: Add CSV export") {
		t.Fatalf("dry run:\n%s", out)
	}
	if strings.Contains(coal(t, home, "find", "budget"), "budget") {
		t.Fatal("dry run wrote a manifest")
	}
	out = coal(t, home, "import", "claude-code", "--dir", dir, "--accept")
	if !strings.Contains(out, "created ") {
		t.Fatalf("accept:\n%s", out)
	}
	if !strings.Contains(coal(t, home, "find", "budget"), "Build a household budget tracker page") {
		t.Error("find after import")
	}
	if out = coal(t, home, "import", "claude-code", "--dir", dir, "--json"); !strings.Contains(out, `"alreadyRecorded"`) {
		t.Errorf("second import:\n%s", out)
	}
	for _, args := range [][]string{{"import", "claude-code"}, {"import", "claude-code", "--dir", dir, "--markers"}} {
		root := rootCmd()
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		root.SetArgs(append([]string{"--home", home}, args...))
		if root.Execute() == nil {
			t.Errorf("coal %v should fail", args)
		}
	}
}
