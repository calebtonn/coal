// Package claudecode imports Claude Code session transcripts as proposed
// Intent Manifests (ADR-012). Transcripts are undocumented and change, so
// the reader is versioned and skips whatever it cannot parse.
package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Importer names the transcript format this reader understands.
const Importer = "claude-code-transcript@1"

// Session is what one transcript file contributes.
type Session struct {
	ID            string    `json:"id"`
	File          string    `json:"file"`
	Cwd           string    `json:"cwd"`
	Model         string    `json:"model,omitempty"`
	FirstAt       time.Time `json:"firstAt"`
	LastAt        time.Time `json:"lastAt"`
	Prompts       []string  `json:"-"`
	LastAssistant string    `json:"-"`
	Files         []string  `json:"-"` // absolute paths written, first write first

	idSet bool
}

// Scan is the result of reading a projects directory.
type Scan struct {
	Sessions     []*Session
	Files        int // transcript files read
	SkippedFiles int // files that could not be opened or held no session
	SkippedLines int // lines that did not parse
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
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
}

var writeTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true}

// Read scans <dir>/<encoded-cwd>/<session-id>.jsonl. Symlinks are never
// followed, so nothing outside dir is read, and deeper files (such as
// subagent transcripts) are ignored.
func Read(dir string) (*Scan, error) {
	projects, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sc := &Scan{}
	for _, p := range projects {
		if !p.IsDir() || p.Type()&os.ModeSymlink != 0 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, p.Name()))
		if err != nil {
			sc.SkippedFiles++
			continue
		}
		for _, f := range files {
			if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sc.Files++
			s, bad, err := readFile(filepath.Join(dir, p.Name(), f.Name()))
			sc.SkippedLines += bad
			if err != nil || s == nil {
				sc.SkippedFiles++
				continue
			}
			sc.Sessions = append(sc.Sessions, s)
		}
	}
	return sc, nil
}

// readFile reads one transcript. It returns nil when the file holds no
// usable session (no cwd or no timestamps).
func readFile(path string) (*Session, int, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	s := &Session{File: path, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	seen := map[string]bool{}
	var lastMsg string
	bad := 0
	r := bufio.NewReader(f)
	for {
		raw, err := r.ReadBytes('\n')
		if raw = bytes.TrimSpace(raw); len(raw) > 0 && !s.add(raw, seen, &lastMsg) {
			bad++
		}
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, bad, err
		}
	}
	if s.Cwd == "" || s.LastAt.IsZero() {
		return nil, bad, nil
	}
	return s, bad, nil
}

// add folds one line into the session and reports whether it parsed.
func (s *Session) add(raw []byte, seen map[string]bool, lastMsg *string) bool {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return false
	}
	if l.SessionID != "" && !s.idSet {
		s.ID, s.idSet = l.SessionID, true
	}
	if s.Cwd == "" && filepath.IsAbs(l.Cwd) {
		s.Cwd = filepath.Clean(l.Cwd)
	}
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		if s.FirstAt.IsZero() || t.Before(s.FirstAt) {
			s.FirstAt = t
		}
		if t.After(s.LastAt) {
			s.LastAt = t
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
		case l.Type == "assistant" && b.Type == "tool_use" && writeTools[b.Name] && b.Input.FilePath != "":
			p := b.Input.FilePath
			if !filepath.IsAbs(p) && s.Cwd != "" {
				p = filepath.Join(s.Cwd, p)
			}
			if p = filepath.Clean(p); filepath.IsAbs(p) && !seen[p] {
				seen[p] = true
				s.Files = append(s.Files, p)
			}
		}
	}
	text := strings.Join(texts, "\n\n")
	switch {
	case l.Type == "user" && text != "" && !strings.HasPrefix(strings.TrimSpace(text), "<"):
		// Lines starting with a tag are injected by Claude Code
		// (command output, reminders), not typed by the user.
		s.Prompts = append(s.Prompts, text)
	case l.Type == "assistant":
		if m.Model != "" {
			s.Model = m.Model
		}
		if text == "" {
			break
		}
		if m.ID != "" && m.ID == *lastMsg {
			s.LastAssistant += "\n\n" + text
		} else {
			s.LastAssistant = text
		}
		*lastMsg = m.ID
	}
	return true
}
