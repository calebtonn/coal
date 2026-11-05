// Package daemon is the local HTTP API and the embedded web UI. It binds
// 127.0.0.1 only and refuses requests for any other host name.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/calebtonn/coal/internal/app"
)

// Version is reported by GET /health.
const Version = "0.1.0-poc"

// Options configures New and Serve.
type Options struct {
	UI     fs.FS       // the built UI rooted at dist/; nil serves the "not built" page
	Logger *log.Logger // one line per request; Serve defaults it to logs/daemon.log
	Tick   func()      // called every TickEvery while serving, for the queue drain
	// TickEvery defaults to two seconds.
	TickEvery time.Duration
	// Ready is called with the listener address once Serve is listening.
	Ready func(net.Addr)
}

type server struct {
	app    *app.App
	opts   Options
	ui     *ui
	mimes  mimeCache
	logger *log.Logger
}

// New returns the daemon handler.
func New(a *app.App, opts Options) http.Handler {
	s := &server{app: a, opts: opts, ui: newUI(opts.UI), logger: opts.Logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/manifests", s.search)
	mux.HandleFunc("POST /api/manifests", s.create)
	mux.HandleFunc("GET /api/manifests/{id}", s.get)
	mux.HandleFunc("PATCH /api/manifests/{id}", s.patch)
	mux.HandleFunc("POST /api/manifests/{id}/artifacts", s.addArtifact)
	mux.HandleFunc("GET /api/artifacts/{hex}", s.artifact)
	mux.HandleFunc("POST /api/reindex", s.reindex)
	mux.HandleFunc("GET /api/schema", s.schema)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no endpoint for %s %s", r.Method, r.URL.Path))
	})
	mux.Handle("/", s.ui)
	return s.logRequests(s.guard(mux))
}

// Serve listens on addr, which must be 127.0.0.1:<port>, and serves until
// ctx is done. It logs to logs/daemon.log unless opts.Logger is set.
func Serve(ctx context.Context, a *app.App, addr string, opts Options) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("the daemon binds 127.0.0.1 only, not %q", host)
	}
	if opts.Logger == nil {
		f, err := openRotating(filepath.Join(a.Store.Dir(), "logs", "daemon.log"), 10<<20, 3)
		if err != nil {
			return err
		}
		defer f.Close()
		opts.Logger = log.New(f, "", log.LstdFlags|log.LUTC)
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: New(a, opts), ReadHeaderTimeout: 10 * time.Second, ErrorLog: opts.Logger}
	if opts.Ready != nil {
		opts.Ready(ln.Addr())
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	if opts.Tick != nil {
		every := opts.TickEvery
		if every <= 0 {
			every = 2 * time.Second
		}
		t := time.NewTicker(every)
		defer t.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					opts.Tick()
				}
			}
		}()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shut); err != nil {
		return err
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// guard rejects other host names (DNS rebinding) and cross-origin writes,
// and sets the headers every response carries.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		if !allowedHost(r) {
			reject(w, r, http.StatusForbidden, "host "+r.Host+" is not allowed; use 127.0.0.1 or localhost")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !safeMethod(r.Method) && origin != "http://"+r.Host {
			reject(w, r, http.StatusForbidden, "cross-origin request from "+origin+" refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedHost accepts 127.0.0.1:<port> and localhost:<port>, where port is
// the port the request arrived on when the server knows it.
func allowedHost(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		return false
	}
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, lport, err := net.SplitHostPort(la.String()); err == nil {
			return port == lport
		}
	}
	return true
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func reject(w http.ResponseWriter, r *http.Request, code int, msg string) {
	if isAPI(r.URL.Path) {
		writeError(w, code, msg)
		return
	}
	http.Error(w, msg, code)
}

func isAPI(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

// logRequests writes method, path, status and duration. The query string
// is left out so search terms stay out of the log.
func (s *server) logRequests(next http.Handler) http.Handler {
	if s.logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		s.logger.Printf("%s %s %d %s", r.Method, r.URL.Path, max(sw.status, 200), time.Since(start).Round(time.Microsecond))
	})
}
