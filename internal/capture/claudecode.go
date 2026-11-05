package capture

import (
	"encoding/json"
	"fmt"

	"github.com/calebtonn/coal/internal/manifest"
)

// ccPayload holds the Claude Code hook input fields COAL reads.
type ccPayload struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	Cwd                  string `json:"cwd"`
	HookEventName        string `json:"hook_event_name"`
	Source               string `json:"source"`
	Model                string `json:"model"`
	Prompt               string `json:"prompt"`
	ToolName             string `json:"tool_name"`
	LastAssistantMessage string `json:"last_assistant_message"`
	Reason               string `json:"reason"`
	ToolInput            struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
}

// SessionRef returns the sessionRef for a Claude Code session id.
func SessionRef(sessionID string) string {
	return "claude-code:session:" + sessionID
}

// FromClaudeCode translates one Claude Code hook payload into an Event.
// hookEvent is the hook event name, such as "PostToolUse"; when empty the
// payload's hook_event_name is used. Events COAL does not capture, and
// tool calls that write no file, return nil, nil.
func FromClaudeCode(hookEvent string, payload []byte) (*Event, error) {
	var p ccPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("claude-code payload: %w", err)
	}
	if hookEvent == "" {
		hookEvent = p.HookEventName
	}
	if p.SessionID == "" {
		return nil, fmt.Errorf("claude-code payload: no session_id")
	}
	e := &Event{
		ID:             manifest.NewID(),
		At:             manifest.Now(),
		Tool:           ToolClaudeCode,
		SessionRef:     SessionRef(p.SessionID),
		Cwd:            p.Cwd,
		TranscriptPath: p.TranscriptPath,
	}
	switch hookEvent {
	case "SessionStart":
		e.Kind, e.Source, e.Model = KindSessionStart, p.Source, p.Model
	case "UserPromptSubmit":
		e.Kind, e.Prompt = KindPrompt, truncate(p.Prompt, MaxText)
	case "PostToolUse":
		switch p.ToolName {
		case "Write", "Edit", "MultiEdit":
			e.Path = p.ToolInput.FilePath
		case "NotebookEdit":
			e.Path = p.ToolInput.NotebookPath
			if e.Path == "" {
				e.Path = p.ToolInput.FilePath
			}
		}
		if e.Path == "" {
			return nil, nil
		}
		e.Kind = KindFileWritten
	case "Stop":
		e.Kind = KindTurnEnd
		e.LastAssistantMessage = headTail(p.LastAssistantMessage, MaxText)
	case "SessionEnd":
		e.Kind, e.Reason = KindSessionEnd, p.Reason
	default:
		return nil, nil
	}
	return e, nil
}
