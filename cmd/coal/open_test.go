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
	page, notes, env := filepath.Join(dir, "budget.html"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, ".env")
	os.WriteFile(page, []byte("<h1>Budget</h1>"), 0o600)
	os.WriteFile(notes, []byte("plain notes"), 0o600)
	os.WriteFile(env, []byte("X=1"), 0o600)
	out := coal(t, home, "add", "--intent", "Household budget tracker", "--artifact", page, "--artifact", notes, "--artifact", env)
	id := strings.Fields(out)[len(strings.Fields(out))-1] // stderr notes the skipped .env first

	// HTML: copied to tmp/<hex>.html, and that copy is what gets opened.
	got := strings.TrimSpace(coal(t, home, "open", id[:13]))
	if len(ran) != 2 || ran[1] != got || filepath.Dir(got) != filepath.Join(home, "tmp") || filepath.Ext(got) != ".html" {
		t.Fatalf("ran %q, printed %q", ran, got)
	}
	if body, _ := os.ReadFile(got); string(body) != "<h1>Budget</h1>" {
		t.Errorf("opened %q", body)
	}
	// Anything else opens the stored body in place.
	got = strings.TrimSpace(coal(t, home, "open", id, "--artifact", "1"))
	if ran[1] != got || filepath.Dir(got) != filepath.Join(home, "artifacts") {
		t.Errorf("ran %q, printed %q", ran, got)
	}

	for args, want := range map[string]string{"2": "has no stored body", "3": "takes 0 to 2", "-1": "takes 0 to 2"} {
		ran = nil
		var errOut strings.Builder
		root := rootCmd()
		root.SetOut(&strings.Builder{})
		root.SetErr(&errOut)
		root.SetArgs([]string{"--home", home, "open", id, "--artifact", args})
		err := root.Execute()
		if err == nil || ran != nil || !strings.Contains(err.Error(), want) {
			t.Errorf("open --artifact %s: err=%v ran=%q", args, err, ran)
		}
	}
}
