package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

const notBuilt = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>COAL</title></head>
<body><p>The web UI is not built. Run make web, then make build.</p></body></html>
`

// ui serves the embedded build with a fallback to index.html for client
// routes, and a plain page when there is no build.
type ui struct {
	files fs.FS
	index []byte
	csp   string
}

var inlineScript = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)

func newUI(files fs.FS) *ui {
	u := &ui{files: files}
	if files != nil {
		u.index, _ = fs.ReadFile(files, "index.html")
	}
	if u.index != nil {
		u.csp = indexCSP(u.index)
	}
	return u
}

// indexCSP is the policy for index.html. Inline scripts, such as the
// theme init script the framework injects, are allowed by hash only.
// font-src allows data: because Vite inlines small font subsets from the
// framework's CSS as data: URLs; without it Chromium blocks them (seen in
// web/e2e). A data: font cannot run code. style-src stays strict: the
// bundle sets styles only through the CSSOM, which CSP does not block.
func indexCSP(html []byte) string {
	src := []string{"'self'"}
	for _, m := range inlineScript.FindAllSubmatch(html, -1) {
		if bytes.Contains(bytes.ToLower(m[1]), []byte("src=")) || len(m[2]) == 0 {
			continue
		}
		sum := sha256.Sum256(m[2])
		src = append(src, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return "default-src 'self'; script-src " + strings.Join(src, " ") +
		"; style-src 'self'; img-src 'self' data:; font-src 'self' data:; frame-src 'self'; object-src 'none'; base-uri 'none'"
}

func (u *ui) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	if u.index == nil {
		h.Set("Content-Security-Policy", "default-src 'none'")
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		w.Write([]byte(notBuilt))
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if body, err := fs.ReadFile(u.files, name); err == nil {
			if strings.HasPrefix(name, "assets/") {
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
			return
		}
		// Hashed build files that are gone stay gone; anything else is a
		// client route.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	h.Set("Content-Security-Policy", u.csp)
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(u.index))
}
