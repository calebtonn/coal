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
	"strings"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
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
	StartedAt            string `json:"startedAt"`
	EndedAt              string `json:"endedAt,omitempty"`
	RecordedByModel      bool   `json:"recordedByModel"`
	// Placeholder is set when this session created the manifest with a
	// summary from the file name, so a later passing prompt replaces it.
	Placeholder bool `json:"placeholder,omitempty"`
	// StateNextStep and StateSummary are the manifest state when the
	// session attached or last wrote it. A difference at sessionEnd
	// means the model recorded state itself.
	StateNextStep string `json:"stateNextStep,omitempty"`
	StateSummary  string `json:"stateSummary,omitempty"`
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
	switch e.Kind {
	case capture.KindSessionStart:
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

func (s *Session) attach(m *manifest.Manifest) {
	s.ManifestID = m.ID
	s.StateNextStep, s.StateSummary = m.State.NextStep, m.State.Summary
}

// match picks the manifest for cwd among all manifests in the store.
func match(a *app.App, cwd string) *manifest.Manifest {
	ms, _ := a.Store.List()
	return marker.Match(cwd, ms)
}

// prompt keeps the first prompt that passes the summary filter (#11),
// and upgrades a placeholder summary this session wrote to it.
func prompt(a *app.App, s *Session, text string) error {
	if s.FirstPrompt != "" {
		return nil
	}
	sum, ok := capture.PickSummary(text)
	if !ok {
		return nil
	}
	s.FirstPrompt = sum
	if !s.Placeholder || s.ManifestID == "" {
		return nil
	}
	s.Placeholder = false
	_, err := a.Update(s.ManifestID, 0, func(m *manifest.Manifest) error {
		if m.Intent.Source != manifest.SourceModel {
			return store.ErrNoChange
		}
		m.Intent.Summary, m.Intent.Source = sum, manifest.SourceUser
		return nil
	}, op("summary from the first prompt"))
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
	if skipFile(a, path) {
		return nil
	}
	if s.ManifestID != "" {
		if _, err := a.Get(s.ManifestID); errors.Is(err, store.ErrNotFound) {
			s.ManifestID = ""
		}
	}
	created := false
	if s.ManifestID == "" {
		if m := match(a, s.Cwd); m != nil {
			s.attach(m)
		} else if err := create(a, s); err != nil {
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

// create makes a manifest for the session's project with contexts and
// a .coal marker (ADR-013, #4).
func create(a *app.App, s *Session) error {
	summary, source := s.FirstPrompt, manifest.SourceUser
	root := marker.ProjectRoot(s.Cwd)
	if summary == "" {
		summary, source = "Work in "+filepath.Base(root), manifest.SourceModel
		s.Placeholder = true
	}
	m := manifest.New(summary, source, manifest.Provenance{Tool: s.Tool, Model: s.Model, SessionRef: s.SessionRef, CapturedBy: CapturedBy})
	m.Contexts = marker.Contexts(s.Cwd)
	if err := a.Create(m, op("create from session")); err != nil {
		return err
	}
	s.attach(m)
	metrics.Record(a.Store.Dir(), metrics.ManifestCreated, map[string]any{"manifest": m.ID, "session": s.SessionRef})
	if err := marker.Write(root, m.ID); err != nil {
		a.Store.Log("capture: marker for %s in %s: %v", m.ID, root, err)
	}
	return nil
}

// closeOut applies the sessionEnd fallback (#2): unless the model
// recorded state during the session, take state.summary and nextStep
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
	if m.State.NextStep != s.StateNextStep || m.State.Summary != s.StateSummary {
		s.RecordedByModel = true
	}
	sum, next := capture.ExtractState(s.LastAssistantMessage)
	if s.RecordedByModel || sum == "" && next == "" {
		s.attach(m)
		return nil
	}
	m, err = a.Update(s.ManifestID, 0, func(m *manifest.Manifest) error {
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
	if err != nil {
		return err
	}
	s.attach(m)
	return nil
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
