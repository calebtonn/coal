package index

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/manifest"
)

func open(t *testing.T) (*Index, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix, path
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// mk returns a manifest with the given summary and source, active at at.
func mk(summary, source, at string) *manifest.Manifest {
	m := manifest.New(summary, source, manifest.Provenance{Tool: "test"})
	m.State.LastActivityAt, m.UpdatedAt = at, at
	return m
}

func search(t *testing.T, ix *Index, q string, opts SearchOpts) []Result {
	t.Helper()
	rs, err := ix.Search(q, opts)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	return rs
}

func ids(rs []Result) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestPorterAndPrefix(t *testing.T) {
	ix, _ := open(t)
	m := mk("Household budget tracker as a single HTML page", "user", "2026-11-01T00:00:00Z")
	other := mk("Garden planting calendar", "user", "2026-11-01T00:00:00Z")
	must(t, ix.Upsert(m))
	must(t, ix.Upsert(other))
	for _, q := range []string{"budgets", "budgeting", "budget", "budg", "BUDGET tracker", "track"} {
		rs := search(t, ix, q, SearchOpts{})
		if !reflect.DeepEqual(ids(rs), []string{m.ID}) {
			t.Errorf("%q: got %v, want only the budget manifest", q, ids(rs))
		}
	}
	if rs := search(t, ix, "budget garden", SearchOpts{}); len(rs) != 0 {
		t.Errorf("terms are ANDed, got %v", ids(rs))
	}
}

func TestHostileQuery(t *testing.T) {
	ix, _ := open(t)
	must(t, ix.Upsert(mk("budget tracker", "user", "2026-11-01T00:00:00Z")))
	for _, q := range []string{`"); DROP TABLE docs; --`, `"`, `*`, `NEAR(a b)`, `summary:budget`, `- _ --`, `a OR`, `(`} {
		rs, err := ix.Search(q, SearchOpts{})
		if err != nil {
			t.Errorf("%q: %v", q, err)
		}
		if q == `"); DROP TABLE docs; --` && len(rs) != 0 {
			t.Errorf("%q: got %d rows", q, len(rs))
		}
	}
	if n, err := ix.Count(); err != nil || n != 1 {
		t.Fatalf("docs table after hostile queries: n=%d err=%v", n, err)
	}
	// Column filter syntax is not honoured: "summary:budget" becomes one term.
	if rs := search(t, ix, "summary:budget", SearchOpts{}); len(rs) != 0 {
		t.Errorf("summary:budget: got %d rows", len(rs))
	}
}

func TestFTSQuery(t *testing.T) {
	for in, want := range map[string]string{
		"budget":            `"budget"*`,
		"Budget tracker":    `"Budget" AND "tracker"*`,
		`"); DROP TABLE x;`: `"DROP" AND "TABLE" AND "x"*`,
		"csv-export a_b":    `"csv-export" AND "a_b"*`,
		"-- ; ***":          ``,
		"":                  ``,
	} {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSnippetEscaped(t *testing.T) {
	ix, _ := open(t)
	must(t, ix.Upsert(mk("budget <script>alert(1)</script> & <b>bold</b> x", "user", "2026-11-01T00:00:00Z")))
	rs := search(t, ix, "budget", SearchOpts{})
	if len(rs) != 1 {
		t.Fatalf("got %d results", len(rs))
	}
	s := rs[0].Snippet
	want := "<b>budget</b> &lt;script&gt;alert(1)&lt;/script&gt; &amp; &lt;b&gt;bold&lt;/b&gt; x"
	if s != want {
		t.Errorf("snippet\n got %s\nwant %s", s, want)
	}
	if rest := strings.NewReplacer("<b>", "", "</b>", "").Replace(s); strings.ContainsAny(rest, "<>") {
		t.Errorf("markup other than <b> in %s", s)
	}
	if !reflect.DeepEqual(rs[0].Matched, []string{"summary"}) {
		t.Errorf("matched %v", rs[0].Matched)
	}
	// The plain summary is returned as stored.
	if !strings.Contains(rs[0].Summary, "<script>") {
		t.Errorf("summary changed: %s", rs[0].Summary)
	}
}

func TestMatchedColumns(t *testing.T) {
	ix, _ := open(t)
	m := mk("Household budget tracker", "user", "2026-11-01T00:00:00Z")
	m.Intent.Goal = "Track monthly spending"
	m.Intent.Tags = []string{"finance", "html"}
	m.Artifacts = []manifest.Artifact{{Hash: "sha256:x", Title: "budget.html", Type: "html"}}
	m.State.NextStep = "Add CSV export"
	m.State.Summary = "Built the tracker page"
	must(t, ix.Upsert(m))
	for q, want := range map[string][]string{
		"track":      {"summary", "goal", "stateSummary"},
		"tracker":    {"summary", "stateSummary"},
		"budget":     {"summary", "artifacts"},
		"finance":    {"tags"},
		"csv":        {"nextStep"},
		"html track": {"summary", "goal", "tags", "artifacts", "stateSummary"},
	} {
		rs := search(t, ix, q, SearchOpts{})
		if len(rs) != 1 || !reflect.DeepEqual(rs[0].Matched, want) {
			t.Errorf("%q: matched %v, want %v", q, rs, want)
		}
	}
	if rs := search(t, ix, "", SearchOpts{}); rs[0].ArtifactCount != 1 || rs[0].NextStep != "Add CSV export" {
		t.Errorf("list row %+v", rs[0])
	}
}

func TestUserOutranksModel(t *testing.T) {
	ix, _ := open(t)
	// The model one is newer, so only the source tie-break puts the user first.
	model := mk("budget tracker", "model", "2026-11-03T00:00:00Z")
	user := mk("budget tracker", "user", "2026-11-01T00:00:00Z")
	must(t, ix.Upsert(model))
	must(t, ix.Upsert(user))
	rs := search(t, ix, "budget", SearchOpts{})
	if !reflect.DeepEqual(ids(rs), []string{user.ID, model.ID}) {
		t.Fatalf("got %v, want user first", ids(rs))
	}
	if rs[0].Rank != rs[1].Rank {
		t.Errorf("equal text should rank equally: %v vs %v", rs[0].Rank, rs[1].Rank)
	}
}

func TestStatusFilterAndList(t *testing.T) {
	ix, _ := open(t)
	a := mk("budget one", "user", "2026-11-01T00:00:00Z")
	b := mk("budget two", "user", "2026-11-03T00:00:00Z")
	c := mk("budget three", "user", "2026-11-02T00:00:00Z")
	c.Status = manifest.StatusDone
	for _, m := range []*manifest.Manifest{a, b, c} {
		must(t, ix.Upsert(m))
	}
	if got := ids(search(t, ix, "", SearchOpts{})); !reflect.DeepEqual(got, []string{b.ID, c.ID, a.ID}) {
		t.Errorf("list order %v", got)
	}
	if got := ids(search(t, ix, "  ", SearchOpts{Status: "done"})); !reflect.DeepEqual(got, []string{c.ID}) {
		t.Errorf("list done %v", got)
	}
	if got := ids(search(t, ix, "budget", SearchOpts{Status: "done"})); !reflect.DeepEqual(got, []string{c.ID}) {
		t.Errorf("search done %v", got)
	}
	if got := search(t, ix, "budget", SearchOpts{Limit: 2}); len(got) != 2 {
		t.Errorf("limit 2 gave %d", len(got))
	}
	must(t, ix.Delete(b.ID))
	must(t, ix.Delete(b.ID))
	if got := ids(search(t, ix, "two", SearchOpts{})); len(got) != 0 {
		t.Errorf("deleted manifest still found: %v", got)
	}
	// Upsert replaces rather than duplicates.
	a.Intent.Summary = "garden plan"
	must(t, ix.Upsert(a))
	if n, _ := ix.Count(); n != 2 {
		t.Errorf("count %d after re-upsert", n)
	}
	if got := ids(search(t, ix, "one", SearchOpts{})); len(got) != 0 {
		t.Errorf("old text still indexed: %v", got)
	}
}

func corpus() []*manifest.Manifest {
	words := []string{"budget", "tracker", "garden", "recipe", "invoice", "travel", "notes", "export", "chart", "report"}
	var ms []*manifest.Manifest
	for i := range 40 {
		src := "user"
		if i%3 == 0 {
			src = "model"
		}
		m := mk(fmt.Sprintf("%s %s project %d", words[i%10], words[(i/10)%10], i), src, fmt.Sprintf("2026-10-%02dT00:00:00Z", 1+i%28))
		m.State.NextStep = "Add " + words[(i+3)%10]
		ms = append(ms, m)
	}
	return ms
}

var queries = []string{"", "budget", "budgets track", "export", "proj", "chart report", "nothing"}

func allResults(t *testing.T, ix *Index) [][]Result {
	var out [][]Result
	for _, q := range queries {
		out = append(out, search(t, ix, q, SearchOpts{Limit: 200}))
	}
	return out
}

func TestRebuildMatchesIncremental(t *testing.T) {
	ms := corpus()
	inc, _ := open(t)
	for _, m := range ms {
		must(t, inc.Upsert(m))
	}
	// Churn: delete and re-add some, so rowids differ from a clean build.
	for _, m := range ms[:10] {
		must(t, inc.Delete(m.ID))
	}
	for _, m := range ms[:10] {
		must(t, inc.Upsert(m))
	}
	want := allResults(t, inc)

	reb, path := open(t)
	must(t, reb.Rebuild(ms[:5])) // Rebuild must clear what was there
	must(t, reb.Rebuild(ms))
	if got := allResults(t, reb); !reflect.DeepEqual(got, want) {
		t.Fatal("Rebuild differs from incremental upserts")
	}

	must(t, reb.Close())
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	fresh, err := Open(path)
	must(t, err)
	defer fresh.Close()
	must(t, fresh.Rebuild(ms))
	if got := allResults(t, fresh); !reflect.DeepEqual(got, want) {
		t.Fatal("Rebuild into a fresh file differs")
	}
}

func TestSchemaVersionMismatchRecreates(t *testing.T) {
	ix, path := open(t)
	must(t, ix.Upsert(mk("budget", "user", "2026-11-01T00:00:00Z")))
	_, err := ix.db.Exec(`UPDATE meta SET value = 'old' WHERE key = 'schema'`)
	must(t, err)
	must(t, ix.Close())
	again, err := Open(path)
	must(t, err)
	defer again.Close()
	if n, _ := again.Count(); n != 0 {
		t.Errorf("stale schema kept %d rows", n)
	}
}

func TestConcurrentOpen(t *testing.T) {
	_, path := open(t)
	other, err := Open(path)
	must(t, err)
	defer other.Close()
	errs := make(chan error, 20)
	for i := range 20 {
		go func() {
			ix, err := Open(path)
			if err == nil {
				err = ix.Upsert(mk(fmt.Sprintf("budget %d", i), "user", "2026-11-01T00:00:00Z"))
				ix.Close()
			}
			errs <- err
		}()
	}
	for range 20 {
		must(t, <-errs)
	}
	if n, _ := other.Count(); n != 20 {
		t.Errorf("count %d", n)
	}
}

func TestSearchTenThousand(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	words := strings.Fields("budget tracker garden recipe invoice travel notes export chart report household page csv plan draft " +
		"letter résumé photo album workout schedule reading list movie script poem story essay grant budgeting")
	ms := make([]*manifest.Manifest, 10000)
	for i := range ms {
		w := func(k int) string { return words[(i*k+k)%len(words)] }
		m := mk(fmt.Sprintf("%s %s %s %d", w(1), w(7), w(13), i), "user", fmt.Sprintf("2026-%02d-%02dT00:00:00Z", 1+i%10, 1+i%28))
		m.Intent.Goal = w(3) + " " + w(5)
		m.Intent.Tags = []string{w(11)}
		m.Artifacts = []manifest.Artifact{{Hash: "sha256:x", Title: w(17) + ".html", Type: "html"}}
		m.State.NextStep = "Add " + w(19)
		m.State.Summary = "Worked on " + w(23)
		ms[i] = m
	}
	ix, _ := open(t)
	t0 := time.Now()
	must(t, ix.Rebuild(ms))
	t.Logf("rebuild of %d manifests: %v", len(ms), time.Since(t0))
	two := strings.Join(strings.Fields(ms[42].Intent.Summary)[:2], " ")
	for _, q := range []string{"budget", "budg", two, "export"} {
		t0 := time.Now()
		rs := search(t, ix, q, SearchOpts{})
		d := time.Since(t0)
		t.Logf("search %q: %d results in %v", q, len(rs), d)
		if len(rs) == 0 {
			t.Errorf("%q: no results", q)
		}
		if d > 100*time.Millisecond {
			t.Errorf("%q took %v, want under 100ms", q, d)
		}
	}
}

func TestStaleUpsertIgnored(t *testing.T) {
	ix, _ := open(t)
	m := mk("budget tracker", "user", "2026-11-01T00:00:00Z")
	m.Rev = 3
	m.State.NextStep = "newer step"
	must(t, ix.Upsert(m))
	old := *m
	old.Rev, old.State.NextStep = 2, "older step"
	must(t, ix.Upsert(&old))
	if rs := search(t, ix, "", SearchOpts{}); len(rs) != 1 || rs[0].NextStep != "newer step" {
		t.Fatalf("stale upsert won: %+v", rs)
	}
	m.Rev, m.State.NextStep = 3, "same rev rewrite"
	must(t, ix.Upsert(m))
	if rs := search(t, ix, "", SearchOpts{}); rs[0].NextStep != "same rev rewrite" {
		t.Errorf("equal rev should replace: %+v", rs[0])
	}
}

func TestTimestampsNormalised(t *testing.T) {
	ix, _ := open(t)
	a := mk("budget a", "user", "2026-11-01T10:00:00.123456+02:00") // 08:00 UTC
	b := mk("budget b", "user", "2026-11-01T09:00:00Z")
	must(t, ix.Upsert(a))
	must(t, ix.Upsert(b))
	rs := search(t, ix, "", SearchOpts{})
	if rs[0].ID != b.ID || rs[1].LastActivityAt != "2026-11-01T08:00:00Z" {
		t.Errorf("order or format wrong: %+v", rs)
	}
}

func TestCorruptFileSetAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	must(t, os.WriteFile(path, []byte("this is not a database, just garbage bytes for the test"), 0o600))
	_, err := Open(path)
	if err == nil || !IsCorrupt(err) {
		t.Fatalf("Open on garbage: %v (corrupt=%v)", err, IsCorrupt(err))
	}
	moved, err := SetAside(path)
	if err != nil || len(moved) == 0 || !strings.Contains(filepath.Base(moved[0]), "index.db.broken.") {
		t.Fatalf("SetAside: %v %v", moved, err)
	}
	for _, p := range moved {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not kept: %v", p, err)
		}
	}
	ix, err := Open(path)
	must(t, err)
	ix.Close()
	if IsCorrupt(fmt.Errorf("database is locked")) {
		t.Error("a plain error counted as corrupt")
	}
}
