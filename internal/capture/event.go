// Package capture holds the capture contract (ADR-011): the events that
// tool adapters drop into the queue, and the Claude Code translation.
package capture

// Tool names.
const ToolClaudeCode = "claude-code"

// Event kinds.
const (
	KindSessionStart = "sessionStart"
	KindPrompt       = "prompt"
	KindFileWritten  = "fileWritten"
	KindTurnEnd      = "turnEnd"
	KindSessionEnd   = "sessionEnd"
)

// MaxText caps Prompt and LastAssistantMessage, in characters.
const MaxText = 8000

// Event is one captured lifecycle event. One event is one queue file.
type Event struct {
	ID                   string `json:"id"`
	At                   string `json:"at"`
	Tool                 string `json:"tool"`
	SessionRef           string `json:"sessionRef"`
	Cwd                  string `json:"cwd"`
	Kind                 string `json:"kind"`
	Model                string `json:"model,omitempty"`
	Prompt               string `json:"prompt,omitempty"`
	Path                 string `json:"path,omitempty"`
	LastAssistantMessage string `json:"lastAssistantMessage,omitempty"`
	TranscriptPath       string `json:"transcriptPath,omitempty"`
	Reason               string `json:"reason,omitempty"`
	Source               string `json:"source,omitempty"`
}

// truncate cuts s to at most n characters.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// trimWords cuts s to at most n characters, on a word boundary when one
// exists in the kept part.
func trimWords(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if next := r[n]; next != ' ' {
		for i := len(cut) - 1; i > 0; i-- {
			if cut[i] == ' ' {
				cut = cut[:i]
				break
			}
		}
	}
	for len(cut) > 0 && cut[len(cut)-1] == ' ' {
		cut = cut[:len(cut)-1]
	}
	return cut
}
