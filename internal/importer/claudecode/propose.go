package claudecode

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/store"
)

// CapturedBy names this importer on manifests and artifact versions.
const CapturedBy = "coal-import-claude-code@0.1.0-poc"

// ExtKey is the ext entry recording where an imported manifest came from.
const ExtKey = "io.coalmine.import"

// Proposal is one manifest the import would create: one per project.
type Proposal struct {
	Root            string     `json:"root"`
	Exists          bool       `json:"exists"`
	Sessions        []*Session `json:"sessions"` // oldest first
	Summary         string     `json:"summary"`
	Source          string     `json:"source"`
	StateSummary    string     `json:"stateSummary,omitempty"`
	NextStep        string     `json:"nextStep,omitempty"`
	LastActivityAt  string     `json:"lastActivityAt"`
	Artifacts       []string   `json:"artifacts"`
	MissingFiles    int        `json:"missingFiles"`
	AlreadyRecorded string     `json:"alreadyRecorded,omitempty"`
	Created         string     `json:"created,omitempty"`
}

// Plan is a dry run: what an import of a projects directory would do.
type Plan struct {
	Importer     string      `json:"importer"`
	Dir          string      `json:"dir"`
	Files        int         `json:"files"`
	SkippedFiles int         `json:"skippedFiles"`
	SkippedLines int         `json:"skippedLines"`
	Proposals    []*Proposal `json:"proposals"`
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Propose reads dir and groups its sessions by project root (the git top
// level when the directory still exists, else the recorded cwd). Groups
// that already match a manifest in a are marked AlreadyRecorded. Nothing
// is written.
func Propose(a *app.App, dir string) (*Plan, error) {
	sc, err := Read(dir)
	if err != nil {
		return nil, err
	}
	groups := map[string]*Proposal{}
	for _, s := range sc.Sessions {
		root, exists := s.Cwd, isDir(s.Cwd)
		if exists {
			root = marker.ProjectRoot(s.Cwd)
		}
		p := groups[root]
		if p == nil {
			p = &Proposal{Root: root, Exists: exists}
			groups[root] = p
		}
		p.Sessions = append(p.Sessions, s)
	}
	existing, _ := a.Store.List()
	plan := &Plan{Importer: Importer, Dir: dir, Files: sc.Files, SkippedFiles: sc.SkippedFiles, SkippedLines: sc.SkippedLines, Proposals: []*Proposal{}}
	for _, p := range groups {
		p.fill()
		p.AlreadyRecorded = recorded(p.Root, existing)
		plan.Proposals = append(plan.Proposals, p)
	}
	slices.SortFunc(plan.Proposals, func(x, y *Proposal) int { return cmp.Compare(y.LastActivityAt, x.LastActivityAt) })
	return plan, nil
}

// recorded returns the manifest that already covers root: a match as
// capture would make it, or any manifest with root as a path context.
// The second rule catches roots Match ignores, such as the home
// directory, so a repeated import never creates duplicates.
func recorded(root string, ms []*manifest.Manifest) string {
	if m := marker.Match(root, ms); m != nil {
		return m.ID
	}
	for _, m := range ms {
		for _, c := range m.Contexts {
			if c.Kind == marker.KindPath && c.Key == root {
				return m.ID
			}
		}
	}
	return ""
}

// fill derives the summary, state and artifacts from the group's sessions.
func (p *Proposal) fill() {
	slices.SortFunc(p.Sessions, func(x, y *Session) int { return x.FirstAt.Compare(y.FirstAt) })
	p.Summary, p.Source = "Work in "+filepath.Base(p.Root), manifest.SourceModel
pick:
	for _, s := range p.Sessions {
		for _, t := range s.Prompts {
			if sum, ok := capture.PickSummary(t); ok {
				p.Summary, p.Source = sum, manifest.SourceUser
				break pick
			}
		}
	}
	newest := slices.MaxFunc(p.Sessions, func(x, y *Session) int { return x.LastAt.Compare(y.LastAt) })
	p.StateSummary, p.NextStep = capture.ExtractState(newest.LastAssistant)
	p.LastActivityAt = newest.LastAt.UTC().Format(time.RFC3339)
	p.Artifacts = []string{}
	seen := map[string]bool{}
	for _, s := range p.Sessions {
		for _, f := range s.Files {
			if seen[f] {
				continue
			}
			seen[f] = true
			if fi, err := os.Lstat(f); p.Exists && err == nil && fi.Mode().IsRegular() && under(p.Root, f) {
				p.Artifacts = append(p.Artifacts, f)
			} else {
				p.MissingFiles++
			}
		}
	}
}

func under(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	// Compare resolved paths too, so a symlinked parent cannot point out.
	rr, err1 := filepath.EvalSymlinks(root)
	rp, err2 := filepath.EvalSymlinks(path)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err = filepath.Rel(rr, rp)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// Accept creates a manifest for every proposal not already recorded and
// registers its artifacts through the app, so the store's deny-list and
// secret scan apply. It writes .coal markers only when markers is true.
// Proposals get Created set; errors on one proposal stop the run.
func Accept(a *app.App, plan *Plan, markers bool) error {
	for _, p := range plan.Proposals {
		if p.AlreadyRecorded != "" {
			continue
		}
		newest := slices.MaxFunc(p.Sessions, func(x, y *Session) int { return x.LastAt.Compare(y.LastAt) })
		prov := manifest.Provenance{Tool: capture.ToolClaudeCode, Model: newest.Model, SessionRef: capture.SessionRef(newest.ID), CapturedBy: CapturedBy}
		m := manifest.New(p.Summary, p.Source, prov)
		m.CreatedAt = p.Sessions[0].FirstAt.UTC().Format(time.RFC3339)
		m.State.Summary, m.State.NextStep, m.State.LastActivityAt = p.StateSummary, p.NextStep, p.LastActivityAt
		m.Contexts = []manifest.Context{{Kind: marker.KindPath, Key: p.Root}}
		if p.Exists {
			m.Contexts = marker.Contexts(p.Root)
		}
		ext, _ := json.Marshal(map[string]any{"importer": Importer, "sessions": len(p.Sessions)})
		m.Ext = map[string]json.RawMessage{ExtKey: ext}
		op := store.Op{Tool: "coal-import", Action: "import claude-code"}
		if err := a.Create(m, op); err != nil {
			return fmt.Errorf("creating manifest for %s: %w", p.Root, err)
		}
		p.Created = m.ID
		for _, f := range p.Artifacts {
			if _, err := a.RegisterArtifact(m.ID, store.ArtifactInput{Path: f, Producer: prov}, op); err != nil {
				a.Store.Log("import: %s on %s: %v", f, m.ID, err)
			}
		}
		if markers && p.Exists {
			if err := marker.Write(p.Root, m.ID); err != nil {
				a.Store.Log("import: marker for %s in %s: %v", m.ID, p.Root, err)
			}
		}
	}
	return nil
}
