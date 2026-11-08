package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/store"
)

// setup builds a projects dir from the fixtures with /PROJECT replaced by
// a temp dir holding real projects: budget (a git repo with index.html),
// notes, layout, and no deleted-project.
func setup(t *testing.T) (root, projects string, a *app.App) {
	t.Helper()
	root = t.TempDir()
	for _, d := range []string{"budget/.git", "budget/src", "notes", "layout", "projects", "elsewhere/-x"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("budget/index.html", "<h1>Budget</h1>")
	write("outside.txt", "not in a project")
	fixtures := map[string]string{"budget-1.jsonl": "-p-budget", "budget-2.jsonl": "-p-budget-src", "notes-1.jsonl": "-p-notes", "gone-1.jsonl": "-p-gone", "layout-1.jsonl": "-p-layout"}
	for f, enc := range fixtures {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "claude-code", "transcripts", f))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(root, "projects", enc), 0o700)
		write(filepath.Join("projects", enc, f), strings.ReplaceAll(string(b), "/PROJECT", root))
	}
	write("projects/-p-notes/broken.jsonl", "{not json\n")
	// A symlinked project dir must not be followed.
	write("elsewhere/-x/s.jsonl", `{"type":"user","cwd":"/tmp","timestamp":"2026-01-01T00:00:00Z","message":{"content":"Read something outside the projects dir"}}`+"\n")
	if err := os.Symlink(filepath.Join(root, "elsewhere", "-x"), filepath.Join(root, "projects", "-link")); err != nil {
		t.Fatal(err)
	}
	a, err := app.Open(filepath.Join(root, "coal"), store.WithHistory(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return root, filepath.Join(root, "projects"), a
}

func byRoot(t *testing.T, plan *Plan, root string) *Proposal {
	t.Helper()
	for _, p := range plan.Proposals {
		if p.Root == root {
			return p
		}
	}
	t.Fatalf("no proposal for %s", root)
	return nil
}

func TestProposeGroupsAndWritesNothing(t *testing.T) {
	root, projects, a := setup(t)
	plan, err := Propose(a, projects)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proposals) != 4 || plan.Files != 6 || plan.SkippedFiles != 1 || plan.SkippedLines != 2 {
		t.Fatalf("plan: %d proposals, files %d, skipped files %d, lines %d", len(plan.Proposals), plan.Files, plan.SkippedFiles, plan.SkippedLines)
	}
	b := byRoot(t, plan, filepath.Join(root, "budget"))
	if len(b.Sessions) != 2 || !b.Exists {
		t.Fatalf("budget sessions %d", len(b.Sessions))
	}
	if b.Summary != "Build a household budget tracker as a single HTML page" || b.Source != "user" {
		t.Errorf("summary %q (%s)", b.Summary, b.Source)
	}
	if b.NextStep != "Add CSV export" || b.StateSummary == "" || b.LastActivityAt != "2026-10-03T14:06:01Z" {
		t.Errorf("state %q / %q / %s", b.StateSummary, b.NextStep, b.LastActivityAt)
	}
	if len(b.Artifacts) != 1 || filepath.Base(b.Artifacts[0]) != "index.html" || b.MissingFiles != 2 {
		t.Errorf("artifacts %v, missing %d", b.Artifacts, b.MissingFiles)
	}
	n := byRoot(t, plan, filepath.Join(root, "notes"))
	if n.Summary != "Work in notes" || n.Source != "model" {
		t.Errorf("notes summary %q (%s)", n.Summary, n.Source)
	}
	// A known wrapper tag is dropped; a prompt that merely starts with
	// "<" is the user's.
	if l := byRoot(t, plan, filepath.Join(root, "layout")); l.Summary != "<div> is off centre, fix it" || l.Source != "user" {
		t.Errorf("layout summary %q (%s)", l.Summary, l.Source)
	}
	g := byRoot(t, plan, filepath.Join(root, "deleted-project"))
	if g.Exists || g.Source != "user" || len(g.Artifacts) != 0 {
		t.Errorf("gone %+v", g)
	}
	if es, _ := os.ReadDir(filepath.Join(root, "coal", "manifests")); len(es) != 0 {
		t.Errorf("dry run wrote %d manifests", len(es))
	}
}

func TestAcceptThenAlreadyRecorded(t *testing.T) {
	root, projects, a := setup(t)
	plan, _ := Propose(a, projects)
	if err := Accept(a, plan, false); err != nil {
		t.Fatal(err)
	}
	b := byRoot(t, plan, filepath.Join(root, "budget"))
	m, err := a.Get(b.Created)
	if err != nil {
		t.Fatal(err)
	}
	if m.Intent.Source != "user" || m.State.NextStep != "Add CSV export" || len(m.Artifacts) != 1 || m.Provenance.SessionRef != "claude-code:session:11111111-aaaa-4aaa-8aaa-000000000002" {
		t.Errorf("manifest %+v", m)
	}
	if m.CreatedAt != "2026-10-01T09:00:00Z" || m.State.LastActivityAt != "2026-10-03T14:06:01Z" || len(m.Contexts) == 0 || m.Ext[ExtKey] == nil {
		t.Errorf("times %s %s, contexts %v", m.CreatedAt, m.State.LastActivityAt, m.Contexts)
	}
	if _, err := os.Stat(filepath.Join(root, "budget", marker.FileName)); !os.IsNotExist(err) {
		t.Error("a marker was written without --markers")
	}
	if rs, _ := a.Search("budget", index.SearchOpts{}); len(rs) != 1 {
		t.Errorf("search after import: %d", len(rs))
	}
	again, _ := Propose(a, projects)
	for _, p := range again.Proposals {
		if p.AlreadyRecorded == "" {
			t.Errorf("%s not reported as already recorded", p.Root)
		}
	}
}

func TestAcceptMarkers(t *testing.T) {
	root, projects, a := setup(t)
	plan, _ := Propose(a, projects)
	if err := Accept(a, plan, true); err != nil {
		t.Fatal(err)
	}
	id, _, ok := marker.Find(filepath.Join(root, "budget", "src"))
	if !ok || id != byRoot(t, plan, filepath.Join(root, "budget")).Created {
		t.Errorf("marker %q %v", id, ok)
	}
}

func TestHomeRootNotDuplicated(t *testing.T) {
	root, projects, a := setup(t)
	t.Setenv("HOME", filepath.Join(root, "notes"))
	plan, _ := Propose(a, projects)
	if err := Accept(a, plan, false); err != nil {
		t.Fatal(err)
	}
	again, _ := Propose(a, projects)
	if p := byRoot(t, again, filepath.Join(root, "notes")); p.AlreadyRecorded == "" {
		t.Error("a project at the home directory was proposed again")
	}
}
