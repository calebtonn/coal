// Package claudecode imports Claude Code session transcripts as proposed
// Intent Manifests (ADR-012). Transcripts are undocumented and change, so
// the reader is versioned and skips whatever it cannot parse. Lines are
// parsed by internal/summarize, the one transcript parser.
package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calebtonn/coal/internal/summarize"
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
}

// Scan is the result of reading a projects directory.
type Scan struct {
	Sessions     []*Session
	Files        int // transcript files read
	SkippedFiles int // files that could not be opened or held no session
	SkippedLines int // lines that did not parse
}

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

// readFile reads one transcript through the shared parser. It returns
// nil when the file holds no usable session (no cwd or no timestamps).
func readFile(path string) (*Session, int, error) {
	t, err := summarize.ReadFile(path, 0)
	if err != nil {
		return nil, 0, err
	}
	if t.Cwd == "" || t.LastAt.IsZero() {
		return nil, t.BadLines, nil
	}
	id := t.SessionID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	return &Session{ID: id, File: path, Cwd: t.Cwd, Model: t.Model, FirstAt: t.FirstAt, LastAt: t.LastAt,
		Prompts: t.Prompts, LastAssistant: t.LastAssistant, Files: t.Files}, t.BadLines, nil
}
