package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
	"github.com/calebtonn/coal/spec"
)

const (
	producer        = "coal-web"
	maxJSONBody     = 1 << 20
	maxArtifactBody = 64 << 20
)

var (
	statuses = []string{manifest.StatusActive, manifest.StatusPaused, manifest.StatusDone, manifest.StatusAbandoned}
	types    = []string{"html", "code", "doc", "data", "image", "other"}
	hexHash  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// httpError carries a status code out of a store callback.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return &httpError{http.StatusBadRequest, fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// fail maps an error to a status code and a JSON error body.
func (s *server) fail(w http.ResponseWriter, err error) {
	var he *httpError
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &he):
		writeError(w, he.code, he.msg)
	case errors.As(err, &mbe):
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		if s.logger != nil {
			s.logger.Printf("error: %v", err)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// decode reads one JSON object into v, refusing unknown fields.
func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		return badRequest("invalid JSON body: %v", err)
	}
	if dec.More() {
		return badRequest("invalid JSON body: more than one value")
	}
	return nil
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
}

// result is index.Result with empty snippet and nextStep as null.
type result struct {
	index.Result
	NextStep *string `json:"nextStep"`
	Snippet  *string `json:"snippet"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := index.SearchOpts{Status: q.Get("status")}
	if opts.Status != "" && !slices.Contains(statuses, opts.Status) {
		s.fail(w, badRequest("status must be one of %s", strings.Join(statuses, ", ")))
		return
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			s.fail(w, badRequest("limit must be a positive integer"))
			return
		}
		opts.Limit = n
	}
	hits, err := s.app.Search(q.Get("q"), opts)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]result, len(hits))
	for i, h := range hits {
		out[i] = result{Result: h, NextStep: nullable(h.NextStep), Snippet: nullable(h.Snippet)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// resolve turns the {id} path value, a full id or a unique 8+ character
// prefix, into an id.
func (s *server) resolve(r *http.Request) (string, error) {
	id, err := s.app.Resolve(r.PathValue("id"))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", badRequest("%v", err)
	}
	return id, err
}

func writeManifest(w http.ResponseWriter, code int, m *manifest.Manifest) {
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, m.Rev))
	writeJSON(w, code, m)
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolve(r)
	if err == nil {
		var m *manifest.Manifest
		if m, err = s.app.Get(id); err == nil {
			writeManifest(w, http.StatusOK, m)
			return
		}
	}
	s.fail(w, err)
}

type createRequest struct {
	Summary    string               `json:"summary"`
	Goal       string               `json:"goal"`
	Tags       []string             `json:"tags"`
	Source     string               `json:"source"`
	Provenance *manifest.Provenance `json:"provenance"`
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(w, r, maxJSONBody, &req); err != nil {
		s.fail(w, err)
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if req.Summary == "" {
		s.fail(w, badRequest("summary is required"))
		return
	}
	if req.Source == "" {
		req.Source = manifest.SourceUser
	}
	if req.Source != manifest.SourceUser && req.Source != manifest.SourceModel {
		s.fail(w, badRequest("source must be user or model"))
		return
	}
	prov := manifest.Provenance{Tool: producer}
	if req.Provenance != nil {
		prov = *req.Provenance
		if prov.Tool == "" {
			prov.Tool = producer
		}
	}
	m := manifest.New(req.Summary, req.Source, prov)
	m.Intent.Goal, m.Intent.Tags = req.Goal, req.Tags
	if err := validate(m); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.app.Create(m, store.Op{Tool: producer, Action: "create manifest"}); err != nil {
		s.fail(w, err)
		return
	}
	created, err := s.app.Get(m.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/manifests/"+created.ID)
	writeManifest(w, http.StatusCreated, created)
}

// validate checks m against the schema so a bad value is a 400, not a
// store failure.
func validate(m *manifest.Manifest) error {
	data, err := m.Marshal()
	if err == nil {
		err = manifest.Validate(data)
	}
	if err != nil {
		return badRequest("the result would not be a valid manifest: %v", err)
	}
	return nil
}

// parseIfMatch returns the rev in If-Match: "<rev>", or 0 when absent or *.
func parseIfMatch(h string) (int, error) {
	h = strings.TrimSpace(h)
	if h == "" || h == "*" {
		return 0, nil
	}
	rev, err := strconv.Atoi(strings.Trim(strings.TrimPrefix(h, "W/"), `"`))
	if err != nil || rev < 1 {
		return 0, badRequest(`If-Match must be "<rev>", got %s`, h)
	}
	return rev, nil
}

func (s *server) patch(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolve(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	rev, err := parseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		s.fail(w, err)
		return
	}
	var p map[string]json.RawMessage
	if err := decode(w, r, maxJSONBody, &p); err != nil {
		s.fail(w, err)
		return
	}
	if err := checkPatch(p); err != nil {
		s.fail(w, err)
		return
	}
	m, err := s.app.Update(id, rev, func(m *manifest.Manifest) error {
		if len(p) == 0 {
			return store.ErrNoChange
		}
		if err := applyPatch(m, p); err != nil {
			return err
		}
		return validate(m)
	}, store.Op{Tool: producer, Action: "edit manifest"})
	var ce *store.ConflictError
	if errors.As(err, &ce) {
		if cur, gerr := s.app.Get(id); gerr == nil {
			writeManifest(w, http.StatusPreconditionFailed, cur)
			return
		}
		err = &httpError{http.StatusPreconditionFailed, ce.Error()}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeManifest(w, http.StatusOK, m)
}

// patchable lists the fields PATCH may change. intent.summary is not one:
// renaming is a separate, deliberate command.
var patchable = map[string][]string{
	"status": nil,
	"intent": {"goal", "tags"},
	"state":  {"nextStep", "openQuestions", "summary"},
}

func checkPatch(p map[string]json.RawMessage) error {
	for k, raw := range p {
		sub, ok := patchable[k]
		if !ok {
			return badRequest("field %s cannot be changed here; allowed: status, intent.goal, intent.tags, state.nextStep, state.openQuestions, state.summary", k)
		}
		if sub == nil {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
			return badRequest("%s must be an object", k)
		}
		for f := range obj {
			if !slices.Contains(sub, f) {
				return badRequest("field %s.%s cannot be changed here", k, f)
			}
		}
	}
	return nil
}

// applyPatch applies a JSON merge patch (RFC 7396) of the patchable
// fields; null removes an optional field.
func applyPatch(m *manifest.Manifest, p map[string]json.RawMessage) error {
	if raw, ok := p["status"]; ok {
		if err := json.Unmarshal(raw, &m.Status); err != nil || string(raw) == "null" || !slices.Contains(statuses, m.Status) {
			return badRequest("status must be one of %s", strings.Join(statuses, ", "))
		}
	}
	targets := map[string]any{
		"intent.goal": &m.Intent.Goal, "intent.tags": &m.Intent.Tags,
		"state.nextStep": &m.State.NextStep, "state.openQuestions": &m.State.OpenQuestions, "state.summary": &m.State.Summary,
	}
	for _, k := range []string{"intent", "state"} {
		var obj map[string]json.RawMessage
		if raw, ok := p[k]; !ok || json.Unmarshal(raw, &obj) != nil {
			continue
		}
		for f, raw := range obj {
			dst := targets[k+"."+f]
			if string(raw) == "null" {
				switch d := dst.(type) {
				case *string:
					*d = ""
				case *[]string:
					*d = nil
				}
				continue
			}
			if err := json.Unmarshal(raw, dst); err != nil {
				return badRequest("%s.%s: %v", k, f, err)
			}
		}
	}
	return nil
}

type artifactRequest struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
	Title   string  `json:"title"`
	Type    string  `json:"type"`
	MIME    string  `json:"mime"`
}

func (s *server) addArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := s.resolve(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	var req artifactRequest
	if err := decode(w, r, maxArtifactBody, &req); err != nil {
		s.fail(w, err)
		return
	}
	in := store.ArtifactInput{Title: req.Title, Type: req.Type, MIME: req.MIME, Producer: manifest.Provenance{Tool: producer}}
	switch {
	case (req.Path == "") == (req.Content == nil):
		err = badRequest("give either path or content")
	case req.Type != "" && !slices.Contains(types, req.Type):
		err = badRequest("type must be one of %s", strings.Join(types, ", "))
	case req.Path != "":
		in.Path = req.Path
		if !filepath.IsAbs(req.Path) {
			err = badRequest("path must be absolute")
		} else if st, serr := os.Stat(req.Path); serr != nil || !st.Mode().IsRegular() {
			err = badRequest("path %s is not a regular file", req.Path)
		}
	case strings.TrimSpace(req.Title) == "":
		err = badRequest("title is required with content")
	default:
		in.Content = []byte(*req.Content)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	a, err := s.app.RegisterArtifact(id, in, store.Op{Tool: producer})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mimes.reset()
	writeJSON(w, http.StatusCreated, a)
}

// mimeCache maps a body hash to the MIME recorded for it, and to the
// skipped reason for hashes that only appear without a body. It is
// rebuilt from the manifests when a lookup misses.
type mimeCache struct {
	mu      sync.Mutex
	mime    map[string]string
	skipped map[string]string
}

func (c *mimeCache) reset() {
	c.mu.Lock()
	c.mime, c.skipped = nil, nil
	c.mu.Unlock()
}

func (c *mimeCache) lookup(a *app.App, hash string) (mime, skipped string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.mime[hash]; !ok {
		if _, ok := c.skipped[hash]; !ok {
			c.rebuild(a)
		}
	}
	return c.mime[hash], c.skipped[hash]
}

func (c *mimeCache) rebuild(a *app.App) {
	c.mime, c.skipped = map[string]string{}, map[string]string{}
	ms, _ := a.Store.List()
	for _, m := range ms {
		for _, art := range m.Artifacts {
			vs := art.Versions
			if len(vs) == 0 {
				vs = []manifest.ArtifactVersion{{Hash: art.Hash, Skipped: art.Skipped}}
			}
			for _, v := range vs {
				if v.Skipped != "" {
					c.skipped[v.Hash] = v.Skipped
				} else if art.MIME != "" {
					c.mime[v.Hash] = art.MIME
				}
			}
		}
	}
}

func (s *server) artifact(w http.ResponseWriter, r *http.Request) {
	hex := r.PathValue("hex")
	if !hexHash.MatchString(hex) {
		writeError(w, http.StatusBadRequest, "artifact hash must be 64 lowercase hex characters")
		return
	}
	mime, skipped := s.mimes.lookup(s.app, "sha256:"+hex)
	f, err := os.Open(s.app.Store.ArtifactPath(hex))
	if err != nil {
		if skipped != "" {
			writeError(w, http.StatusNotFound, "this artifact has no stored body: "+skipped)
		} else {
			writeError(w, http.StatusNotFound, "no artifact body with hash "+hex)
		}
		return
	}
	defer f.Close()
	if mime == "" {
		mime = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+hex+`"`)
	st, err := f.Stat()
	if err != nil {
		s.fail(w, err)
		return
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (s *server) reindex(w http.ResponseWriter, r *http.Request) {
	n, skipped, err := s.app.Reindex()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mimes.reset()
	writeJSON(w, http.StatusOK, map[string]any{"indexed": n, "skipped": skipped})
}

func (s *server) schema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Write(spec.IntentManifestSchema)
}
