package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

const (
	maxTextChars  = 200_000
	maxSniffBytes = 1 << 20
	capturedBy    = "coal-mcp@" + Version
)

var artifactTypes = []string{"html", "code", "doc", "data", "image", "other"}

var statuses = []string{manifest.StatusActive, manifest.StatusPaused, manifest.StatusDone, manifest.StatusAbandoned}

func (s *server) addTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_find", Description: "Search the user's record of work: things they built, wrote or were working on in any AI tool. " +
		"Call it whenever the user refers to earlier work (\"the budget thing\", \"that script from last week\"). " +
		"For current work, read the " + ActiveURI + " resource first. Full-text search with stemming; the last word also matches as a prefix. " +
		"If nothing comes back, try two or three other phrasings (synonyms, the artifact type such as \"html page\" or \"script\", the project or folder name) before concluding nothing exists. " +
		"Returns manifests best first, each with id, summary, source (user means the user's own words), status, lastActivityAt, artifactCount, nextStep, a snippet and the matched fields. " +
		"Use coal_get for the full manifest and coal_read_artifact for an artifact's content."}, s.find)
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_get", Description: "Get one full Intent Manifest by id (a unique prefix of 8 or more characters works): " +
		"intent, status, artifacts with versions, state (nextStep, openQuestions, summary), contexts and rev. " +
		"Read it before coal_update_state and pass its rev."}, s.get)
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_read_artifact", Description: "Read an artifact of a manifest. Pick it by artifactIndex (0-based position in artifacts[], default 0) or by uri. " +
		"Returns the latest version unless version is set (0-based index into that artifact's versions[], oldest first). " +
		"Text comes back as text, capped at 200,000 characters. Binary content comes back as a file:// path with MIME type and size. " +
		"Artifacts that were skipped (for example a possible secret) return the reason and no content."}, s.readArtifact)
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_record_intent", Description: "Start a new record of work when the user begins something new that coal_find does not already cover. " +
		"Pass the user's own words as summary (their request, trimmed) and set summaryFromUser to true; the user's words make the record findable later. " +
		"If you wrote the summary yourself, leave summaryFromUser false. Put your own description of the goal in goal. " +
		"Returns the new manifest id and rev. Register files with coal_register_artifact and record progress with coal_update_state."}, s.recordIntent)
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_register_artifact", Description: "Attach something the user made to a manifest: a file by absolute path, or inline content. " +
		"Registering the same path or uri again adds a new version instead of a second artifact. " +
		"The body is copied into the local COAL store; files that look like secrets are recorded without a copy. " +
		"Returns the artifact with its latest hash, its index and the number of versions."}, s.registerArtifact)
	mcp.AddTool(srv, &mcp.Tool{Name: "coal_update_state", Description: "Record where a piece of work stands. Call it when a piece of work finishes or pauses: " +
		"set nextStep to the next concrete action (\"Add CSV export\"), openQuestions to what is still undecided, and summary to a short account of what was done. " +
		"goal sets the goal; status is active, paused, done or abandoned. The user's own intent summary cannot be changed here. " +
		"Pass the rev from coal_get to avoid overwriting a change made elsewhere; on a conflict, re-read with coal_get and retry. Returns the updated manifest."}, s.updateState)
}

// FindInput is the coal_find input.
type FindInput struct {
	Query  string `json:"query" jsonschema:"words to search for; the user's own phrasing works best"`
	Status string `json:"status,omitempty" jsonschema:"only this status: active, paused, done or abandoned"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum results, default 20, at most 200"`
}

// FindResult is one coal_find hit.
type FindResult struct {
	ID             string   `json:"id"`
	Summary        string   `json:"summary"`
	Source         string   `json:"source"`
	Status         string   `json:"status"`
	LastActivityAt string   `json:"lastActivityAt"`
	ArtifactCount  int      `json:"artifactCount"`
	NextStep       string   `json:"nextStep,omitempty"`
	Snippet        string   `json:"snippet,omitempty"`
	Matched        []string `json:"matched"`
}

// FindOutput wraps the list so structured content is an object.
type FindOutput struct {
	Results []FindResult `json:"results"`
}

var stripB = strings.NewReplacer("<b>", "", "</b>", "")

func (s *server) find(_ context.Context, req *mcp.CallToolRequest, in FindInput) (*mcp.CallToolResult, FindOutput, error) {
	s.count(clientName(req))
	out := FindOutput{Results: []FindResult{}}
	if in.Status != "" && !slices.Contains(statuses, in.Status) {
		return nil, out, s.fail("coal_find", fmt.Errorf("status must be one of %s", strings.Join(statuses, ", ")))
	}
	rs, err := s.app.Search(in.Query, index.SearchOpts{Status: in.Status, Limit: in.Limit})
	if err != nil {
		return nil, out, s.fail("coal_find", err)
	}
	var b strings.Builder
	for i, r := range rs {
		f := FindResult{r.ID, r.Summary, r.Source, r.Status, r.LastActivityAt, r.ArtifactCount, r.NextStep, html.UnescapeString(stripB.Replace(r.Snippet)), r.Matched}
		if f.Matched == nil {
			f.Matched = []string{}
		}
		out.Results = append(out.Results, f)
		fmt.Fprintf(&b, "%d. %s [%s, %s] %s\n   %d artifact(s), last activity %s", i+1, f.ID, f.Status, f.Source, f.Summary, f.ArtifactCount, f.LastActivityAt)
		if f.NextStep != "" {
			fmt.Fprintf(&b, ", next step: %s", f.NextStep)
		}
		if f.Snippet != "" {
			fmt.Fprintf(&b, "\n   matched %s: %s", strings.Join(f.Matched, ", "), f.Snippet)
		}
		b.WriteString("\n")
	}
	if len(rs) == 0 {
		return text("No manifests match %q. Try two or three other phrasings (synonyms, the artifact type, the project or folder name) before concluding nothing exists.", in.Query), out, nil
	}
	return text("%d result(s) for %q:\n%s", len(rs), in.Query, b.String()), out, nil
}

// GetInput is the coal_get input.
type GetInput struct {
	ID string `json:"id" jsonschema:"manifest id, or a unique prefix of 8 or more characters"`
}

func (s *server) load(id string) (*manifest.Manifest, error) {
	full, err := s.app.Resolve(id)
	if err != nil {
		return nil, err
	}
	return s.app.Get(full)
}

func (s *server) get(_ context.Context, _ *mcp.CallToolRequest, in GetInput) (*mcp.CallToolResult, any, error) {
	m, err := s.load(in.ID)
	if err != nil {
		return nil, nil, s.fail("coal_get", err)
	}
	return nil, m, nil
}

// ReadInput is the coal_read_artifact input.
type ReadInput struct {
	ID            string `json:"id" jsonschema:"manifest id or unique prefix"`
	ArtifactIndex *int   `json:"artifactIndex,omitempty" jsonschema:"0-based index into artifacts[], default 0"`
	URI           string `json:"uri,omitempty" jsonschema:"pick the artifact by its uri instead of by index"`
	Version       *int   `json:"version,omitempty" jsonschema:"0-based index into the artifact's versions[], oldest first; default is the latest"`
}

// ReadOutput describes the artifact read.
type ReadOutput struct {
	ManifestID    string `json:"manifestId"`
	ArtifactIndex int    `json:"artifactIndex"`
	Title         string `json:"title"`
	URI           string `json:"uri,omitempty"`
	MIME          string `json:"mime"`
	Hash          string `json:"hash"`
	Size          int64  `json:"size"`
	Version       int    `json:"version"`
	VersionCount  int    `json:"versionCount"`
	Skipped       string `json:"skipped,omitempty"`
	Text          string `json:"text,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	Path          string `json:"path,omitempty"`
}

func isTextMIME(mime string) bool {
	mime, _, _ = strings.Cut(strings.ToLower(mime), ";")
	return strings.HasPrefix(mime, "text/") || mime == "application/json" || strings.HasSuffix(mime, "+json") ||
		strings.Contains(mime, "xml") || strings.Contains(mime, "javascript")
}

func (s *server) readArtifact(_ context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
	fail := func(err error) (*mcp.CallToolResult, any, error) { return nil, nil, s.fail("coal_read_artifact", err) }
	m, err := s.load(in.ID)
	if err != nil {
		return fail(err)
	}
	if len(m.Artifacts) == 0 {
		return fail(fmt.Errorf("manifest %s has no artifacts", m.ID))
	}
	idx := 0
	switch {
	case in.URI != "":
		want := store.CanonicalURI(in.URI)
		idx = slices.IndexFunc(m.Artifacts, func(a manifest.Artifact) bool { return a.URI != "" && store.CanonicalURI(a.URI) == want })
		if idx < 0 {
			return fail(fmt.Errorf("no artifact with uri %q on manifest %s; coal_get lists its artifacts", in.URI, m.ID))
		}
	case in.ArtifactIndex != nil:
		idx = *in.ArtifactIndex
		if idx < 0 || idx >= len(m.Artifacts) {
			return fail(fmt.Errorf("artifactIndex %d is out of range; manifest %s has %d artifact(s)", idx, m.ID, len(m.Artifacts)))
		}
	}
	a := m.Artifacts[idx]
	versions := a.Versions
	if len(versions) == 0 {
		versions = []manifest.ArtifactVersion{{Hash: a.Hash, Size: a.Size, CreatedAt: a.CreatedAt, Producer: a.Producer, Skipped: a.Skipped}}
	}
	vi := len(versions) - 1
	if in.Version != nil {
		if vi = *in.Version; vi < 0 || vi >= len(versions) {
			return fail(fmt.Errorf("version %d is out of range; artifact %d has %d version(s), numbered from 0", vi, idx, len(versions)))
		}
	}
	v := versions[vi]
	out := ReadOutput{ManifestID: m.ID, ArtifactIndex: idx, Title: a.Title, URI: a.URI, MIME: a.MIME, Hash: v.Hash,
		Version: vi, VersionCount: len(versions), Skipped: v.Skipped}
	if v.Size != nil {
		out.Size = *v.Size
	}
	size := fmt.Sprintf("%d bytes", out.Size)
	if v.Skipped != "" || v.Size == nil {
		size = "size not recorded"
	}
	head := fmt.Sprintf("%s (%s, %s, version %d of %d, %s)", a.Title, a.MIME, size, vi+1, len(versions), v.Hash)
	if v.Skipped != "" {
		return text("%s\nThe content was not stored: %s.", head, v.Skipped), out, nil
	}
	path := s.app.Store.ArtifactPath(v.Hash)
	if path == "" {
		return fail(fmt.Errorf("artifact %d has an invalid hash %q", idx, v.Hash))
	}
	textual := isTextMIME(a.MIME)
	if textual || out.Size <= maxSniffBytes {
		body, err := os.ReadFile(path)
		if err != nil {
			return fail(fmt.Errorf("reading stored body: %w", err))
		}
		if textual || utf8.Valid(body) {
			t := string(body)
			if utf8.RuneCountInString(t) > maxTextChars {
				t, out.Truncated = string([]rune(t)[:maxTextChars]), true
			}
			out.Text = t
			res := text("%s\n\n%s", head, t)
			if out.Truncated {
				res.Content = append(res.Content, &mcp.TextContent{Text: fmt.Sprintf("[Truncated at %d characters. The full body is at %s]", maxTextChars, fileURL(path))})
			}
			return res, out, nil
		}
	}
	out.Path = fileURL(path)
	return text("%s\nBinary content, stored at %s", head, out.Path), out, nil
}

func fileURL(p string) string { return (&url.URL{Scheme: "file", Path: p}).String() }

// RecordInput is the coal_record_intent input.
type RecordInput struct {
	Summary         string   `json:"summary" jsonschema:"what the user is trying to do, in the user's own words where possible"`
	SummaryFromUser bool     `json:"summaryFromUser,omitempty" jsonschema:"true only if summary is the user's own words"`
	Goal            string   `json:"goal,omitempty" jsonschema:"what done looks like, in your words"`
	Tags            []string `json:"tags,omitempty" jsonschema:"short tags"`
	Cwd             string   `json:"cwd,omitempty" jsonschema:"absolute working directory the work lives in, if any"`
	Tool            string   `json:"tool,omitempty" jsonschema:"the AI tool recording this; defaults to the MCP client name"`
	Model           string   `json:"model,omitempty" jsonschema:"model id, if known"`
	SessionRef      string   `json:"sessionRef,omitempty" jsonschema:"an opaque session or conversation id, never a token"`
}

// RecordOutput is the coal_record_intent result.
type RecordOutput struct {
	ID     string `json:"id"`
	Rev    int    `json:"rev"`
	Source string `json:"source"`
}

func (s *server) recordIntent(_ context.Context, req *mcp.CallToolRequest, in RecordInput) (*mcp.CallToolResult, RecordOutput, error) {
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		return nil, RecordOutput{}, s.fail("coal_record_intent", errors.New("summary is required"))
	}
	src := manifest.SourceModel
	if in.SummaryFromUser {
		src = manifest.SourceUser
	}
	tool := in.Tool
	if tool == "" {
		tool = clientName(req)
	}
	m := manifest.New(summary, src, manifest.Provenance{Tool: tool, Model: in.Model, SessionRef: in.SessionRef, CapturedBy: capturedBy})
	m.Intent.Goal, m.Intent.Tags = in.Goal, in.Tags
	if in.Cwd != "" {
		if !filepath.IsAbs(in.Cwd) {
			return nil, RecordOutput{}, s.fail("coal_record_intent", errors.New("cwd must be an absolute path"))
		}
		m.Contexts = []manifest.Context{{Kind: "path", Key: filepath.Clean(in.Cwd)}}
	}
	if err := s.app.Create(m, store.Op{Tool: tool, Action: "record intent"}); err != nil {
		return nil, RecordOutput{}, s.fail("coal_record_intent", err)
	}
	out := RecordOutput{m.ID, m.Rev, src}
	return text("Recorded manifest %s (rev %d, summary source %s).", m.ID, m.Rev, src), out, nil
}

// RegisterInput is the coal_register_artifact input.
type RegisterInput struct {
	ManifestID string  `json:"manifestId" jsonschema:"manifest id or unique prefix"`
	Path       string  `json:"path,omitempty" jsonschema:"absolute path to a regular file; set path or content"`
	Content    *string `json:"content,omitempty" jsonschema:"inline text content; set path or content"`
	Title      string  `json:"title,omitempty" jsonschema:"display title; defaults to the file name"`
	Type       string  `json:"type,omitempty" jsonschema:"html, code, doc, data, image or other; guessed when empty"`
	MIME       string  `json:"mime,omitempty" jsonschema:"MIME type; guessed when empty"`
	URI        string  `json:"uri,omitempty" jsonschema:"stable identity for inline content; registering the same uri again adds a version"`
}

// RegisterOutput is the coal_register_artifact result.
type RegisterOutput struct {
	ManifestID    string            `json:"manifestId"`
	ArtifactIndex int               `json:"artifactIndex"`
	Artifact      manifest.Artifact `json:"artifact"`
	LatestHash    string            `json:"latestHash"`
	VersionCount  int               `json:"versionCount"`
}

func (s *server) registerArtifact(_ context.Context, req *mcp.CallToolRequest, in RegisterInput) (*mcp.CallToolResult, any, error) {
	fail := func(err error) (*mcp.CallToolResult, any, error) {
		return nil, nil, s.fail("coal_register_artifact", err)
	}
	if (in.Path == "") == (in.Content == nil) {
		return fail(errors.New("set exactly one of path or content"))
	}
	if in.Type != "" && !slices.Contains(artifactTypes, in.Type) {
		return fail(fmt.Errorf("type %q is not allowed; use one of %s, or leave it empty to guess", in.Type, strings.Join(artifactTypes, ", ")))
	}
	id, err := s.app.Resolve(in.ManifestID)
	if err != nil {
		return fail(err)
	}
	uri := in.URI
	if uri != "" {
		uri = store.CanonicalURI(uri)
	}
	ai := store.ArtifactInput{Path: in.Path, URI: uri, Title: in.Title, Type: in.Type, MIME: in.MIME,
		Producer: manifest.Provenance{Tool: clientName(req), CapturedBy: capturedBy}}
	if in.Path != "" {
		if !filepath.IsAbs(in.Path) {
			return fail(errors.New("path must be absolute"))
		}
		if fi, err := os.Stat(in.Path); err != nil {
			return fail(err)
		} else if !fi.Mode().IsRegular() {
			return fail(fmt.Errorf("%s is not a regular file", in.Path))
		}
	} else {
		ai.Content = []byte(*in.Content)
	}
	a, err := s.app.RegisterArtifact(id, ai, store.Op{Tool: ai.Producer.Tool})
	if err != nil {
		return fail(err)
	}
	out := RegisterOutput{ManifestID: id, ArtifactIndex: -1, Artifact: *a, LatestHash: a.Hash, VersionCount: max(1, len(a.Versions))}
	if m, err := s.app.Get(id); err == nil {
		out.ArtifactIndex = slices.IndexFunc(m.Artifacts, func(x manifest.Artifact) bool {
			return x.Hash == a.Hash && x.URI == a.URI
		})
	}
	msg := fmt.Sprintf("Registered %q on %s as artifact %d, version %d, %s.", a.Title, id, out.ArtifactIndex, out.VersionCount, a.Hash)
	if a.Skipped != "" {
		msg += " The content was not stored: " + a.Skipped + "."
	}
	return text("%s", msg), out, nil
}

// UpdateInput is the coal_update_state input. There is deliberately no
// field for intent.summary: that stays in the user's words.
type UpdateInput struct {
	ID            string   `json:"id" jsonschema:"manifest id or unique prefix"`
	NextStep      *string  `json:"nextStep,omitempty" jsonschema:"the next concrete action; empty string clears it"`
	OpenQuestions []string `json:"openQuestions,omitempty" jsonschema:"what is still undecided; replaces the list, an empty list clears it"`
	Summary       *string  `json:"summary,omitempty" jsonschema:"short account of what was done so far (state.summary)"`
	Goal          *string  `json:"goal,omitempty" jsonschema:"what done looks like (intent.goal)"`
	Status        string   `json:"status,omitempty" jsonschema:"active, paused, done or abandoned"`
	Rev           *int     `json:"rev,omitempty" jsonschema:"the rev you read with coal_get; the update fails if the manifest changed since"`
}

// stateChanged is the daemon's rule (daemon.StateChanged), mirrored so
// this package does not import the daemon: nextStep, summary or
// openQuestions differ, with nil and [] equal. lastActivityAt and Extra
// are ignored.
func stateChanged(before, after manifest.State) bool {
	return before.NextStep != after.NextStep || before.Summary != after.Summary ||
		!slices.Equal(before.OpenQuestions, after.OpenQuestions)
}

// stampTool names the writer in the state stamp: the client name, or
// coal-mcp when the client sent none.
func stampTool(req *mcp.CallToolRequest) string {
	if n := clientName(req); n != "mcp" {
		return n
	}
	return "coal-mcp"
}

func (s *server) updateState(_ context.Context, req *mcp.CallToolRequest, in UpdateInput) (*mcp.CallToolResult, any, error) {
	fail := func(err error) (*mcp.CallToolResult, any, error) { return nil, nil, s.fail("coal_update_state", err) }
	if in.Status != "" && !slices.Contains(statuses, in.Status) {
		return fail(fmt.Errorf("status must be one of %s", strings.Join(statuses, ", ")))
	}
	if in.NextStep == nil && in.OpenQuestions == nil && in.Summary == nil && in.Goal == nil && in.Status == "" {
		return fail(errors.New("nothing to update: set nextStep, openQuestions, summary, goal or status"))
	}
	rev := 0
	if in.Rev != nil {
		if rev = *in.Rev; rev < 1 {
			return fail(errors.New("rev must be 1 or more"))
		}
	}
	id, err := s.app.Resolve(in.ID)
	if err != nil {
		return fail(err)
	}
	m, err := s.app.Update(id, rev, func(m *manifest.Manifest) error {
		status, goal, state := m.Status, m.Intent.Goal, m.State
		state.OpenQuestions = slices.Clone(state.OpenQuestions)
		if in.NextStep != nil {
			m.State.NextStep = strings.TrimSpace(*in.NextStep)
		}
		if in.OpenQuestions != nil {
			// An empty list clears the field rather than storing [].
			m.State.OpenQuestions = in.OpenQuestions
			if len(m.State.OpenQuestions) == 0 {
				m.State.OpenQuestions = nil
			}
		}
		if in.Summary != nil {
			m.State.Summary = strings.TrimSpace(*in.Summary)
		}
		if in.Goal != nil {
			m.Intent.Goal = strings.TrimSpace(*in.Goal)
		}
		if in.Status != "" {
			m.Status = in.Status
		}
		// The same rule as PATCH: only a real change counts. Capture
		// reads the stamp to skip its session-end fallback, so a no-op
		// call must not leave one.
		changed := stateChanged(state, m.State)
		if !changed && status == m.Status && goal == m.Intent.Goal {
			return store.ErrNoChange
		}
		if changed || status != m.Status {
			m.State.LastActivityAt = manifest.Now()
		}
		if changed {
			manifest.StampState(m, stampTool(req))
		}
		return nil
	}, store.Op{Tool: clientName(req), Action: "update state"})
	var ce *store.ConflictError
	if errors.As(err, &ce) {
		s.log.Info("rev conflict", "id", id, "expected", ce.Expected, "current", ce.Current)
		msg := fmt.Sprintf("Conflict: manifest %s changed since it was read (you passed rev %d, the current rev is %d). Nothing was written. Re-read it with coal_get, then retry coal_update_state with rev %d.", id, ce.Expected, ce.Current, ce.Current)
		if cur, err := s.app.Get(id); err == nil {
			st, _ := json.Marshal(cur.State)
			msg += fmt.Sprintf("\nCurrent status: %s. Current state: %s", cur.Status, st)
		}
		res := text("%s", msg)
		res.IsError = true
		return res, nil, nil
	}
	if err != nil {
		return fail(err)
	}
	return nil, m, nil
}
