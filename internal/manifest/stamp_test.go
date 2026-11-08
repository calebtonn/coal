package manifest

import "testing"

func TestStampState(t *testing.T) {
	m := New("Household budget tracker", SourceUser, Provenance{Tool: "test"})
	if _, ok := StateStampOf(m); ok {
		t.Fatal("new manifest has a stamp")
	}
	StampState(m, "coal-mcp")
	s, ok := StateStampOf(m)
	if !ok || s.RecordedBy != "coal-mcp" || s.At == "" {
		t.Fatalf("stamp = %+v, %v", s, ok)
	}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(b); err != nil {
		t.Fatalf("stamped manifest does not validate: %v", err)
	}
}
