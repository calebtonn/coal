// Package manifest holds the Intent Manifest types, ids, timestamps and
// schema validation.
package manifest

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// SchemaURL is the $schema and $id of Intent Manifest v0.1.
	SchemaURL = "https://coalmine.io/schema/intent-manifest/0.1.json"
	// Version is the manifest format version.
	Version = "0.1"
)

// Status values.
const (
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusDone      = "done"
	StatusAbandoned = "abandoned"
)

// Intent source values.
const (
	SourceUser  = "user"
	SourceModel = "model"
)

// Manifest is one Intent Manifest. Unknown top-level fields are kept in
// Extra and written back out, so a rewrite never drops them.
type Manifest struct {
	Schema     string                     `json:"$schema"`
	Version    string                     `json:"version"`
	ID         string                     `json:"id"`
	Rev        int                        `json:"rev,omitempty"`
	Intent     Intent                     `json:"intent"`
	Status     string                     `json:"status"`
	Artifacts  []Artifact                 `json:"artifacts"`
	State      State                      `json:"state"`
	Provenance Provenance                 `json:"provenance"`
	Links      []Link                     `json:"links,omitempty"`
	Contexts   []Context                  `json:"contexts,omitempty"`
	Owner      *Owner                     `json:"owner,omitempty"`
	Privacy    *Privacy                   `json:"privacy,omitempty"`
	CreatedAt  string                     `json:"createdAt"`
	UpdatedAt  string                     `json:"updatedAt"`
	Ext        map[string]json.RawMessage `json:"ext,omitempty"`

	Extra map[string]json.RawMessage `json:"-"`
}

// Intent is what the person is trying to accomplish.
type Intent struct {
	Summary string   `json:"summary"`
	Goal    string   `json:"goal,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
}

// Artifact is something produced toward the intent. The top-level hash,
// size, createdAt and producer mirror the last entry of Versions.
type Artifact struct {
	Hash      string            `json:"hash"`
	Title     string            `json:"title"`
	Type      string            `json:"type"`
	MIME      string            `json:"mime"`
	URI       string            `json:"uri,omitempty"`
	Size      *int64            `json:"size,omitempty"`
	CreatedAt string            `json:"createdAt"`
	Producer  Provenance        `json:"producer"`
	Versions  []ArtifactVersion `json:"versions,omitempty"`
	Skipped   string            `json:"skipped,omitempty"`
}

// ArtifactVersion is one recorded body of an artifact.
type ArtifactVersion struct {
	Hash      string     `json:"hash"`
	Size      *int64     `json:"size,omitempty"`
	CreatedAt string     `json:"createdAt"`
	Producer  Provenance `json:"producer"`
	Skipped   string     `json:"skipped,omitempty"`
}

// State is where the work stands.
type State struct {
	LastActivityAt string   `json:"lastActivityAt"`
	NextStep       string   `json:"nextStep,omitempty"`
	OpenQuestions  []string `json:"openQuestions,omitempty"`
	Summary        string   `json:"summary,omitempty"`
}

// Provenance records which tool wrote something.
type Provenance struct {
	Tool       string `json:"tool"`
	Model      string `json:"model,omitempty"`
	SessionRef string `json:"sessionRef,omitempty"`
	CapturedBy string `json:"capturedBy,omitempty"`
}

// Link relates this manifest to another.
type Link struct {
	Rel string `json:"rel"`
	ID  string `json:"id"`
}

// Context is a place or moment where the intent is relevant.
type Context struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
}

// Owner identifies who the manifest belongs to.
type Owner struct {
	ID string `json:"id"`
}

// Privacy sets who may see the manifest.
type Privacy struct {
	Scope string `json:"scope"`
}

// NewID returns a lowercase UUIDv7.
func NewID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// Now returns the current time as RFC 3339 UTC with second precision.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// New returns an active manifest with fresh ids and timestamps, no
// artifacts, and rev 1.
func New(summary, source string, prov Provenance) *Manifest {
	now := Now()
	return &Manifest{
		Schema:     SchemaURL,
		Version:    Version,
		ID:         NewID(),
		Rev:        1,
		Intent:     Intent{Summary: summary, Source: source},
		Status:     StatusActive,
		Artifacts:  []Artifact{},
		State:      State{LastActivityAt: now},
		Provenance: prov,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Parse validates data against the schema and decodes it.
func Parse(data []byte) (*Manifest, error) {
	if err := Validate(data); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Marshal encodes the manifest as two-space-indented JSON with a
// trailing newline.
func (m *Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// fields is the manifest type without its JSON methods.
type fields Manifest

var knownKeys = sync.OnceValue(func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeFor[fields]()
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
})

// UnmarshalJSON decodes known fields and keeps the rest in Extra.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var f fields
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	f.Extra = nil
	for k, v := range all {
		if !knownKeys()[k] {
			if f.Extra == nil {
				f.Extra = map[string]json.RawMessage{}
			}
			f.Extra[k] = v
		}
	}
	*m = Manifest(f)
	return nil
}

// MarshalJSON encodes known fields, then any Extra fields in key order.
// A nil Artifacts slice is written as an empty array.
func (m Manifest) MarshalJSON() ([]byte, error) {
	f := fields(m)
	if f.Artifacts == nil {
		f.Artifacts = []Artifact{}
	}
	b, err := json.Marshal(f)
	if err != nil || len(m.Extra) == 0 {
		return b, err
	}
	keys := make([]string, 0, len(m.Extra))
	for k := range m.Extra {
		if !knownKeys()[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1])
	for _, k := range keys {
		kb, _ := json.Marshal(k)
		buf.WriteByte(',')
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(m.Extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
