package summarize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// jl builds one transcript line.
func jl(typ, cwd string, msg map[string]any) string {
	b, _ := json.Marshal(map[string]any{"type": typ, "sessionId": "s1", "cwd": cwd, "timestamp": "2026-11-06T10:00:00Z", "message": msg})
	return string(b) + "\n"
}

func user(cwd, text string) string { return jl("user", cwd, map[string]any{"content": text}) }

func assistant(cwd, id string, blocks ...map[string]any) string {
	return jl("assistant", cwd, map[string]any{"id": id, "model": "m", "content": blocks})
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func tool(name, key, path string) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": map[string]any{key: path}}
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFromTranscript(t *testing.T) {
	cwd := "/work/budget"
	body := user(cwd, "Build a budget tracker page") +
		assistant(cwd, "m1", text("Writing it."), tool("Write", "file_path", cwd+"/index.html")) +
		"not json\n" +
		user(cwd, "Now add a chart by category") +
		assistant(cwd, "m2", tool("Edit", "file_path", "index.html"), tool("MultiEdit", "file_path", cwd+"/app.js"),
			tool("NotebookEdit", "notebook_path", cwd+"/a.ipynb"),
			map[string]any{"type": "tool_use", "name": "NotebookEdit", "input": map[string]any{"file_path": cwd + "/b.ipynb", "notebook_path": cwd + "/b.ipynb"}}, tool("Read", "file_path", cwd+"/README.md")) +
		user(cwd, "<system-reminder>injected, not typed</system-reminder>") +
		assistant(cwd, "m3", text("Added a spending chart by category.")) +
		assistant(cwd, "m3", text("Next step: Add CSV export"))
	p := write(t, body)
	summary, next, files, err := FromTranscript(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Added a spending chart by category." || next != "Add CSV export" {
		t.Errorf("summary %q, next %q", summary, next)
	}
	want := []string{cwd + "/index.html", cwd + "/app.js", cwd + "/a.ipynb", cwd + "/b.ipynb"}
	if !slices.Equal(files, want) {
		t.Errorf("files %v", files)
	}
	tr, _ := ReadFile(p, 0)
	if tr.LastPrompt() != "Now add a chart by category" || tr.BadLines != 1 {
		t.Errorf("last prompt %q, bad lines %d", tr.LastPrompt(), tr.BadLines)
	}
}

func TestFromTranscriptPromptFallback(t *testing.T) {
	long := strings.Repeat("word ", 100)
	p := write(t, user("/w", "first prompt here please")+user("/w", long)+assistant("/w", "m1", tool("Write", "file_path", "/w/x.md")))
	summary, next, files, err := FromTranscript(p, 0)
	if err != nil || next != "" || len(files) != 1 {
		t.Fatalf("%q %q %v %v", summary, next, files, err)
	}
	if n := len([]rune(summary)); n > 300 || n < 250 || !strings.HasPrefix(summary, "word word") || strings.HasSuffix(summary, " ") {
		t.Errorf("fallback summary (%d runes) %q", n, summary)
	}
}

func TestFromTranscriptTail(t *testing.T) {
	early := user("/w", "An early prompt that the tail must not see")
	pad := strings.Repeat(assistant("/w", "pad", text(strings.Repeat("x", 200))), 200)
	last := user("/w", "The last prompt of the session") + assistant("/w", "m9", text("Done with the tail."))
	p := write(t, early+pad+last)
	tr, err := ReadFile(p, int64(len(last)+300))
	if err != nil {
		t.Fatal(err)
	}
	if tr.BadLines != 0 || slices.Contains(tr.Prompts, "An early prompt that the tail must not see") || tr.LastPrompt() != "The last prompt of the session" {
		t.Errorf("tail: bad %d, prompts %v", tr.BadLines, tr.Prompts)
	}
	if summary, _, _, _ := FromTranscript(p, 1000); summary != "Done with the tail." {
		t.Errorf("tail summary %q", summary)
	}
}

func TestReadFileRefusesSymlink(t *testing.T) {
	p := write(t, user("/w", "hello there you all"))
	link := filepath.Join(t.TempDir(), "link.jsonl")
	os.Symlink(p, link)
	if _, err := ReadFile(link, 0); err == nil {
		t.Error("a symlinked transcript was read")
	}
}

// Fable review M11 pass 2, P2: a trivial last prompt gives no summary.
func TestFromTranscriptTrivialPrompt(t *testing.T) {
	for _, prompt := range []string{"hello", "/exit"} {
		p := write(t, user("/w", prompt)+assistant("/w", "m1", tool("Write", "file_path", "/w/x.md")))
		summary, next, files, err := FromTranscript(p, 0)
		if err != nil || summary != "" || next != "" || len(files) != 1 {
			t.Errorf("%q: summary %q, next %q, files %v, %v", prompt, summary, next, files, err)
		}
	}
}
