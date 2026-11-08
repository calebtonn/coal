// Package metrics keeps local counters in logs/metrics.jsonl (#12): one
// JSON object per line, {"at": ts, "event": name, ...fields}. The file
// never leaves the machine and recording never fails the caller.
package metrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/calebtonn/coal/internal/manifest"
)

// Event names in use. coal_find carries a "tool" field.
const (
	Find            = "coal_find"
	ResumeShown     = "resume_shown"
	ResumeActed     = "resume_acted"
	SessionAttached = "session_attached"
	ManifestCreated = "manifest_created"
	Rename          = "rename"
	Purge           = "purge"
)

// Events lists the known events in the order stats prints them.
var Events = []string{Find, ResumeShown, ResumeActed, SessionAttached, ManifestCreated, Rename, Purge}

// Path returns the metrics file under the data directory home.
func Path(home string) string { return filepath.Join(home, "logs", "metrics.jsonl") }

// Record appends one line for event. fields cannot override "at" or
// "event". Errors are swallowed: counters must never break real work.
func Record(home, event string, fields map[string]any) {
	line := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		line[k] = v
	}
	line["at"], line["event"] = manifest.Now(), event
	b, err := json.Marshal(line)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(Path(home)), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(Path(home), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	f.Write(append(b, '\n')) // one write per line, so concurrent appends do not interleave
	f.Close()
}

// Summary counts events at or after Since.
type Summary struct {
	Since     time.Time
	Counts    map[string]int // by event
	FindTools map[string]int // coal_find by tool
	Bad       int            // lines that did not parse
}

// Summarize reads the metrics file. A missing file is an empty summary.
func Summarize(home string, since time.Time) (Summary, error) {
	s := Summary{Since: since, Counts: map[string]int{}, FindTools: map[string]int{}}
	f, err := os.Open(Path(home))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return s, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var line struct {
			At    string `json:"at"`
			Event string `json:"event"`
			Tool  string `json:"tool"`
		}
		if len(sc.Bytes()) == 0 {
			continue
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Event == "" {
			s.Bad++
			continue
		}
		at, err := time.Parse(time.RFC3339, line.At)
		if err != nil {
			s.Bad++
			continue
		}
		if at.Before(since) {
			continue
		}
		s.Counts[line.Event]++
		if line.Event == Find {
			tool := line.Tool
			if tool == "" {
				tool = "unknown"
			}
			s.FindTools[tool]++
		}
	}
	return s, sc.Err()
}

// Ratio is a count over a total; Total 0 means there is nothing to compare.
type Ratio struct{ N, Total int }

// ResumeActed is resume lines acted on over resume lines shown.
func (s Summary) ResumeActed() Ratio { return Ratio{s.Counts[ResumeActed], s.Counts[ResumeShown]} }

// Attached is sessions attached over sessions attached plus manifests created.
func (s Summary) Attached() Ratio {
	a := s.Counts[SessionAttached]
	return Ratio{a, a + s.Counts[ManifestCreated]}
}

// Others returns events outside Events that appeared, sorted.
func (s Summary) Others() []string {
	known := map[string]bool{}
	for _, e := range Events {
		known[e] = true
	}
	var out []string
	for e := range s.Counts {
		if !known[e] {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}
