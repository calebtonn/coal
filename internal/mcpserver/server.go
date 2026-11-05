// Package mcpserver exposes the COAL record of work to AI tools over MCP:
// six tools and the coal://manifests/active resource.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/manifest"
)

// Version is the server version reported in initialize.
const Version = "0.1.0-poc"

// ActiveURI is the resource listing current work.
const ActiveURI = "coal://manifests/active"

const instructions = "COAL is the user's record of work across AI tools: what they set out to do " +
	"(intents), what they made (artifacts), and what is still open (next steps and questions). " +
	"Call coal_find whenever the user refers to something they built, wrote or were working on, " +
	"and read " + ActiveURI + " for current work. When a piece of work finishes, call " +
	"coal_update_state with the next step so the next session can pick it up."

type server struct {
	app     *app.App
	logs    string
	log     *slog.Logger
	metrics sync.Mutex
}

// New builds the MCP server. Logs go to logs/mcp.log in the data dir; the
// returned closer closes that file.
func New(a *app.App) (*mcp.Server, func() error, error) {
	logs := filepath.Join(a.Store.Dir(), "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(logs, "mcp.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	s := &server{app: a, logs: logs, log: slog.New(slog.NewTextHandler(f, nil))}
	srv := mcp.NewServer(&mcp.Implementation{Name: "coal", Version: Version}, &mcp.ServerOptions{
		Instructions: instructions,
		Logger:       s.log,
		Capabilities: &mcp.ServerCapabilities{},
	})
	s.addTools(srv)
	srv.AddResource(&mcp.Resource{
		URI:         ActiveURI,
		Name:        "active-manifests",
		Title:       "Active work",
		Description: "Active Intent Manifests, newest activity first (up to 50): id, summary, next step, last activity and artifact count. Read this first to see what the user is working on now.",
		MIMEType:    "application/json",
	}, s.readActive)
	return srv, f.Close, nil
}

// ActiveItem is one entry of the active resource.
type ActiveItem struct {
	ID             string `json:"id"`
	Summary        string `json:"summary"`
	NextStep       string `json:"nextStep,omitempty"`
	LastActivityAt string `json:"lastActivityAt"`
	ArtifactCount  int    `json:"artifactCount"`
}

func (s *server) readActive(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	rs, err := s.app.Search("", index.SearchOpts{Status: manifest.StatusActive, Limit: 50})
	if err != nil {
		s.log.Error("read active", "err", err)
		return nil, err
	}
	items := make([]ActiveItem, 0, len(rs))
	for _, r := range rs {
		items = append(items, ActiveItem{r.ID, r.Summary, r.NextStep, r.LastActivityAt, r.ArtifactCount})
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: ActiveURI, MIMEType: "application/json", Text: string(b)}}}, nil
}

// clientName is the MCP client's name from initialize, or "mcp".
func clientName(req *mcp.CallToolRequest) string {
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			return p.ClientInfo.Name
		}
	}
	return "mcp"
}

// count appends one line to logs/metrics.jsonl (#12). It never leaves the
// machine. Failures are logged and ignored.
func (s *server) count(event, tool string) {
	line, _ := json.Marshal(map[string]string{"at": manifest.Now(), "event": event, "tool": tool})
	s.metrics.Lock()
	defer s.metrics.Unlock()
	f, err := os.OpenFile(filepath.Join(s.logs, "metrics.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
		f.Close()
	}
	if err != nil {
		s.log.Warn("metrics", "err", err)
	}
}

// fail logs a tool error and returns it for the SDK to wrap as isError.
func (s *server) fail(tool string, err error) error {
	s.log.Warn("tool error", "tool", tool, "err", err)
	return err
}

func text(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}}}
}
