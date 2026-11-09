// Package summarize reads Claude Code transcripts for a session's state:
// the deterministic extraction (FromTranscript) and the opt-in model
// summarizer (Run) that ADR-016 and F5 describe. It is the one
// transcript parser; the importer uses it too.
package summarize

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/calebtonn/coal/internal/capture"
)

// Transcript is what one transcript file holds, as far as COAL cares.
type Transcript struct {
	SessionID     string
	Cwd           string
	Model         string
	FirstAt       time.Time
	LastAt        time.Time
	Prompts       []string // real user prompts, oldest first
	LastAssistant string   // text of the last assistant message
	Files         []string // absolute paths written, first write first
	Writes        []Write  // every write that did not fail, in order
	BadLines      int      // lines that did not parse

	seen    map[string]bool
	lastMsg string
	failed  map[string]bool // tool_use ids whose tool_result is an error
	ids     []string        // the tool_use id of each entry in Writes
}

// Write is one write tool call in a transcript: the absolute path and
// the time of its line, zero when the line had none. A resumed session's
// transcript still holds the writes from before the resume, so a caller
// that cares about one stretch filters by At. A call whose tool_result
// has is_error set, because the user rejected it at the permission
// prompt or the edit failed, wrote nothing and is left out of Writes;
// Files still lists it, as a file the session tried to write.
type Write struct {
	Path string
	At   time.Time
}

type line struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Cwd         string          `json:"cwd"`
	Timestamp   string          `json:"timestamp"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
	Input     struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"input"`
}

var writeTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

// ReadFile parses the transcript at path. It refuses a symlink. When
// maxBytes is positive and the file is larger, only the last maxBytes are
// read, starting after the first partial line, so a huge transcript stays
// fast.
func ReadFile(path string, maxBytes int64) (*Transcript, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("transcript is not a regular file")
	}
	r := bufio.NewReader(f)
	if maxBytes > 0 && fi.Size() > maxBytes {
		if _, err := f.Seek(fi.Size()-maxBytes, io.SeekStart); err != nil {
			return nil, err
		}
		r.Reset(f)
		if _, err := r.ReadBytes('\n'); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	return parse(r)
}

// Parse reads transcript lines from r.
func Parse(r io.Reader) (*Transcript, error) { return parse(bufio.NewReader(r)) }

func parse(r *bufio.Reader) (*Transcript, error) {
	t := &Transcript{seen: map[string]bool{}, failed: map[string]bool{}}
	for {
		raw, err := r.ReadBytes('\n')
		if raw = bytes.TrimSpace(raw); len(raw) > 0 && !t.add(raw) {
			t.BadLines++
		}
		if errors.Is(err, io.EOF) {
			t.dropFailed()
			return t, nil
		} else if err != nil {
			t.dropFailed()
			return t, err
		}
	}
}

// dropFailed removes the writes whose tool_result was an error. A write
// with no result in the transcript, such as one cut off by the end of
// the session, is kept.
func (t *Transcript) dropFailed() {
	if len(t.failed) == 0 {
		return
	}
	kept := t.Writes[:0]
	for i, w := range t.Writes {
		if id := t.ids[i]; id == "" || !t.failed[id] {
			kept = append(kept, w)
		}
	}
	t.Writes, t.ids = kept, nil
}

// add folds one line in and reports whether it parsed.
func (t *Transcript) add(raw []byte) bool {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return false
	}
	if t.SessionID == "" {
		t.SessionID = l.SessionID
	}
	if t.Cwd == "" && filepath.IsAbs(l.Cwd) {
		t.Cwd = filepath.Clean(l.Cwd)
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		ts = time.Time{}
	}
	if !ts.IsZero() {
		if t.FirstAt.IsZero() || ts.Before(t.FirstAt) {
			t.FirstAt = ts
		}
		if ts.After(t.LastAt) {
			t.LastAt = ts
		}
	}
	if l.IsMeta || l.IsSidechain || len(l.Message) == 0 || (l.Type != "user" && l.Type != "assistant") {
		return true
	}
	var m message
	if err := json.Unmarshal(l.Message, &m); err != nil {
		return false
	}
	var blocks []block
	var str string
	if json.Unmarshal(m.Content, &str) == nil {
		blocks = []block{{Type: "text", Text: str}}
	} else if json.Unmarshal(m.Content, &blocks) != nil {
		return false
	}
	var texts []string
	for _, b := range blocks {
		switch {
		case b.Type == "text" && strings.TrimSpace(b.Text) != "":
			texts = append(texts, b.Text)
		case l.Type == "assistant" && b.Type == "tool_use" && writeTools[b.Name]:
			p := b.Input.FilePath
			if p == "" {
				p = b.Input.NotebookPath
			}
			t.wrote(p, ts, b.ID)
		case l.Type == "user" && b.Type == "tool_result" && b.IsError && b.ToolUseID != "":
			t.failed[b.ToolUseID] = true
		}
	}
	text := strings.Join(texts, "\n\n")
	switch {
	case l.Type == "user" && text != "" && !capture.IsWrapper(text):
		// Known wrapper tags are injected by Claude Code (command
		// output, reminders), not typed by the user.
		t.Prompts = append(t.Prompts, text)
	case l.Type == "assistant":
		if m.Model != "" {
			t.Model = m.Model
		}
		if text == "" {
			break
		}
		// Claude Code writes one line per content block; blocks of one
		// message share its id.
		if m.ID != "" && m.ID == t.lastMsg {
			t.LastAssistant += "\n\n" + text
		} else {
			t.LastAssistant = text
		}
		t.lastMsg = m.ID
	}
	return true
}

func (t *Transcript) wrote(p string, at time.Time, id string) {
	if p == "" {
		return
	}
	if !filepath.IsAbs(p) && t.Cwd != "" {
		p = filepath.Join(t.Cwd, p)
	}
	if p = filepath.Clean(p); !filepath.IsAbs(p) {
		return
	}
	t.Writes = append(t.Writes, Write{Path: p, At: at})
	t.ids = append(t.ids, id)
	if !t.seen[p] {
		t.seen[p] = true
		t.Files = append(t.Files, p)
	}
}

// LastPrompt is the last real user prompt, or "".
func (t *Transcript) LastPrompt() string {
	if len(t.Prompts) == 0 {
		return ""
	}
	return t.Prompts[len(t.Prompts)-1]
}

// FromTranscript is the deterministic extraction (ADR-016): the summary
// is the first paragraph of the last assistant message, else the last
// real user prompt cut to 300 runes when it passes capture.PickSummary,
// else ""; nextStep comes from a "Next step" line in that message; files are the paths written, deduplicated, in
// order. At most maxBytes are read from the end of the file.
func FromTranscript(path string, maxBytes int64) (summary, nextStep string, files []string, err error) {
	t, err := ReadFile(path, maxBytes)
	if err != nil {
		return "", "", nil, err
	}
	summary, nextStep = capture.ExtractState(t.LastAssistant)
	// The prompt fallback must pass the summary filter (#11), so a
	// trivial later session ("hello", "/exit") never replaces a good
	// summary.
	if summary == "" {
		if _, ok := capture.PickSummary(t.LastPrompt()); ok {
			summary = Cut(t.LastPrompt(), capture.MaxStateSummary)
		}
	}
	return summary, nextStep, t.Files, nil
}

// Cut collapses whitespace and cuts s to at most n runes, on a word
// boundary when there is one in the second half.
func Cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	r = r[:n]
	for i := len(r) - 1; i > n/2; i-- {
		if unicode.IsSpace(r[i]) {
			return strings.TrimSpace(string(r[:i]))
		}
	}
	return string(r)
}
