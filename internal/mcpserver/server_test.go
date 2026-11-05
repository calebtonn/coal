package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

const page1 = "<!doctype html><title>Budget</title><h1>Household budget</h1>"

type fixture struct {
	t   *testing.T
	app *app.App
	cs  *mcp.ClientSession
	id  string
	uri string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	a, err := app.Open(filepath.Join(dir, "coal"), store.WithHistory(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	m := manifest.New("Household budget tracker as a single HTML page", manifest.SourceUser, manifest.Provenance{Tool: "test"})
	if err := a.Create(m, store.Op{Tool: "test"}); err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(dir, "budget.html")
	if err := os.WriteFile(html, []byte(page1), 0o600); err != nil {
		t.Fatal(err)
	}
	art, err := a.RegisterArtifact(m.ID, store.ArtifactInput{Path: html}, store.Op{Tool: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv, closeLog, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeLog() })
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return &fixture{t, a, cs, m.ID, art.URI}
}

func (f *fixture) call(name string, args map[string]any, out any) *mcp.CallToolResult {
	f.t.Helper()
	res, err := f.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
	if out != nil && res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			f.t.Fatal(err)
		}
	}
	return res
}

func resText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolsList(t *testing.T) {
	f := setup(t)
	res, err := f.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"coal_find": true, "coal_get": true, "coal_read_artifact": true, "coal_record_intent": true, "coal_register_artifact": true, "coal_update_state": true}
	if len(res.Tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(res.Tools), len(want))
	}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %s", tool.Name)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(strings.ToLower(tool.Description+string(schema)), "memory") {
			t.Errorf("%s mentions memory", tool.Name)
		}
		if tool.Name == "coal_update_state" {
			var s struct {
				Properties map[string]any `json:"properties"`
			}
			json.Unmarshal(schema, &s)
			if _, ok := s.Properties["summary"]; !ok {
				t.Error("coal_update_state should accept a state summary")
			}
			for k := range s.Properties {
				if strings.Contains(strings.ToLower(k), "intent") || k == "intentSummary" {
					t.Errorf("coal_update_state has property %q", k)
				}
			}
		}
	}
	if init := f.cs.InitializeResult(); init.ServerInfo.Name != "coal" || strings.Contains(strings.ToLower(init.Instructions), "memory") {
		t.Errorf("server info %+v", init.ServerInfo)
	}
}

func TestFind(t *testing.T) {
	f := setup(t)
	var out FindOutput
	res := f.call("coal_find", map[string]any{"query": "budgets"}, &out)
	if res.IsError || len(out.Results) != 1 || out.Results[0].ID != f.id {
		t.Fatalf("find budgets: %+v %s", out, resText(res))
	}
	if r := out.Results[0]; r.Source != "user" || r.ArtifactCount != 1 || strings.Contains(r.Snippet, "<b>") || len(r.Matched) == 0 {
		t.Errorf("result %+v", r)
	}
	if !strings.Contains(resText(res), f.id) {
		t.Errorf("text rendering lacks the id: %s", resText(res))
	}
	res = f.call("coal_find", map[string]any{"query": "zqxjkv"}, &out)
	if res.IsError || out.Results == nil || len(out.Results) != 0 {
		t.Fatalf("nonsense query: %+v %s", out, resText(res))
	}
	if !strings.Contains(resText(res), "phrasings") {
		t.Errorf("empty result should suggest rephrasing: %s", resText(res))
	}
	res = f.call("coal_find", map[string]any{"query": "budget", "status": "bogus"}, nil)
	if !res.IsError {
		t.Error("bad status should be a tool error")
	}
}

func TestReadArtifactVersions(t *testing.T) {
	f := setup(t)
	var out ReadOutput
	res := f.call("coal_read_artifact", map[string]any{"id": f.id[:8]}, &out)
	if res.IsError || out.Text != page1 || !strings.Contains(resText(res), page1) || out.VersionCount != 1 {
		t.Fatalf("read: %+v %s", out, resText(res))
	}
	page2 := page1 + "<p>CSV export</p>"
	var reg RegisterOutput
	res = f.call("coal_register_artifact", map[string]any{"manifestId": f.id, "content": page2, "uri": strings.Replace(f.uri, "file://", "FILE://", 1)}, &reg)
	if res.IsError || reg.VersionCount != 2 || reg.ArtifactIndex != 0 {
		t.Fatalf("register: %+v %s", reg, resText(res))
	}
	// Another spelling of the same uri reads the same artifact.
	other := strings.Replace(strings.Replace(f.uri, "file://", "FILE://", 1), "budget.html", "budget%2Ehtml", 1)
	f.call("coal_read_artifact", map[string]any{"id": f.id, "uri": other}, &out)
	if out.Text != page2 || out.Version != 1 {
		t.Errorf("latest: %+v", out)
	}
	f.call("coal_read_artifact", map[string]any{"id": f.id, "artifactIndex": 0, "version": 0}, &out)
	if out.Text != page1 || out.Version != 0 {
		t.Errorf("version 0: %+v", out)
	}
	if res := f.call("coal_read_artifact", map[string]any{"id": f.id, "version": 5}, nil); !res.IsError {
		t.Error("out-of-range version should be a tool error")
	}
	res = f.call("coal_register_artifact", map[string]any{"manifestId": f.id, "content": "x", "type": "document"}, nil)
	if !res.IsError || !strings.Contains(resText(res), "html, code, doc, data, image, other") {
		t.Errorf("bad type: %v %s", res.IsError, resText(res))
	}
	f.call("coal_register_artifact", map[string]any{"manifestId": f.id, "content": "notes", "uri": "coal:test/notes", "type": "doc"}, &reg)
	if reg.Artifact.Type != "doc" {
		t.Errorf("doc type: %+v", reg.Artifact)
	}
	if res := f.call("coal_register_artifact", map[string]any{"manifestId": f.id, "path": "relative.txt"}, nil); !res.IsError {
		t.Error("relative path should be a tool error")
	}
}

func TestUpdateStateRev(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.app.Store.Dir(), "manifests", f.id+".json")
	before, _ := os.ReadFile(path)
	m, _ := f.app.Get(f.id)
	res := f.call("coal_update_state", map[string]any{"id": f.id, "nextStep": "x", "rev": m.Rev - 1}, nil)
	if !res.IsError || !strings.Contains(resText(res), "current rev is "+itoa(m.Rev)) || !strings.Contains(resText(res), "coal_get") {
		t.Fatalf("stale rev: %v %s", res.IsError, resText(res))
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("stale update changed the file")
	}
	var got manifest.Manifest
	res = f.call("coal_update_state", map[string]any{"id": f.id, "nextStep": "Add CSV export", "summary": "Built the page", "goal": "Track spending"}, &got)
	if res.IsError || got.State.NextStep != "Add CSV export" || got.Intent.Summary != m.Intent.Summary || got.Intent.Goal != "Track spending" || got.State.Summary != "Built the page" {
		t.Fatalf("update: %+v %s", got, resText(res))
	}
	var out FindOutput
	f.call("coal_find", map[string]any{"query": "budget"}, &out)
	if len(out.Results) != 1 || out.Results[0].NextStep != "Add CSV export" {
		t.Errorf("find after update: %+v", out)
	}
	if st, ok := manifest.StateStampOf(&got); !ok || st.RecordedBy != "test-client" {
		t.Errorf("state stamp %+v %v", st, ok)
	}
	// A status-only change is not a recorded state: no new stamp.
	var rec RecordOutput
	f.call("coal_record_intent", map[string]any{"summary": "a second piece of work"}, &rec)
	f.call("coal_update_state", map[string]any{"id": rec.ID, "status": "paused"}, &got)
	if st, ok := manifest.StateStampOf(&got); ok || got.Status != "paused" {
		t.Errorf("status-only update stamped: %+v", st)
	}
	// The SDK rejects the unknown property before the handler runs.
	res = f.call("coal_update_state", map[string]any{"id": f.id, "intent": map[string]any{"summary": "hijack"}}, nil)
	if !res.IsError || !strings.Contains(resText(res), "unexpected additional properties") {
		t.Errorf("intent argument: %v %s", res.IsError, resText(res))
	}
	if after, _ := f.app.Get(f.id); after.Intent.Summary != m.Intent.Summary {
		t.Errorf("intent.summary changed: %q", after.Intent.Summary)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestRecordIntent(t *testing.T) {
	f := setup(t)
	var out RecordOutput
	f.call("coal_record_intent", map[string]any{"summary": "make a packing list for the trip", "summaryFromUser": true, "cwd": "/tmp/trip"}, &out)
	m, err := f.app.Get(out.ID)
	if err != nil || m.Intent.Source != "user" || m.Provenance.Tool != "test-client" || len(m.Contexts) != 1 {
		t.Fatalf("record: %+v %v", m, err)
	}
	f.call("coal_record_intent", map[string]any{"summary": "A packing list generator"}, &out)
	if out.Source != "model" {
		t.Errorf("default source %q", out.Source)
	}
}

func TestActiveResource(t *testing.T) {
	f := setup(t)
	res, err := f.cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: ActiveURI})
	if err != nil {
		t.Fatal(err)
	}
	var items []ActiveItem
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &items); err != nil || len(items) != 1 || items[0].ID != f.id || items[0].ArtifactCount != 1 {
		t.Fatalf("active: %v %s", err, res.Contents[0].Text)
	}
	if res.Contents[0].MIMEType != "application/json" {
		t.Errorf("mime %q", res.Contents[0].MIMEType)
	}
}

func TestFindMetrics(t *testing.T) {
	f := setup(t)
	for range 3 {
		f.call("coal_find", map[string]any{"query": "budget"}, nil)
	}
	file, err := os.Open(filepath.Join(f.app.Store.Dir(), "logs", "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	n := 0
	for sc := bufio.NewScanner(file); sc.Scan(); n++ {
		var line map[string]string
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line["event"] != "coal_find" || line["tool"] != "test-client" || line["at"] == "" {
			t.Errorf("line %q", sc.Text())
		}
	}
	if n != 3 {
		t.Errorf("got %d lines, want 3", n)
	}
}

func TestUpdateStateOnlyRealChanges(t *testing.T) {
	f := setup(t)
	var got manifest.Manifest
	f.call("coal_update_state", map[string]any{"id": f.id, "nextStep": "Add CSV export"}, &got)
	first, _ := manifest.StateStampOf(&got)
	rev := got.Rev
	path := filepath.Join(f.app.Store.Dir(), "manifests", f.id+".json")
	before, _ := os.ReadFile(path)

	// The same nextStep again, and an empty list where there is none:
	// nothing changes, so nothing is written.
	for _, args := range []map[string]any{
		{"id": f.id, "nextStep": "Add CSV export"},
		{"id": f.id, "nextStep": " Add CSV export ", "openQuestions": []string{}},
	} {
		res := f.call("coal_update_state", args, &got)
		if res.IsError || got.Rev != rev || got.State.NextStep != "Add CSV export" {
			t.Fatalf("no-op %v: rev %d %s", args, got.Rev, resText(res))
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Errorf("no-op %v rewrote the file", args)
		}
		if st, _ := manifest.StateStampOf(&got); st != first {
			t.Errorf("no-op restamped: %+v", st)
		}
	}

	// A goal-only change writes, but neither bumps activity nor stamps.
	var rec RecordOutput
	f.call("coal_record_intent", map[string]any{"summary": "a second piece of work"}, &rec)
	m, _ := f.app.Update(rec.ID, 0, func(m *manifest.Manifest) error {
		m.State.LastActivityAt = "2026-01-01T00:00:00Z" // so a bump would show
		return nil
	}, store.Op{Tool: "test"})
	f.call("coal_update_state", map[string]any{"id": rec.ID, "goal": "Done when it ships"}, &got)
	if got.Rev != m.Rev+1 || got.Intent.Goal != "Done when it ships" || got.State.LastActivityAt != m.State.LastActivityAt {
		t.Errorf("goal-only: rev %d->%d, activity %s->%s", m.Rev, got.Rev, m.State.LastActivityAt, got.State.LastActivityAt)
	}
	if _, ok := manifest.StateStampOf(&got); ok {
		t.Error("goal-only update stamped state")
	}

	// A status change alone bumps activity but does not stamp.
	f.call("coal_update_state", map[string]any{"id": rec.ID, "status": "paused"}, &got)
	if got.Status != "paused" || got.State.LastActivityAt == "2026-01-01T00:00:00Z" {
		t.Errorf("status-only: %s, activity %s", got.Status, got.State.LastActivityAt)
	}
	if _, ok := manifest.StateStampOf(&got); ok {
		t.Error("status-only update stamped state")
	}
}

func TestReadSkippedArtifact(t *testing.T) {
	f := setup(t)
	var reg RegisterOutput
	f.call("coal_register_artifact", map[string]any{"manifestId": f.id, "content": "TOKEN=x", "uri": "file:///p/.env"}, &reg)
	if reg.Artifact.Skipped == "" {
		t.Fatalf("expected a skipped artifact: %+v", reg.Artifact)
	}
	var out ReadOutput
	res := f.call("coal_read_artifact", map[string]any{"id": f.id, "artifactIndex": reg.ArtifactIndex}, &out)
	if txt := resText(res); res.IsError || out.Text != "" || !strings.Contains(txt, "size not recorded") || strings.Contains(txt, "0 bytes") {
		t.Errorf("skipped read: %s", txt)
	}
}
