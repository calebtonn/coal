package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/calebtonn/coal/internal/manifest"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// setup opens a store without history in a temp dir and creates one manifest.
func setup(t *testing.T, opts ...Option) (*Store, *manifest.Manifest) {
	t.Helper()
	s, err := Open(t.TempDir(), append([]Option{WithHistory(false)}, opts...)...)
	must(t, err)
	m := manifest.New("Household budget tracker", manifest.SourceUser, manifest.Provenance{Tool: "coal-cli"})
	must(t, s.Create(m, Op{Tool: "test", Action: "create"}))
	return s, m
}

func TestRoundTrip(t *testing.T) {
	s, m := setup(t)
	got, err := s.Update(m.ID, 1, func(m *manifest.Manifest) error {
		m.State.NextStep = "Add CSV export"
		return nil
	}, Op{})
	must(t, err)
	data, err := os.ReadFile(filepath.Join(s.Dir(), "manifests", m.ID+".json"))
	must(t, err)
	var disk manifest.Manifest
	must(t, json.Unmarshal(data, &disk))
	if m.Rev != 1 || disk.Rev != 2 || disk.State.NextStep != "Add CSV export" || !reflect.DeepEqual(&disk, got) {
		t.Fatalf("create rev %d; on disk:\n%s", m.Rev, data)
	}
}

func TestStaleRevConflicts(t *testing.T) {
	s, m := setup(t)
	_, err := s.Update(m.ID, 1, func(*manifest.Manifest) error { return nil }, Op{})
	must(t, err)
	path := filepath.Join(s.Dir(), "manifests", m.ID+".json")
	before, _ := os.ReadFile(path)
	_, err = s.Update(m.ID, 1, func(m *manifest.Manifest) error { m.Status = "done"; return nil }, Op{})
	var ce *ConflictError
	if !errors.As(err, &ce) || !errors.Is(err, ErrConflict) || ce.Current != 2 {
		t.Fatalf("want conflict at rev 2, got %v", err)
	}
	if after, _ := os.ReadFile(path); string(before) != string(after) {
		t.Fatal("file changed on conflict")
	}
}

func TestParallelRegister(t *testing.T) {
	s, m := setup(t)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			in := ArtifactInput{Content: fmt.Appendf(nil, "body %d", i), URI: fmt.Sprintf("file:///tmp/a%d.txt", i)}
			if _, err := s.RegisterArtifact(m.ID, in, Op{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := s.Get(m.ID)
	must(t, err)
	if len(got.Artifacts) != 50 || got.Rev != 51 {
		t.Fatalf("got %d artifacts at rev %d, want 50 at 51", len(got.Artifacts), got.Rev)
	}
}

func TestSameURIVersions(t *testing.T) {
	s, m := setup(t)
	p := filepath.Join(t.TempDir(), "budget tracker.html")
	escaped := "file://" + strings.ReplaceAll(p, " ", "%20")
	var last *manifest.Artifact
	// The second write names the same file through another spelling of its uri.
	for i, body := range []string{"<h1>v1</h1>", "<h1>v2</h1>", "<h1>v2</h1>"} {
		must(t, os.WriteFile(p, []byte(body), 0o600))
		in := ArtifactInput{Path: p}
		if i == 1 {
			in.URI = "FILE://" + p + "/" // upper-case scheme, raw space, trailing slash
		}
		a, err := s.RegisterArtifact(m.ID, in, Op{})
		must(t, err)
		last = a
	}
	got, _ := s.Get(m.ID)
	if got.Rev != 3 || len(got.Artifacts) != 1 {
		t.Fatalf("rev %d with %d artifacts, want rev 3 with 1", got.Rev, len(got.Artifacts))
	}
	a := got.Artifacts[0]
	if len(a.Versions) != 2 || a.Hash != a.Versions[1].Hash || a.Hash != last.Hash {
		t.Fatalf("versions wrong: %+v", a)
	}
	if a.Type != "html" || !strings.HasPrefix(a.MIME, "text/html") || a.Title != "budget tracker.html" || a.URI != escaped {
		t.Fatalf("defaults wrong: %+v", a)
	}
	if body, err := s.ReadArtifact(a.Hash); err != nil || string(body) != "<h1>v2</h1>" {
		t.Fatalf("latest body %q, %v", body, err)
	}
	// Without a uri, identity is the hash.
	for range 2 {
		_, err := s.RegisterArtifact(m.ID, ArtifactInput{Content: []byte("same"), Title: "note.md"}, Op{})
		must(t, err)
	}
	if got, _ := s.Get(m.ID); len(got.Artifacts) != 2 || got.Artifacts[1].Type != "doc" {
		t.Fatalf("want one more doc artifact, got %+v", got.Artifacts)
	}
}

func TestSecretsSkipped(t *testing.T) {
	s, m := setup(t)
	dir := t.TempDir()
	files := map[string]string{".env": "PASSWORD=hunter2\n", "deploy.sh": "export AWS=AKIAABCDEFGHIJKLMNOP\n"}
	for name, body := range files {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, []byte(body), 0o600))
		a, err := s.RegisterArtifact(m.ID, ArtifactInput{Path: p}, Op{})
		must(t, err)
		if !strings.HasPrefix(a.Skipped, "possible secret: ") || a.Versions[0].Skipped != a.Skipped ||
			*a.Size != int64(len(body)) || strings.Contains(a.Skipped, "AKIAAB") {
			t.Fatalf("%s: %+v", name, a)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Dir(), "artifacts")); len(entries) != 0 {
		t.Fatalf("%d bodies landed in artifacts/", len(entries))
	}
}

func TestHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	s, m := setup(t, WithHistory(true))
	_, err := s.RegisterArtifact(m.ID, ArtifactInput{Content: []byte("x"), Title: "budget.html"}, Op{Tool: "claude-code"})
	must(t, err)
	_, err = s.Remove(m.ID, Op{})
	must(t, err)
	log, err := s.git("log", "--format=%s")
	must(t, err)
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 3 || lines[1] != "claude-code: register artifact budget.html on "+m.ID {
		t.Fatalf("want 3 commits, got:\n%s", log)
	}
	if tracked, _ := s.git("ls-files"); strings.Contains(tracked, "artifacts/") || strings.Contains(tracked, ".lock") {
		t.Fatalf("unexpected tracked paths:\n%s", tracked)
	}
}

func TestListAndRemove(t *testing.T) {
	s, a := setup(t)
	b := manifest.New("Second", manifest.SourceUser, a.Provenance)
	must(t, s.Create(b, Op{}))
	must(t, os.WriteFile(filepath.Join(s.Dir(), "manifests", manifest.NewID()+".json"), []byte("{bad"), 0o600))
	ms, errs := s.List()
	if len(ms) != 2 || len(errs) != 1 || ms[0].ID != a.ID || ms[1].ID != b.ID {
		t.Fatalf("got %d manifests, %d errors", len(ms), len(errs))
	}
	dst, err := s.Remove(a.ID, Op{})
	must(t, err)
	if _, err := os.Stat(dst); err != nil || filepath.Dir(dst) != filepath.Join(s.Dir(), "trash") {
		t.Fatalf("moved to %s: %v", dst, err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove: %v", err)
	}
}

func TestPathSafety(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.Get("../../etc/passwd"); err == nil {
		t.Fatal("Get accepted a path")
	}
	for _, h := range []string{"../../etc/passwd", "sha256:../../etc/passwd", strings.Repeat("A", 64)} {
		if _, err := s.ReadArtifact(h); err == nil {
			t.Fatalf("%q accepted", h)
		}
	}
	ref := "claude-code:session:abc/../x"
	if p := s.SessionPath(ref); filepath.Base(p) != "claude-code_session_abc_.._x.json" {
		t.Fatalf("path %s", p)
	}
	must(t, s.WriteSession(ref, map[string]string{"cwd": "/tmp"}))
	var got map[string]string
	if err := s.ReadSession(ref, &got); err != nil || got["cwd"] != "/tmp" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestIDMustMatchFileName(t *testing.T) {
	s, m := setup(t)
	data, err := os.ReadFile(filepath.Join(s.Dir(), "manifests", m.ID+".json"))
	must(t, err)
	other := manifest.NewID()
	must(t, os.WriteFile(filepath.Join(s.Dir(), "manifests", other+".json"), data, 0o600))
	if _, err := s.Get(other); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("Get of a copied file: %v", err)
	}
	ms, errs := s.List()
	if len(ms) != 1 || ms[0].ID != m.ID || len(errs) != 1 || !strings.Contains(errs[0].Error(), other) {
		t.Fatalf("List: %d manifests, errs %v", len(ms), errs)
	}
}

func TestCanonicalURI(t *testing.T) {
	for in, want := range map[string]string{
		"file:///tmp/a%20b.html":        "file:///tmp/a%20b.html",
		"FILE:///tmp/a b.html":          "file:///tmp/a%20b.html",
		"file:///tmp/a%2Db.html":        "file:///tmp/a-b.html",
		"https://Example.COM/Docs/":     "https://example.com/Docs",
		"https://example.com/":          "https://example.com/",
		"https://example.com/p?q=1#top": "https://example.com/p?q=1#top",
		" file:///x ":                   "file:///x",
		"":                              "",
	} {
		if got := CanonicalURI(in); got != want {
			t.Errorf("CanonicalURI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegisterChecksManifestAndMIME(t *testing.T) {
	s, m := setup(t)
	in := ArtifactInput{Content: []byte("orphan?"), Title: "a.txt"}
	if _, err := s.RegisterArtifact(manifest.NewID(), in, Op{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown manifest: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Dir(), "artifacts")); len(entries) != 0 {
		t.Fatalf("a body was stored for a missing manifest")
	}
	for _, bad := range []string{"text/html; charset", "not a type", "text/"} {
		in.MIME = bad
		if _, err := s.RegisterArtifact(m.ID, in, Op{}); err == nil {
			t.Errorf("mime %q accepted", bad)
		}
	}
	in.MIME = "text/html; charset=utf-8"
	if _, err := s.RegisterArtifact(m.ID, in, Op{}); err != nil {
		t.Errorf("valid mime rejected: %v", err)
	}
}
