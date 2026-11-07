package fold

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Marker settings (ADR-013).
const (
	MarkersProject = "project"
	MarkersNone    = "none"
)

// DefaultExtensions are the file types PostToolUse captures (F5).
var DefaultExtensions = []string{".html", ".md", ".py", ".ts", ".tsx"}

// Config is the part of config.json that capture reads.
type Config struct {
	// Markers is "project" (write .coal) or "none" (never write one).
	Markers string
	// CaptureExtensions lists lowercase extensions with a leading dot.
	CaptureExtensions []string
	// Summarizer is set when config.summarizer holds anything but null
	// or false; the daemon runs it, capture only queues jobs.
	Summarizer bool
}

// LoadConfig reads capture settings from home/config.json. A missing or
// unreadable file, or a missing key, gives the defaults.
func LoadConfig(home string) Config {
	var raw struct {
		Markers           string          `json:"markers"`
		CaptureExtensions []string        `json:"captureExtensions"`
		Summarizer        json.RawMessage `json:"summarizer"`
	}
	if b, err := os.ReadFile(filepath.Join(home, "config.json")); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	c := Config{Markers: MarkersProject, CaptureExtensions: DefaultExtensions}
	if strings.EqualFold(raw.Markers, MarkersNone) {
		c.Markers = MarkersNone
	}
	if raw.CaptureExtensions != nil {
		c.CaptureExtensions = nil
		for _, e := range raw.CaptureExtensions {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				if !strings.HasPrefix(e, ".") {
					e = "." + e
				}
				c.CaptureExtensions = append(c.CaptureExtensions, e)
			}
		}
	}
	s := bytes.TrimSpace(raw.Summarizer)
	c.Summarizer = len(s) > 0 && !bytes.Equal(s, []byte("null")) && !bytes.Equal(s, []byte("false"))
	return c
}

// Captures reports whether a file with this path's extension is captured.
func (c Config) Captures(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range c.CaptureExtensions {
		if e == ext {
			return true
		}
	}
	return false
}
