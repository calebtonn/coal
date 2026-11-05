package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "claude-code", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFromClaudeCodeRecorded(t *testing.T) {
	cases := []struct {
		file, hook, kind string
		check            func(*Event) bool
	}{
		{"session-start.json", "SessionStart", KindSessionStart, func(e *Event) bool { return e.Source == "startup" && e.Model == "claude-opus-5-5" }},
		{"user-prompt-submit.json", "UserPromptSubmit", KindPrompt, func(e *Event) bool { return e.Prompt == "Build me a household budget tracker as a single HTML page" }},
		{"post-tool-use-write.json", "PostToolUse", KindFileWritten, func(e *Event) bool { return e.Path == "/PROJECT/budget/budget.html" }},
		{"post-tool-use-edit.json", "PostToolUse", KindFileWritten, func(e *Event) bool { return e.Path == "/PROJECT/budget/budget.html" }},
		{"stop.json", "Stop", KindTurnEnd, func(e *Event) bool { return strings.HasSuffix(e.LastAssistantMessage, "Next step: add CSV export") }},
		{"session-end.json", "SessionEnd", KindSessionEnd, func(e *Event) bool { return e.Reason == "prompt_input_exit" }},
		{"session-start-resume.json", "", KindSessionStart, func(e *Event) bool { return e.SessionRef != SessionRef("7c1f3e2a-5b4d-4e8f-9a01-2b3c4d5e6f70") }},
	}
	for _, c := range cases {
		e, err := FromClaudeCode(c.hook, load(t, c.file))
		if err != nil || e == nil {
			t.Fatalf("%s: %v, %v", c.file, e, err)
		}
		if e.Kind != c.kind || e.Tool != ToolClaudeCode || e.Cwd != "/PROJECT/budget" ||
			!strings.HasPrefix(e.SessionRef, "claude-code:session:") || e.TranscriptPath == "" || !c.check(e) {
			t.Errorf("%s: unexpected event %+v", c.file, e)
		}
		if len(e.ID) != 36 || e.ID[14] != '7' || e.At == "" {
			t.Errorf("%s: id %q at %q", c.file, e.ID, e.At)
		}
		b, _ := json.Marshal(e)
		if !strings.Contains(string(b), `"sessionRef"`) || strings.Contains(string(b), `"model":""`) {
			t.Errorf("%s: json %s", c.file, b)
		}
	}
}

func TestFromClaudeCodeSkips(t *testing.T) {
	for _, p := range []string{
		`{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`,
		`{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{}}`,
		`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/x"}}`,
		`{"session_id":"s","hook_event_name":"Notification"}`,
	} {
		if e, err := FromClaudeCode("", []byte(p)); e != nil || err != nil {
			t.Errorf("%s: got %+v, %v", p, e, err)
		}
	}
	e, _ := FromClaudeCode("PostToolUse", []byte(`{"session_id":"s","tool_name":"NotebookEdit","tool_input":{"notebook_path":"/p/a.ipynb"}}`))
	if e == nil || e.Path != "/p/a.ipynb" {
		t.Errorf("notebook: %+v", e)
	}
	if _, err := FromClaudeCode("Stop", []byte(`not json`)); err == nil {
		t.Error("bad json: no error")
	}
	if _, err := FromClaudeCode("Stop", []byte(`{}`)); err == nil {
		t.Error("no session_id: no error")
	}
	msg := strings.Repeat("a", 9000) + "\nNext step: ship it"
	b, _ := json.Marshal(map[string]string{"session_id": "s", "last_assistant_message": msg})
	e, _ = FromClaudeCode("Stop", b)
	if _, next := ExtractState(e.LastAssistantMessage); next != "ship it" || !strings.Contains(e.LastAssistantMessage, "\n…\n") ||
		len([]rune(e.LastAssistantMessage)) != MaxText+3 {
		t.Errorf("head and tail: next %q, %d runes", next, len([]rune(e.LastAssistantMessage)))
	}
	long := strings.Repeat("é", MaxText+50)
	e, _ = FromClaudeCode("UserPromptSubmit", []byte(`{"session_id":"s","prompt":"`+long+`"}`))
	if len([]rune(e.Prompt)) != MaxText {
		t.Errorf("prompt cap: %d", len([]rune(e.Prompt)))
	}
}

func TestPickSummary(t *testing.T) {
	accept := map[string]string{
		"Build me a household budget tracker as a single HTML page": "Build me a household budget tracker as a single HTML page",
		"  Fix the   login redirect\n\nIt loops forever.":           "Fix the login redirect",
		"Why does this fail?\n<pasted_content id=\"1\">\nTraceback (most recent call last)\n  File \"a.py\", line 1\n</pasted_content id=\"1\">\nPlease explain the cause": "Why does this fail?",
		"Hi\nplease add a dark mode toggle": "please add a dark mode toggle",
	}
	for in, want := range accept {
		got, ok := PickSummary(in)
		if !ok || got != want {
			t.Errorf("PickSummary(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	reject := []string{
		"", "/review the open pull request now", "continue", "Yes!", "go on.", "OK", "Keep going",
		"do it", "yes please go ahead and continue", "fix the bug", "Next.",
		"<cross-session-message from=\"x\">please build the budget page now</cross-session-message>",
		"  <task-notification>the build finished with three warnings</task-notification>",
		"Hi there\n<pasted_content id=\"2\">\nlots of pasted words here\n</pasted_content id=\"2\">",
		"TypeError: x is not a function\n    at foo (a.js:1:2)\n    at bar (a.js:3:4)\nwhat is this",
		"func main() {\n\tfmt.Println(1)\n}\nwhy no output here",
		"Traceback (most recent call last):\n  File \"x.py\", line 2, in <module>\nValueError: bad value\nhelp me",
	}
	for _, in := range reject {
		if got, ok := PickSummary(in); ok {
			t.Errorf("PickSummary(%q) accepted %q", in, got)
		}
	}
	long := strings.Repeat("word ", 100)
	got, ok := PickSummary(long)
	if !ok || len(got) > MaxSummary || strings.HasSuffix(got, " ") || !strings.HasSuffix(got, "word") {
		t.Errorf("long: %q", got)
	}
}

func TestExtractState(t *testing.T) {
	var p struct {
		Msg string `json:"last_assistant_message"`
	}
	_ = json.Unmarshal(load(t, "stop.json"), &p)
	sum, next := ExtractState(p.Msg)
	if !strings.HasPrefix(sum, "I built budget.html") || strings.Contains(sum, "localStorage") || next != "add CSV export" {
		t.Errorf("recorded: %q, %q", sum, next)
	}
	cases := map[string]string{
		"Done.\n\n- **Next step:** wire up the API.":    "wire up the API",
		"Done.\n\nNext steps:\n1. write tests\n2. ship": "write tests",
		"Done.\nnext: deploy":                           "deploy",
		"Next, I will refactor the parser.":             "",
		"The next step is unclear.":                     "",
	}
	for in, want := range cases {
		if _, got := ExtractState(in); got != want {
			t.Errorf("ExtractState(%q) next = %q, want %q", in, got, want)
		}
	}
	sum, _ = ExtractState(strings.Repeat("abc ", 200))
	if len(sum) > MaxStateSummary || strings.HasSuffix(sum, " ") {
		t.Errorf("summary cap: %d", len(sum))
	}
}

func TestResumeOutput(t *testing.T) {
	m := manifest.New("Household budget tracker as a single HTML page", manifest.SourceUser, manifest.Provenance{Tool: "claude-code"})
	m.Intent.Goal = "Track monthly spending"
	m.State.NextStep = "Add CSV export"
	m.State.OpenQuestions = []string{"Which currency?"}
	m.Artifacts = []manifest.Artifact{{Title: "budget.html", URI: "file:///p/budget/budget.html"}}
	b, err := ResumeOutput(m, "")
	if err != nil {
		t.Fatal(err)
	}
	var out resumeJSON
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.SystemMessage != `COAL: You left off at "Add CSV export". 1 open question.` {
		t.Errorf("systemMessage: %q", out.SystemMessage)
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Error("hookEventName")
	}
	for _, want := range []string{m.ID, "Household budget tracker", "Track monthly spending", "Add CSV export",
		"Which currency?", "budget.html (file:///p/budget/budget.html)", "coal_update_state", "first reply"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context lacks %q:\n%s", want, ctx)
		}
	}
	if strings.Contains(strings.ToLower(string(b)), "memory") {
		t.Error("output uses the word memory")
	}

	m.State.NextStep, m.State.OpenQuestions, m.Intent.Source = "", nil, manifest.SourceModel
	if got := ResumeLine(m, m.ID[:10]); got != `COAL: Resuming "Household budget tracker as a single HTML page". coal rename `+m.ID[:10]+` "..." to fix the summary.` {
		t.Errorf("model line: %q", got)
	}
	m.State.NextStep, m.State.OpenQuestions, m.Intent.Source = "x", []string{"a", "b"}, manifest.SourceUser
	if got := ResumeLine(m, ""); got != `COAL: You left off at "x". 2 open questions.` {
		t.Errorf("two questions: %q", got)
	}

	for i := range 200 {
		m.Artifacts = append(m.Artifacts, manifest.Artifact{Title: strings.Repeat("t", 90), URI: "file:///" + strings.Repeat("u", 200) + string(rune('a'+i%26))})
	}
	m.State.Summary = strings.Repeat("long ", 200)
	b, _ = ResumeOutput(m, "")
	_ = json.Unmarshal(b, &out)
	ctx = out.HookSpecificOutput.AdditionalContext
	if n := len([]rune(ctx)); n >= MaxContext || !strings.Contains(ctx, "more") || !strings.Contains(ctx, "coal_update_state") {
		t.Errorf("big context: %d chars", n)
	}
}
