// Package fold applies captured events to manifests. It runs only
// inside a queue drain, which holds queue/.lock, so it is the single
// writer for capture (ADR-011, #1).
package fold

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
	"github.com/calebtonn/coal/internal/summarize"
)

// CapturedBy names this adapter on artifact versions.
const CapturedBy = "coal-adapter-claude-code@0.1.0-poc"

// MaxFile is the largest file that is registered as an artifact.
const MaxFile = 5 << 20

// StaleAfter is how long a session can go without an end before the
// next sessionStart closes it out.
const StaleAfter = 12 * time.Hour

// Session is the writer's scratch for one session, in sessions/.
type Session struct {
	SessionRef           string `json:"sessionRef"`
	Tool                 string `json:"tool"`
	Cwd                  string `json:"cwd"`
	Model                string `json:"model,omitempty"`
	FirstPrompt          string `json:"firstPrompt,omitempty"`
	ManifestID           string `json:"manifestID,omitempty"`
	LastAssistantMessage string `json:"lastAssistantMessage,omitempty"`
	TranscriptPath       string `json:"transcriptPath,omitempty"`
	ProjectsRoot         string `json:"projectsRoot,omitempty"`
	StartedAt            string `json:"startedAt"`
	EndedAt              string `json:"endedAt,omitempty"`
	// RecordedByModel is set at sessionEnd when the manifest's state
	// stamp shows coal_update_state or a PATCH wrote state during the
	// session, so the extraction fallback was skipped.
	RecordedByModel bool `json:"recordedByModel"`
	// Unanchored is set once the session is known to run in the home
	// directory, above it, or at the root, where nothing is captured.
	Unanchored bool `json:"unanchored,omitempty"`
	// Wrote is set after the session's first captured file.
	Wrote bool `json:"wrote,omitempty"`
	// ResumedFrom is the manifest matched at sessionStart, whose resume
	// line the hook showed; ResumeActed records that the session then
	// wrote to it (#12).
	ResumedFrom string `json:"resumedFrom,omitempty"`
	ResumeActed bool   `json:"resumeActed,omitempty"`
}

func op(action string) store.Op { return store.Op{Tool: "coal-adapter-claude-code", Action: action} }

// Apply folds one event into the store.
func Apply(a *app.App, e *capture.Event) error {
	var s Session
	err := a.Store.ReadSession(e.SessionRef, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.SessionRef == "" {
		s = Session{SessionRef: e.SessionRef, Tool: e.Tool, Cwd: e.Cwd, StartedAt: e.At}
	}
	if e.Model != "" {
		s.Model = e.Model
	}
	if s.Cwd == "" {
		s.Cwd = e.Cwd
	}
	if e.TranscriptPath != "" {
		s.TranscriptPath = e.TranscriptPath
	}
	if e.ProjectsRoot != "" {
		s.ProjectsRoot = e.ProjectsRoot
	}
	switch e.Kind {
	case capture.KindSessionStart:
		// A resumed or forked session starts a new stretch of work, so a
		// state stamp written by another session before now must not
		// suppress this one's extraction at its end.
		if e.Source == "resume" || e.Source == "fork" {
			s.StartedAt, s.EndedAt, s.RecordedByModel = e.At, "", false
		}
		if s.ManifestID == "" {
			if m := match(a, s.Cwd); m != nil {
				s.attach(m)
				s.ResumedFrom = m.ID
			}
		}
		if err := a.Store.WriteSession(s.SessionRef, &s); err != nil {
			return err
		}
		return closeStale(a, s.SessionRef, time.Now())
	case capture.KindPrompt:
		if err := prompt(a, &s, e.Prompt); err != nil {
			return err
		}
	case capture.KindFileWritten:
		if err := fileWritten(a, &s, e); err != nil {
			return err
		}
	case capture.KindTurnEnd:
		s.LastAssistantMessage = e.LastAssistantMessage
		if s.ManifestID != "" {
			if _, err := a.Update(s.ManifestID, 0, func(m *manifest.Manifest) error {
				m.State.LastActivityAt = e.At
				return nil
			}, op("turn end")); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
	case capture.KindSessionEnd:
		s.EndedAt = e.At
		if err := closeOut(a, &s); err != nil {
			return err
		}
	default:
		return nil
	}
	return a.Store.WriteSession(s.SessionRef, &s)
}

func (s *Session) attach(m *manifest.Manifest) { s.ManifestID = m.ID }

// match picks the manifest for cwd among all manifests in the store.
func match(a *app.App, cwd string) *manifest.Manifest {
	ms, _ := a.Store.List()
	return marker.Match(cwd, ms)
}

// prompt keeps the first prompt that passes the summary filter (#11).
func prompt(a *app.App, s *Session, text string) error {
	if s.FirstPrompt != "" {
		return nil
	}
	sum, ok := capture.PickSummary(text)
	if !ok {
		return nil
	}
	s.FirstPrompt = sum
	return upgradePlaceholder(a, s)
}

// isPlaceholder reports whether m still carries the "Work in <project>"
// summary capture writes when no prompt passed the filter.
func isPlaceholder(m *manifest.Manifest) bool {
	if m.Intent.Source != manifest.SourceModel {
		return false
	}
	for _, c := range m.Contexts {
		if c.Kind == marker.KindPath && m.Intent.Summary == "Work in "+filepath.Base(c.Key) {
			return true
		}
	}
	return false
}

// upgradePlaceholder replaces a placeholder summary on the session's
// manifest with the session's first passing prompt, in the user's
// words. Any session may do it, not only the one that created it.
func upgradePlaceholder(a *app.App, s *Session) error {
	if s.FirstPrompt == "" || s.ManifestID == "" {
		return nil
	}
	_, err := a.Update(s.ManifestID, 0, func(m *manifest.Manifest) error {
		if !isPlaceholder(m) {
			return store.ErrNoChange
		}
		m.Intent.Summary, m.Intent.Source = s.FirstPrompt, manifest.SourceUser
		return nil
	}, op("summary from a prompt"))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

// skipFile reports whether path should not be captured at all: inside
// the data dir or a .git dir, not a regular file, or over MaxFile.
func skipFile(a *app.App, path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true
	}
	data := a.Store.Dir()
	if r, err := filepath.EvalSymlinks(data); err == nil {
		data = r
	}
	if real == data || strings.HasPrefix(real, data+"/") {
		return true
	}
	for _, part := range strings.Split(real, "/") {
		if part == ".git" {
			return true
		}
	}
	fi, err := os.Stat(real)
	return err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxFile
}

func fileWritten(a *app.App, s *Session, e *capture.Event) error {
	path := e.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Cwd, path)
	}
	cfg := LoadConfig(a.Store.Dir())
	if !cfg.Captures(path) || skipFile(a, path) {
		return nil
	}
	// Only files inside the session's project are captured (C2), and
	// nothing at all when the project would be ~, above it, or / (C1).
	root := marker.ProjectRoot(s.Cwd)
	if marker.Unanchored(root) {
		if !s.Unanchored {
			s.Unanchored = true
			a.Store.Log("capture: unanchored session in %s, not captured", root)
		}
		return nil
	}
	real, _ := filepath.EvalSymlinks(path)
	if real != root && !strings.HasPrefix(real, root+"/") {
		a.Store.Log("capture: %s is outside the project %s, not captured", real, root)
		return nil
	}
	path = real
	if s.ManifestID != "" {
		if _, err := a.Get(s.ManifestID); errors.Is(err, store.ErrNotFound) {
			s.ManifestID = ""
		}
	}
	created := false
	if s.ManifestID == "" {
		if m := match(a, s.Cwd); m != nil {
			s.attach(m)
			if err := upgradePlaceholder(a, s); err != nil {
				return err
			}
		} else if err := create(a, s, cfg); err != nil {
			return err
		} else {
			created = true
		}
	}
	fields := map[string]any{"manifest": s.ManifestID, "session": s.SessionRef}
	if !s.Wrote && !created {
		metrics.Record(a.Store.Dir(), metrics.SessionAttached, fields)
	}
	s.Wrote = true
	if s.ResumedFrom != "" && s.ResumedFrom == s.ManifestID && !s.ResumeActed {
		s.ResumeActed = true
		metrics.Record(a.Store.Dir(), metrics.ResumeActed, fields)
	}
	prov := manifest.Provenance{Tool: s.Tool, Model: s.Model, SessionRef: s.SessionRef, CapturedBy: CapturedBy}
	_, err := a.RegisterArtifact(s.ManifestID, store.ArtifactInput{Path: path, Producer: prov}, op("capture "+filepath.Base(path)))
	return err
}

// create makes a manifest for the session's project with contexts and,
// unless config.json sets markers to none, a .coal marker (ADR-013, #4).
func create(a *app.App, s *Session, cfg Config) error {
	summary, source := s.FirstPrompt, manifest.SourceUser
	root := marker.ProjectRoot(s.Cwd)
	if summary == "" {
		summary, source = "Work in "+filepath.Base(root), manifest.SourceModel
	}
	m := manifest.New(summary, source, manifest.Provenance{Tool: s.Tool, Model: s.Model, SessionRef: s.SessionRef, CapturedBy: CapturedBy})
	m.Contexts = marker.Contexts(s.Cwd)
	if err := a.Create(m, op("create from session")); err != nil {
		return err
	}
	s.attach(m)
	metrics.Record(a.Store.Dir(), metrics.ManifestCreated, map[string]any{"manifest": m.ID, "session": s.SessionRef})
	if cfg.Markers == MarkersNone {
		return nil
	}
	if err := marker.Write(root, m.ID); err != nil {
		a.Store.Log("capture: marker for %s in %s: %v", m.ID, root, err)
	}
	return nil
}

// closeOut applies the sessionEnd fallback (#2): unless the manifest's
// state stamp shows a deliberate state write (coal_update_state or a
// PATCH) since the session started, take state.summary and nextStep
// from the last assistant message. intent.summary is never touched.
func closeOut(a *app.App, s *Session) error {
	if s.ManifestID == "" {
		return nil
	}
	m, err := a.Get(s.ManifestID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	// A stamp from coal_update_state or a PATCH since the session began
	// means the model recorded state itself. The summarizer's own stamps
	// do not count, or a late run for one session would hide the next.
	if st, ok := manifest.StateStampOf(m); ok && st.At >= s.StartedAt && st.RecordedBy != summarize.Tool {
		s.RecordedByModel = true
		return nil
	}
	// The summarizer already ran after this session ended: applying the
	// sessionEnd again must not undo its result or pay for another call.
	// Strictly after, because stamps have one-second precision and a
	// summarizer run for an earlier session can land in the second this
	// one ends; a run for this session takes longer than that.
	if st, ok := manifest.StateStampOf(m); ok && st.RecordedBy == summarize.Tool && s.EndedAt != "" && st.At > s.EndedAt {
		return nil
	}
	sum, next, worked := extract(a, s)
	// A session that wrote nothing and got no reply, such as "hello"
	// then exit, has nothing to say about where the work stands.
	if !worked {
		return nil
	}
	if sum != "" || next != "" {
		if err := applyState(a, s, sum, next); err != nil {
			return err
		}
	}
	queueSummarizer(a, s)
	return nil
}

// queueSummarizer asks the daemon's opt-in summarizer to improve on the
// extraction, when config.summarizer is set. Capture runs nothing
// (ADR-016). Errors are logged; the extraction already landed.
func queueSummarizer(a *app.App, s *Session) {
	cfg, err := summarize.LoadConfig(a.Store.Dir())
	if err != nil {
		a.Store.Log("capture: summarizer config: %v", err)
	}
	if cfg == nil {
		return
	}
	if !allowedTranscript(s) {
		a.Store.Log("capture: no summarize job for %s: transcript is not a Claude Code session file", s.SessionRef)
		return
	}
	j := summarize.Job{ManifestID: s.ManifestID, SessionRef: s.SessionRef, TranscriptPath: s.TranscriptPath, StartedAt: s.StartedAt, EndedAt: s.EndedAt, ProjectsRoot: s.ProjectsRoot}
	if err := queueJob(a.Store.Dir(), j); err != nil {
		a.Store.Log("capture: summarize job for %s: %v", s.SessionRef, err)
	}
}

// applyState writes the extracted state, never intent.summary.
func applyState(a *app.App, s *Session, sum, next string) error {
	_, err := a.Update(s.ManifestID, 0, func(m *manifest.Manifest) error {
		if sum != "" {
			m.State.Summary = sum
		}
		if next != "" {
			m.State.NextStep = next
		}
		if s.EndedAt > m.State.LastActivityAt {
			m.State.LastActivityAt = s.EndedAt
		}
		return nil
	}, op("session end"))
	return err
}

// closeStale closes out sessions other than current that never ended
// and have had no event for StaleAfter, so sessions killed without a
// SessionEnd still get a state summary (#1).
func closeStale(a *app.App, current string, now time.Time) error {
	files, _ := filepath.Glob(filepath.Join(a.Store.Dir(), "sessions", "*.json"))
	cutoff := now.Add(-StaleAfter)
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		var s Session
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &s) != nil || s.SessionRef == "" || s.SessionRef == current || s.EndedAt != "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s.StartedAt); err == nil && t.After(cutoff) {
			continue
		}
		s.EndedAt = now.UTC().Format(time.RFC3339)
		if err := closeOut(a, &s); err != nil {
			a.Store.Log("capture: closing stale session %s: %v", s.SessionRef, err)
		}
		if err := a.Store.WriteSession(s.SessionRef, &s); err != nil {
			return err
		}
	}
	return nil
}

// MaxTranscript is how much of the end of a transcript extraction reads.
const MaxTranscript = 2 << 20

// MaxFilesTouched caps the file names appended to state.summary.
const MaxFilesTouched = 5

// extract is the deterministic sessionEnd fallback (#2, ADR-016). When
// the session's transcript exists and summarize.AllowedTranscript
// accepts it, or there is no last assistant
// message, it reads the transcript: the summary is the last assistant
// message's first paragraph, else the last user prompt, followed by
// "Files touched: a, b" with at most five base names. Otherwise, or if
// the transcript cannot be read, it uses the last assistant message
// that Stop delivered.
func extract(a *app.App, s *Session) (summary, nextStep string, worked bool) {
	summary, nextStep = capture.ExtractState(s.LastAssistantMessage)
	worked = s.Wrote || s.LastAssistantMessage != ""
	// Only Claude Code's own session files are read, so a payload cannot
	// point extraction at any other file.
	if s.TranscriptPath == "" || !allowedTranscript(s) {
		return summary, nextStep, worked
	}
	if _, err := os.Lstat(s.TranscriptPath); err != nil && s.LastAssistantMessage != "" {
		return summary, nextStep, worked
	}
	t, err := summarize.ReadFile(s.TranscriptPath, MaxTranscript)
	if err != nil {
		a.Store.Log("capture: transcript for %s: %v", s.SessionRef, err)
		return summary, nextStep, worked
	}
	worked = worked || t.LastAssistant != "" || len(t.Files) > 0
	// As in summarize.FromTranscript: the prompt fallback must pass the
	// summary filter (#11). This reads the transcript itself because it
	// also needs to know whether there was an assistant reply.
	tsum, tnext := capture.ExtractState(t.LastAssistant)
	if _, ok := capture.PickSummary(t.LastPrompt()); tsum == "" && ok {
		tsum = summarize.Cut(t.LastPrompt(), capture.MaxStateSummary)
	}
	if tsum != "" {
		summary = tsum
	}
	if tnext != "" {
		nextStep = tnext
	}
	if names := baseNames(t.Files, MaxFilesTouched); len(names) > 0 {
		summary = strings.TrimSpace(summary + " Files touched: " + strings.Join(names, ", "))
	}
	return summary, nextStep, worked
}

// allowedTranscript reports whether the session's transcript is a Claude
// Code session file that capture may read, under this process's
// projects dir or the one the hook recorded.
func allowedTranscript(s *Session) bool {
	return summarize.AllowedTranscript(s.TranscriptPath, s.ProjectsRoot)
}

// baseNames returns up to n distinct base names from paths, in order.
func baseNames(paths []string, n int) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if b := filepath.Base(p); b != "." && b != "/" && !seen[b] && len(out) < n {
			seen[b] = true
			out = append(out, b)
		}
	}
	return out
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// queueJob writes j to queue/summarize/<sessionRef>.json through a temp
// file and a rename, so the runner never reads half a job. A later job
// for the same session replaces the earlier one.
func queueJob(home string, j summarize.Job) error {
	dir := summarize.Dir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".job-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, unsafeName.ReplaceAllString(j.SessionRef, "_")+".json"))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
