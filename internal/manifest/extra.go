package manifest

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Every manifest object keeps the keys it does not know in an Extra
// field and writes them back, so a reader on an older version never
// drops a newer writer's data on rewrite. Each type converts itself to a
// method-free alias and calls decodeExtra and encodeExtra.

var knownKeyCache sync.Map // reflect.Type -> map[string]bool

func knownKeysOf(t reflect.Type) map[string]bool {
	if k, ok := knownKeyCache.Load(t); ok {
		return k.(map[string]bool)
	}
	keys := map[string]bool{}
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	knownKeyCache.Store(t, keys)
	return keys
}

// decodeExtra decodes data into fields (a pointer to a method-free alias)
// and returns the unknown keys, or nil when there are none. The target is
// zeroed first.
func decodeExtra(data []byte, fields any) (map[string]json.RawMessage, error) {
	reflect.ValueOf(fields).Elem().SetZero() // decode replaces, it does not merge
	if err := json.Unmarshal(data, fields); err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	known := knownKeysOf(reflect.TypeOf(fields).Elem())
	var extra map[string]json.RawMessage
	for k, v := range all {
		if !known[k] {
			if extra == nil {
				extra = map[string]json.RawMessage{}
			}
			extra[k] = v
		}
	}
	return extra, nil
}

// encodeExtra encodes fields (a method-free alias value), then appends
// the unknown keys from extra in key order.
func encodeExtra(fields any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(fields)
	if err != nil || len(extra) == 0 {
		return b, err
	}
	known := knownKeysOf(reflect.TypeOf(fields))
	keys := make([]string, 0, len(extra))
	for k := range extra {
		if !known[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return b, nil
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1])
	sep := len(b) > 2 // "{}" has no field to follow
	for _, k := range keys {
		if sep {
			buf.WriteByte(',')
		}
		sep = true
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type (
	intentFields          Intent
	artifactFields        Artifact
	artifactVersionFields ArtifactVersion
	stateFields           State
	provenanceFields      Provenance
	linkFields            Link
	contextFields         Context
	ownerFields           Owner
	privacyFields         Privacy
)

func (x *Intent) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*intentFields)(x))
	return
}
func (x Intent) MarshalJSON() ([]byte, error) { return encodeExtra(intentFields(x), x.Extra) }

func (x *Artifact) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*artifactFields)(x))
	return
}
func (x Artifact) MarshalJSON() ([]byte, error) { return encodeExtra(artifactFields(x), x.Extra) }

func (x *ArtifactVersion) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*artifactVersionFields)(x))
	return
}
func (x ArtifactVersion) MarshalJSON() ([]byte, error) {
	return encodeExtra(artifactVersionFields(x), x.Extra)
}

func (x *State) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*stateFields)(x))
	return
}
func (x State) MarshalJSON() ([]byte, error) { return encodeExtra(stateFields(x), x.Extra) }

func (x *Provenance) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*provenanceFields)(x))
	return
}
func (x Provenance) MarshalJSON() ([]byte, error) { return encodeExtra(provenanceFields(x), x.Extra) }

func (x *Link) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*linkFields)(x))
	return
}
func (x Link) MarshalJSON() ([]byte, error) { return encodeExtra(linkFields(x), x.Extra) }

func (x *Context) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*contextFields)(x))
	return
}
func (x Context) MarshalJSON() ([]byte, error) { return encodeExtra(contextFields(x), x.Extra) }

func (x *Owner) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*ownerFields)(x))
	return
}
func (x Owner) MarshalJSON() ([]byte, error) { return encodeExtra(ownerFields(x), x.Extra) }

func (x *Privacy) UnmarshalJSON(b []byte) (err error) {
	x.Extra, err = decodeExtra(b, (*privacyFields)(x))
	return
}
func (x Privacy) MarshalJSON() ([]byte, error) { return encodeExtra(privacyFields(x), x.Extra) }
