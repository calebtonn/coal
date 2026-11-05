package paths

import (
	"path/filepath"
	"testing"
)

func TestHomeOrder(t *testing.T) {
	t.Setenv(EnvHome, "/tmp/coal-env")
	got, err := Home("")
	if err != nil || got != "/tmp/coal-env" {
		t.Fatalf("env: got %q, %v", got, err)
	}
	got, err = Home("/tmp/coal-flag")
	if err != nil || got != "/tmp/coal-flag" {
		t.Fatalf("flag: got %q, %v", got, err)
	}
	t.Setenv(EnvHome, "")
	t.Setenv("HOME", "/tmp/someone")
	got, err = Home("")
	if err != nil || got != filepath.Join("/tmp/someone", ".coal") {
		t.Fatalf("default: got %q, %v", got, err)
	}
}
