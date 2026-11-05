package marker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
)

// fakeRepo makes dir a git top level with an optional origin url.
func fakeRepo(t *testing.T, dir, origin string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\tbare = false\n"
	if origin != "" {
		cfg += "[remote \"upstream\"]\n\turl = https://example.com/other/x\n[remote \"origin\"]\n\turl = " + origin + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	r, _ := filepath.EvalSymlinks(p)
	return r
}

func man(status string, ctx ...manifest.Context) *manifest.Manifest {
	m := manifest.New("x", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	m.Status, m.Contexts = status, ctx
	return m
}

func pathCtx(key string) manifest.Context { return manifest.Context{Kind: KindPath, Key: key} }

// sandbox returns a resolved temp dir and points HOME at a separate one.
func sandbox(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", mkdir(t, t.TempDir(), "home"))
	return mkdir(t, t.TempDir())
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@GitHub.com:Owner/Repo.git":               "github.com/Owner/Repo",
		"ssh://git@github.com/owner/repo":             "github.com/owner/repo",
		"ssh://git@github.com:22/owner/repo.git/":     "github.com/owner/repo",
		"https://github.com/owner/repo/":              "github.com/owner/repo",
		"https://user:tok@GitLab.example.com/a/b.git": "gitlab.example.com/a/b",
		"/srv/git/repo.git":                           "/srv/git/repo",
		"":                                            "",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectRootAndRemote(t *testing.T) {
	base := sandbox(t)
	repo := mkdir(t, base, "budget")
	fakeRepo(t, repo, "git@github.com:dave/budget.git")
	sub := mkdir(t, repo, "src", "js")
	if got := ProjectRoot(sub); got != repo {
		t.Errorf("root = %q, want %q", got, repo)
	}
	if got := OriginRemote(repo); got != "github.com/dave/budget" {
		t.Errorf("remote = %q", got)
	}
	plain := mkdir(t, base, "plain")
	if got := ProjectRoot(plain); got != plain || OriginRemote(plain) != "" {
		t.Errorf("plain root %q", got)
	}
	link := filepath.Join(base, "link")
	_ = os.Symlink(sub, link)
	if got := ProjectRoot(link); got != repo {
		t.Errorf("symlink root = %q", got)
	}

	// A worktree: .git is a file pointing into the main repo.
	wt := mkdir(t, base, "wt")
	gd := mkdir(t, repo, ".git", "worktrees", "wt")
	_ = os.WriteFile(filepath.Join(gd, "commondir"), []byte("../..\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gd+"\n"), 0o644)
	if got := OriginRemote(ProjectRoot(wt)); got != "github.com/dave/budget" {
		t.Errorf("worktree remote = %q", got)
	}

	ctx := Contexts(sub)
	if len(ctx) != 2 || ctx[0].Kind != KindPath || ctx[0].Key != repo || ctx[0].Label != "budget" ||
		ctx[1].Kind != KindRepo || ctx[1].Key != "github.com/dave/budget" {
		t.Errorf("contexts = %+v", ctx)
	}
	if ctx := Contexts(plain); len(ctx) != 1 || ctx[0].Key != plain {
		t.Errorf("plain contexts = %+v", ctx)
	}
}

func TestWriteAndFind(t *testing.T) {
	base := sandbox(t)
	repo := mkdir(t, base, "budget")
	fakeRepo(t, repo, "")
	id := manifest.NewID()
	if err := Write(repo, id); err != nil {
		t.Fatal(err)
	}
	if err := Write(repo, id); err != nil {
		t.Fatalf("same id again: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, FileName))
	if !strings.Contains(string(b), SchemaURL) || !strings.Contains(string(b), id) {
		t.Errorf("marker: %s", b)
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".coal\n") != 1 {
		t.Errorf("exclude: %q", ex)
	}
	if err := Write(repo, manifest.NewID()); err == nil {
		t.Error("overwrote a marker for another manifest")
	}
	if err := Write(repo, "not-an-id"); err == nil {
		t.Error("accepted a bad id")
	}
	got, dir, ok := Find(mkdir(t, repo, "a", "b"))
	if !ok || got != id || dir != repo {
		t.Errorf("Find = %q %q %v", got, dir, ok)
	}

	// Existing exclude lines are kept and not duplicated.
	other := mkdir(t, base, "other")
	fakeRepo(t, other, "")
	_ = os.MkdirAll(filepath.Join(other, ".git", "info"), 0o755)
	_ = os.WriteFile(filepath.Join(other, ".git", "info", "exclude"), []byte("*.log"), 0o644)
	if err := Write(other, id); err != nil {
		t.Fatal(err)
	}
	ex, _ = os.ReadFile(filepath.Join(other, ".git", "info", "exclude"))
	if string(ex) != "*.log\n.coal\n" {
		t.Errorf("exclude: %q", ex)
	}
}

func TestFindSkips(t *testing.T) {
	base := sandbox(t)
	home := os.Getenv("HOME")
	// The data dir at $HOME/.coal is a directory and never a marker.
	_ = os.MkdirAll(filepath.Join(home, FileName, "manifests"), 0o755)
	if _, _, ok := Find(mkdir(t, home, "proj")); ok {
		t.Error("matched the data dir")
	}
	outer := mkdir(t, base, "outer")
	inner := mkdir(t, outer, "inner")
	id := manifest.NewID()
	if err := Write(outer, id); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"{nope", `{"manifest":"0190a1b2-c3d4-4e5f-8a6b-7c8d9e0f1a2b"}`} {
		_ = os.WriteFile(filepath.Join(inner, FileName), []byte(bad), 0o644)
		if got, _, _ := Find(inner); got != id {
			t.Errorf("malformed marker %q: Find = %q, want the outer one", bad, got)
		}
	}
}

func TestMatch(t *testing.T) {
	base := sandbox(t)
	home := os.Getenv("HOME")
	budget := mkdir(t, base, "x", "budget")
	old := mkdir(t, base, "x", "budget-old")
	sub := mkdir(t, budget, "src")
	deep := mkdir(t, budget, "src", "lib")

	mBudget := man(manifest.StatusActive, pathCtx(budget))
	mSrc := man(manifest.StatusPaused, pathCtx(sub))
	mHome := man(manifest.StatusActive, pathCtx(home), pathCtx("/"))
	all := []*manifest.Manifest{mHome, mBudget, mSrc}

	cases := []struct {
		name string
		cwd  string
		want *manifest.Manifest
	}{
		{"exact", budget, mBudget},
		{"longest key wins", deep, mSrc},
		{"sibling with a shared prefix", old, nil},
		{"home directory", home, nil},
		{"under home but no project", mkdir(t, home, "notes"), nil},
	}
	for _, c := range cases {
		if got := Match(c.cwd, all); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, id(got), id(c.want))
		}
	}

	// A done manifest does not match.
	mBudget.Status, mSrc.Status = manifest.StatusDone, manifest.StatusAbandoned
	if got := Match(budget, all); got != nil {
		t.Errorf("done matched: %v", id(got))
	}
	mBudget.Status = manifest.StatusActive

	// A marker beats a longer path context, and the nearest marker wins.
	mSrc.Status = manifest.StatusActive
	mMarked := man(manifest.StatusActive)
	mNear := man(manifest.StatusActive)
	all = append(all, mMarked, mNear)
	if err := Write(budget, mMarked.ID); err != nil {
		t.Fatal(err)
	}
	if got := Match(deep, all); got != mMarked {
		t.Errorf("marker: got %v", id(got))
	}
	if err := Write(sub, mNear.ID); err != nil {
		t.Fatal(err)
	}
	if got := Match(deep, all); got != mNear {
		t.Errorf("nearest marker: got %v", id(got))
	}
	// A marker for an unknown or done manifest falls back to contexts.
	mNear.Status = manifest.StatusDone
	if got := Match(sub, all); got != mMarked {
		t.Errorf("done marker: got %v", id(got))
	}
	_ = os.WriteFile(filepath.Join(sub, FileName), []byte("garbage"), 0o644)
	_ = os.Remove(filepath.Join(budget, FileName))
	if got := Match(deep, all); got != mSrc {
		t.Errorf("malformed marker: got %v", id(got))
	}
}

func TestMatchRepoClones(t *testing.T) {
	base := sandbox(t)
	one := mkdir(t, base, "one", "budget")
	two := mkdir(t, base, "two", "budget-clone")
	fakeRepo(t, one, "git@github.com:dave/budget.git")
	fakeRepo(t, two, "https://GitHub.com/dave/budget")
	m := man(manifest.StatusActive, Contexts(one)...)
	other := man(manifest.StatusActive, manifest.Context{Kind: KindRepo, Key: "github.com/dave/other"})
	for _, cwd := range []string{one, mkdir(t, two, "src")} {
		if got := Match(cwd, []*manifest.Manifest{other, m}); got != m {
			t.Errorf("%s: got %v", cwd, id(got))
		}
	}
	if got := Match(mkdir(t, base, "three"), []*manifest.Manifest{m}); got != nil {
		t.Errorf("no repo matched %v", id(got))
	}
}

func id(m *manifest.Manifest) string {
	if m == nil {
		return "nil"
	}
	return m.ID
}

func TestHomeAndAboveNeverAnchor(t *testing.T) {
	sandbox(t)
	home := os.Getenv("HOME")
	notes := mkdir(t, home, "notes")
	id := manifest.NewID()
	// A stray marker file in ~ is never read, and none is ever written there.
	os.WriteFile(filepath.Join(home, FileName), []byte(`{"manifest":"`+id+`"}`), 0o644)
	if got, _, ok := Find(notes); ok {
		t.Errorf("matched ~/.coal from ~/notes: %s", got)
	}
	m := man(manifest.StatusActive)
	m.ID = id
	if got := Match(notes, []*manifest.Manifest{m}); got != nil {
		t.Errorf("Match used ~/.coal")
	}
	os.Remove(filepath.Join(home, FileName))
	for _, dir := range []string{home, filepath.Dir(home), "/"} {
		if !Unanchored(dir) {
			t.Errorf("%s is anchorable", dir)
		}
		if dir == home && Write(dir, id) == nil {
			t.Error("wrote a marker in ~")
		}
	}
	if Unanchored(notes) {
		t.Error("~/notes is unanchored")
	}
	if _, err := os.Stat(filepath.Join(home, FileName)); err == nil {
		t.Error("~/.coal exists")
	}
}

func TestWorktreeExclude(t *testing.T) {
	base := sandbox(t)
	repo := mkdir(t, base, "main")
	fakeRepo(t, repo, "")
	wt := mkdir(t, base, "wt")
	gd := mkdir(t, repo, ".git", "worktrees", "wt")
	os.WriteFile(filepath.Join(gd, "commondir"), []byte("../..\n"), 0o644)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gd+"\n"), 0o644)
	if err := Write(wt, manifest.NewID()); err != nil {
		t.Fatal(err)
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if string(ex) != ".coal\n" {
		t.Errorf("main repo exclude: %q", ex)
	}
	if _, err := os.Stat(filepath.Join(gd, "info")); err == nil {
		t.Error("wrote info/ in the worktree gitdir")
	}
}
