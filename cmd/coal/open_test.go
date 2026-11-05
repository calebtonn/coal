package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCommand(t *testing.T) {
	for goos, want := range map[string][]string{
		"linux":   {"xdg-open", "/p"},
		"freebsd": {"xdg-open", "/p"},
		"darwin":  {"open", "/p"},
		"windows": {"cmd", "/c", "start", "", "/p"},
	} {
		name, args := openCommand(goos, "/p")
		if got := append([]string{name}, args...); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}

func TestOpen(t *testing.T) {
	var ran []string
	saved := opener
	opener = func(name string, args ...string) error { ran = append([]string{name}, args...); return nil }
	t.Cleanup(func() { opener = saved })

	home, dir := t.TempDir(), t.TempDir()
	page, env := filepath.Join(dir, "budget.html"), filepath.Join(dir, ".env")
	os.WriteFile(page, []byte("<h1>Budget</h1>"), 0o600)
	os.WriteFile(env, []byte("X=1"), 0o600)
	id := strings.TrimSpace(coal(t, home, "add", "--intent", "Household budget tracker", "--artifact", page, "--artifact", env))
	id = strings.Fields(id)[len(strings.Fields(id))-1] // stderr notes the skipped .env first

	out := strings.TrimSpace(coal(t, home, "open", id[:13]))
	if len(ran) != 2 || ran[1] != out || filepath.Dir(out) != filepath.Join(home, "artifacts") {
		t.Fatalf("ran %q, printed %q", ran, out)
	}
	if body, _ := os.ReadFile(out); string(body) != "<h1>Budget</h1>" {
		t.Errorf("opened %q", body)
	}

	for _, args := range [][]string{{"--artifact", "2"}, {"--artifact", "3"}, {"--artifact", "0"}} {
		ran = nil
		root := rootCmd()
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		root.SetArgs(append([]string{"--home", home, "open", id}, args...))
		if err := root.Execute(); err == nil || ran != nil {
			t.Errorf("open %v: err=%v ran=%q", args, err, ran)
		}
	}
}
