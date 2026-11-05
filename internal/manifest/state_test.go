package manifest

import (
	"encoding/json"
	"testing"
)

func TestStateChanged(t *testing.T) {
	base := State{LastActivityAt: "2020-01-01T00:00:00Z", NextStep: "n", Summary: "s"}
	cases := []struct {
		name  string
		after State
		want  bool
	}{
		{"same", base, false},
		{"empty list and new timestamp", State{LastActivityAt: "2026-01-01T00:00:00Z", NextStep: "n", Summary: "s", OpenQuestions: []string{}}, false},
		{"extra ignored", State{NextStep: "n", Summary: "s", Extra: map[string]json.RawMessage{"x": json.RawMessage("1")}}, false},
		{"next step", State{NextStep: "m", Summary: "s"}, true},
		{"summary", State{NextStep: "n", Summary: "t"}, true},
		{"open question added", State{NextStep: "n", Summary: "s", OpenQuestions: []string{"q"}}, true},
	}
	for _, c := range cases {
		if got := StateChanged(base, c.after); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	withQ := State{OpenQuestions: []string{"a", "b"}}
	if !StateChanged(withQ, State{OpenQuestions: []string{"b", "a"}}) || !StateChanged(withQ, State{}) {
		t.Error("reordered or cleared open questions did not count as a change")
	}
}
