package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

func openApp(t *testing.T) *app.App {
	t.Helper()
	a, err := app.Open(t.TempDir(), store.WithHistory(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func start(t *testing.T, a *app.App, ui fstest.MapFS) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(a, Options{UI: ui}))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, body string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1]
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func want(t *testing.T, res *http.Response, body []byte, code int) {
	t.Helper()
	if res.StatusCode != code {
		t.Fatalf("%s %s: status %d, want %d: %s", res.Request.Method, res.Request.URL.Path, res.StatusCode, code, body)
	}
}

// seed creates a budget manifest with an HTML artifact via the API.
func seed(t *testing.T, srv *httptest.Server) *manifest.Manifest {
	t.Helper()
	res, body := do(t, srv, "POST", "/api/manifests", `{"summary":"Household budget tracker as one HTML page","tags":["finance"]}`)
	want(t, res, body, 201)
	var m manifest.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.Intent.Source != "user" || m.Provenance.Tool != "coal-web" {
		t.Fatalf("defaults: source %q tool %q", m.Intent.Source, m.Provenance.Tool)
	}
	res, body = do(t, srv, "POST", "/api/manifests/"+m.ID+"/artifacts", `{"content":"<h1>Budget</h1>","title":"budget.html"}`)
	want(t, res, body, 201)
	return &m
}

func TestServeBindsLoopback(t *testing.T) {
	a := openApp(t)
	if err := Serve(context.Background(), a, "0.0.0.0:0", Options{}); err == nil {
		t.Fatal("Serve accepted 0.0.0.0")
	}
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, a, "127.0.0.1:0", Options{Ready: func(x net.Addr) { addr <- x }})
	}()
	var got net.Addr
	select {
	case got = <-addr:
	case err := <-done:
		t.Fatal(err)
	}
	tcp := got.(*net.TCPAddr)
	if !tcp.IP.Equal(net.IPv4(127, 0, 0, 1)) || tcp.Port == 0 {
		t.Fatalf("listening on %v", got)
	}
	res, err := http.Get("http://" + got.String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(a.Store.Dir(), "logs", "daemon.log"))
	if err != nil || !bytes.Contains(log, []byte("GET /health 200")) {
		t.Fatalf("daemon.log: %q %v", log, err)
	}
}

func TestAPI(t *testing.T) {
	a := openApp(t)
	srv := start(t, a, fstest.MapFS{})

	res, body := do(t, srv, "GET", "/health", "")
	want(t, res, body, 200)
	if strings.TrimSpace(string(body)) != `{"ok":true,"version":"0.1.0-poc"}` {
		t.Fatalf("health: %s", body)
	}

	m := seed(t, srv)
	res, body = do(t, srv, "GET", "/api/manifests?q=budgets&limit=5", "")
	want(t, res, body, 200)
	var found struct {
		Results []map[string]any `json:"results"`
	}
	json.Unmarshal(body, &found)
	if len(found.Results) != 1 || found.Results[0]["id"] != m.ID || found.Results[0]["artifactCount"] != 1.0 ||
		!strings.Contains(found.Results[0]["snippet"].(string), "<b>") || found.Results[0]["nextStep"] != nil {
		t.Fatalf("search: %s", body)
	}
	res, body = do(t, srv, "GET", "/api/manifests?status=bogus", "")
	want(t, res, body, 400)

	res, body = do(t, srv, "GET", "/api/manifests/"+m.ID[:8], "")
	want(t, res, body, 200)
	if res.Header.Get("ETag") != `"2"` || !strings.Contains(string(body), m.ID) {
		t.Fatalf("get by prefix: etag %s body %s", res.Header.Get("ETag"), body)
	}
	res, body = do(t, srv, "GET", "/api/manifests/0000", "")
	want(t, res, body, 400)
	res, body = do(t, srv, "GET", "/api/manifests/00000000", "")
	want(t, res, body, 404)
	if !strings.HasPrefix(string(body), `{"error":`) {
		t.Fatalf("404 body: %s", body)
	}

	// PATCH: stale If-Match is a 412 with the current manifest; the file is unchanged.
	path := filepath.Join(a.Store.Dir(), "manifests", m.ID+".json")
	before, _ := os.ReadFile(path)
	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"state":{"nextStep":"Add CSV export"}}`, "If-Match", `"1"`)
	want(t, res, body, 412)
	if res.Header.Get("ETag") != `"2"` || !strings.Contains(string(body), `"rev": 2`) && !strings.Contains(string(body), `"rev":2`) {
		t.Fatalf("412 body: %s", body)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("stale PATCH changed the file")
	}
	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"intent":{"summary":"renamed"}}`)
	want(t, res, body, 400)
	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"createdAt":"2020-01-01T00:00:00Z"}`)
	want(t, res, body, 400)
	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"status":"sleeping"}`)
	want(t, res, body, 400)

	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID,
		`{"status":"paused","intent":{"tags":null},"state":{"nextStep":"Add CSV export","openQuestions":["Which currency?"]}}`, "If-Match", `"2"`)
	want(t, res, body, 200)
	got, _ := a.Get(m.ID)
	if got.Rev != 3 || got.Status != "paused" || got.State.NextStep != "Add CSV export" || got.Intent.Tags != nil ||
		got.Intent.Summary != m.Intent.Summary || res.Header.Get("ETag") != `"3"` {
		t.Fatalf("after PATCH: %+v", got)
	}
	if hits, _ := a.Search("csv", index.SearchOpts{}); len(hits) != 1 {
		t.Fatal("PATCH did not reach the index")
	}

	res, body = do(t, srv, "GET", "/api/schema", "")
	want(t, res, body, 200)
	res, body = do(t, srv, "POST", "/api/reindex", "")
	want(t, res, body, 200)
	if strings.TrimSpace(string(body)) != `{"indexed":1,"skipped":0}` {
		t.Fatalf("reindex: %s", body)
	}
	res, body = do(t, srv, "DELETE", "/api/manifests/"+m.ID, "")
	want(t, res, body, 404)
}

func TestArtifacts(t *testing.T) {
	a := openApp(t)
	srv := start(t, a, fstest.MapFS{})
	m := seed(t, srv)
	got, _ := a.Get(m.ID)
	hex := strings.TrimPrefix(got.Artifacts[0].Hash, "sha256:")

	res, body := do(t, srv, "GET", "/api/artifacts/"+hex, "")
	want(t, res, body, 200)
	if string(body) != "<h1>Budget</h1>" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") ||
		res.Header.Get("Content-Security-Policy") != "sandbox" || res.Header.Get("X-Content-Type-Options") != "nosniff" ||
		res.Header.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("artifact: %v %q", res.Header, body)
	}
	for _, bad := range []string{"..%2f..%2fetc%2fpasswd", strings.ToUpper(hex), hex[:63]} {
		res, body = do(t, srv, "GET", "/api/artifacts/"+bad, "")
		if res.StatusCode != 400 && res.StatusCode != 404 {
			t.Fatalf("%s: status %d", bad, res.StatusCode)
		}
	}

	// A path artifact: must be absolute and a regular file.
	file := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(file, []byte("# Notes"), 0o600)
	res, body = do(t, srv, "POST", "/api/manifests/"+m.ID+"/artifacts", `{"path":"notes.md"}`)
	want(t, res, body, 400)
	res, body = do(t, srv, "POST", "/api/manifests/"+m.ID+"/artifacts", `{"path":`+jsonString(filepath.Dir(file))+`}`)
	want(t, res, body, 400)
	res, body = do(t, srv, "POST", "/api/manifests/"+m.ID+"/artifacts", `{"path":`+jsonString(file)+`}`)
	want(t, res, body, 201)
	var art manifest.Artifact
	json.Unmarshal(body, &art)
	res, body = do(t, srv, "GET", "/api/artifacts/"+strings.TrimPrefix(art.Hash, "sha256:"), "")
	want(t, res, body, 200)
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("markdown MIME: %s", res.Header.Get("Content-Type"))
	}

	// A secret is recorded without a body and served as a JSON 404.
	res, body = do(t, srv, "POST", "/api/manifests/"+m.ID+"/artifacts", `{"content":"KEY=1","title":".env"}`)
	want(t, res, body, 201)
	json.Unmarshal(body, &art)
	res, body = do(t, srv, "GET", "/api/artifacts/"+strings.TrimPrefix(art.Hash, "sha256:"), "")
	want(t, res, body, 404)
	if art.Skipped == "" || !strings.Contains(string(body), art.Skipped) {
		t.Fatalf("skipped artifact: %s", body)
	}
}

func TestGuards(t *testing.T) {
	srv := start(t, openApp(t), fstest.MapFS{})
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	res, body := do(t, srv, "GET", "/health", "", "Host", "evil.example:"+strconv.Itoa(port))
	want(t, res, body, 403)
	res, body = do(t, srv, "GET", "/api/manifests", "", "Host", "localhost:1")
	want(t, res, body, 403)
	res, body = do(t, srv, "GET", "/health", "", "Host", "localhost:"+strconv.Itoa(port))
	want(t, res, body, 200)
	res, body = do(t, srv, "POST", "/api/manifests", `{"summary":"from elsewhere"}`, "Origin", "http://evil.example")
	want(t, res, body, 403)
	if !strings.HasPrefix(string(body), `{"error":`) {
		t.Fatalf("not a JSON error: %s", body)
	}
	res, body = do(t, srv, "POST", "/api/manifests", `{"summary":"from the UI"}`, "Origin", srv.URL)
	want(t, res, body, 201)
}

func TestUI(t *testing.T) {
	a := openApp(t)
	res, body := do(t, start(t, a, fstest.MapFS{}), "GET", "/", "")
	want(t, res, body, 200)
	if !strings.Contains(string(body), "The web UI is not built. Run make web, then make build.") {
		t.Fatalf("fallback page: %s", body)
	}

	script := `(function(){document.documentElement.dataset.theme="light"})();`
	sum := sha256.Sum256([]byte(script))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	srv := start(t, a, fstest.MapFS{
		"index.html":       {Data: []byte(`<html><head><script data-wf-theme-init="">` + script + `</script><script type="module" src="/assets/app.js"></script></head></html>`)},
		"assets/app.js":    {Data: []byte(`console.log(1)`)},
		"favicon.svg":      {Data: []byte(`<svg/>`)},
		"assets/.keep.txt": {Data: nil},
	})
	for _, p := range []string{"/", "/foo", "/manifests/abc"} {
		res, body = do(t, srv, "GET", p, "")
		want(t, res, body, 200)
		csp := res.Header.Get("Content-Security-Policy")
		if !strings.Contains(string(body), "data-wf-theme-init") || !strings.Contains(csp, "script-src 'self' "+hash+";") ||
			!strings.Contains(csp, "object-src 'none'") || !strings.Contains(csp, "font-src 'self' data:;") || res.Header.Get("X-Frame-Options") != "SAMEORIGIN" ||
			res.Header.Get("Referrer-Policy") != "no-referrer" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: csp %q body %s", p, csp, body)
		}
	}
	res, body = do(t, srv, "GET", "/assets/app.js", "")
	want(t, res, body, 200)
	if !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("asset type %s", res.Header.Get("Content-Type"))
	}
	res, body = do(t, srv, "GET", "/assets/gone.js", "")
	want(t, res, body, 404)
	res, body = do(t, srv, "POST", "/", "")
	want(t, res, body, 405)
}

func TestRotatingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.log")
	f, err := openRotating(p, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		f.Write([]byte("12345678\n"))
	}
	f.Close()
	for _, name := range []string{p, p + ".1", p + ".2", p + ".3"} {
		if st, err := os.Stat(name); err != nil || st.Size() != 9 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(p + ".4"); err == nil {
		t.Fatal("kept more than 3 rotated files")
	}
}

func TestTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan struct{}, 10)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, openApp(t), "127.0.0.1:0", Options{TickEvery: 10 * time.Millisecond, Tick: func() { ticks <- struct{}{} }})
	}()
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick was not called")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func (c *mimeCache) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rebuilds
}

// Fable review U5: unknown hashes cannot make the daemon re-read the data
// dir on every request.
func TestMIMECacheBounded(t *testing.T) {
	a := openApp(t)
	s, h := newServer(a, Options{UI: fstest.MapFS{}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	m := seed(t, srv) // the seed's own writes reset the cache
	fake := func(i int) string { return strings.Repeat("0", 60) + fmt.Sprintf("%04x", i) }

	base := s.mimes.count()
	for i := range 100 {
		res, body := do(t, srv, "GET", "/api/artifacts/"+fake(i), "")
		want(t, res, body, 404)
	}
	if n := s.mimes.count() - base; n > 1 {
		t.Fatalf("100 unknown hashes caused %d rebuilds, want at most 1", n)
	}

	// A body written behind the daemon's back, as a hook would, gets its
	// MIME without waiting for the next tick.
	art, err := a.RegisterArtifact(m.ID, store.ArtifactInput{Content: []byte("a,b\n"), Title: "rows.csv"}, store.Op{Tool: "test"})
	if err != nil {
		t.Fatal(err)
	}
	res, body := do(t, srv, "GET", "/api/artifacts/"+strings.TrimPrefix(art.Hash, "sha256:"), "")
	want(t, res, body, 200)
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv MIME: %s", res.Header.Get("Content-Type"))
	}

	s.tick()
	base = s.mimes.count()
	for i := range 100 {
		do(t, srv, "GET", "/api/artifacts/"+fake(i), "")
	}
	if n := s.mimes.count() - base; n != 1 {
		t.Fatalf("after a tick: %d rebuilds, want 1", n)
	}
}

// Fable review U6 and C6: a state change bumps lastActivityAt and stamps
// the state; a status change bumps lastActivityAt only.
func TestPatchActivityAndStamp(t *testing.T) {
	a := openApp(t)
	srv := start(t, a, fstest.MapFS{})
	m := seed(t, srv)
	old := "2020-01-01T00:00:00Z"
	setOld := func() {
		if _, err := a.Update(m.ID, 0, func(m *manifest.Manifest) error { m.State.LastActivityAt = old; return nil }, store.Op{Tool: "test"}); err != nil {
			t.Fatal(err)
		}
	}

	setOld()
	res, body := do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"status":"paused"}`)
	want(t, res, body, 200)
	got, _ := a.Get(m.ID)
	if got.State.LastActivityAt == old {
		t.Fatal("status change did not bump lastActivityAt")
	}
	if _, ok := manifest.StateStampOf(got); ok {
		t.Fatal("status-only change stamped the state")
	}

	setOld()
	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"intent":{"goal":"Track spending"}}`)
	want(t, res, body, 200)
	if got, _ = a.Get(m.ID); got.State.LastActivityAt != old {
		t.Fatal("goal change bumped lastActivityAt")
	}

	res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, `{"state":{"nextStep":"Add CSV export"}}`)
	want(t, res, body, 200)
	got, _ = a.Get(m.ID)
	stamp, ok := manifest.StateStampOf(got)
	if got.State.LastActivityAt == old || !ok || stamp.RecordedBy != "coal-web" {
		t.Fatalf("state change: lastActivityAt %s stamp %+v %v", got.State.LastActivityAt, stamp, ok)
	}

	// Setting the same value again, or an empty list where there is none
	// (Fable review 2), changes nothing: no write, no new rev, no bump.
	setOld()
	before, _ := a.Get(m.ID)
	for _, patch := range []string{
		`{"state":{"nextStep":"Add CSV export"}}`,
		`{"state":{"openQuestions":[]}}`,
		`{"state":{"openQuestions":null,"summary":null}}`,
		`{"intent":{"goal":"Track spending"},"status":"paused"}`,
	} {
		res, body = do(t, srv, "PATCH", "/api/manifests/"+m.ID, patch)
		want(t, res, body, 200)
		got, _ = a.Get(m.ID)
		if got.Rev != before.Rev || got.State.LastActivityAt != old || res.Header.Get("ETag") != fmt.Sprintf(`"%d"`, before.Rev) {
			t.Fatalf("no-op PATCH %s: rev %d -> %d, lastActivityAt %s", patch, before.Rev, got.Rev, got.State.LastActivityAt)
		}
	}
}

func TestStateChanged(t *testing.T) {
	base := manifest.State{LastActivityAt: "2020-01-01T00:00:00Z", NextStep: "x"}
	same := base
	same.OpenQuestions, same.LastActivityAt = []string{}, "2026-01-01T00:00:00Z"
	if StateChanged(base, same) {
		t.Error("an empty openQuestions or a new lastActivityAt counted as a change")
	}
	diff := base
	diff.OpenQuestions = []string{"q"}
	if !StateChanged(base, diff) {
		t.Error("a new open question did not count as a change")
	}
}

// Fable review U8: a Host with no port means port 80.
func TestAllowedHostDefaultPort(t *testing.T) {
	check := func(host string, local net.Addr) bool {
		r := httptest.NewRequest("GET", "/health", nil)
		r.Host = host
		if local != nil {
			r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
		}
		return allowedHost(r)
	}
	at := func(port int) net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }
	for _, c := range []struct {
		host  string
		local net.Addr
		ok    bool
	}{
		{"127.0.0.1", at(80), true},
		{"localhost", at(80), true},
		{"127.0.0.1", at(4242), false},
		{"127.0.0.1:4242", at(4242), true},
		{"evil.example", at(80), false},
		{"127.0.0.1", nil, true},
	} {
		if got := check(c.host, c.local); got != c.ok {
			t.Errorf("Host %q on %v: allowed %v, want %v", c.host, c.local, got, c.ok)
		}
	}
}
