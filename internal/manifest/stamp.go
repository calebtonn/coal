package manifest

import "encoding/json"

// StateExtKey is the ext namespace that records who last wrote state on
// purpose, through coal_update_state or a PATCH. Capture reads it to tell
// a model-recorded next step from one it extracted itself.
const StateExtKey = "io.coalmine.state"

// StateStamp is the value stored under ext[StateExtKey].
type StateStamp struct {
	RecordedBy string `json:"recordedBy"`
	At         string `json:"at"`
}

// StampState records that tool wrote the manifest's state just now.
func StampState(m *Manifest, tool string) {
	b, _ := json.Marshal(StateStamp{RecordedBy: tool, At: Now()})
	if m.Ext == nil {
		m.Ext = map[string]json.RawMessage{}
	}
	m.Ext[StateExtKey] = b
}

// StateStampOf returns the stamp, if the manifest has one.
func StateStampOf(m *Manifest) (StateStamp, bool) {
	var s StateStamp
	raw, ok := m.Ext[StateExtKey]
	if !ok || json.Unmarshal(raw, &s) != nil || s.At == "" {
		return StateStamp{}, false
	}
	return s, true
}
