package main

import (
	"encoding/json"
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

	hexOf := func(i int) string {
		var m struct{ Artifacts []struct{ Hash string } }
		json.Unmarshal([]byte(coal(t, home, "show", id)), &m)
		return strings.TrimPrefix(m.Artifacts[i].Hash, "sha256:")
	}
	tmp := filepath.Join(home, "tmp")

	// Every artifact opens as a copy in tmp/, named <hex8>-<title>.
	got := strings.TrimSpace(coal(t, home, "open", id[:13]))
	if want := filepath.Join(tmp, hexOf(0)[:8]+"-budget.html"); got != want || len(ran) != 2 || ran[1] != want {
		t.Fatalf("ran %q, printed %q, want %s", ran, got, want)
	}
	os.WriteFile(got, []byte("edited in a viewer"), 0o600)
	coal(t, home, "open", id)
	if body, _ := os.ReadFile(got); string(body) != "<h1>Budget</h1>" {
		t.Errorf("copy not refreshed: %q", body)
	}
	if body, _ := os.ReadFile(filepath.Join(home, "artifacts", hexOf(0))); string(body) != "<h1>Budget</h1>" {
		t.Errorf("store body changed: %q", body)
	}
	got = strings.TrimSpace(coal(t, home, "open", id, "--artifact", "2"))
	if want := filepath.Join(tmp, hexOf(1)[:8]+"-notes.txt"); got != want {
		t.Errorf("artifact 2 opened %q, want %s", got, want)
	}

	for args, want := range map[string]string{"3": "has no stored body", "4": "takes 1 to 3", "0": "takes 1 to 3"} {
		ran = nil
		root := rootCmd()
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		root.SetArgs([]string{"--home", home, "open", id, "--artifact", args})
		err := root.Execute()
		if err == nil || ran != nil || !strings.Contains(err.Error(), want) {
			t.Errorf("open --artifact %s: err=%v ran=%q", args, err, ran)
		}
	}
}

func TestViewCopyName(t *testing.T) {
	home := t.TempDir()
	body := filepath.Join(t.TempDir(), "0123456789abcdef")
	os.WriteFile(body, []byte("x"), 0o600)
	for _, c := range []struct{ title, mime, want string }{
		{"budget.html", "text/html", "01234567-budget.html"},
		{"Budget tracker", "text/html; charset=utf-8", "01234567-Budget-tracker.html"},
		{"notes", "text/plain", "01234567-notes.txt"},
		{"../../etc/passwd", "text/plain", "01234567-etc-passwd.txt"},
		{"", "application/octet-stream", "01234567-artifact.bin"},
		{"..", "", "01234567-artifact"},
		// The extension follows the MIME type, never a runnable title.
		{"install.sh", "text/plain", "01234567-install.txt"},
		{"run.sh", "", "01234567-run"},
		{"page.html", "text/plain", "01234567-page.html.txt"},
		{"PAGE.HTML", "text/html", "01234567-PAGE.HTML"},
		{"v1.2 notes", "text/plain", "01234567-v1.2-notes.txt"},
	} {
		got, err := viewCopy(home, body, c.title, c.mime)
		if err != nil || got != filepath.Join(home, "tmp", c.want) {
			t.Errorf("viewCopy(%q, %q) = %q, %v; want %s", c.title, c.mime, got, err, c.want)
		}
	}
	// Whatever the system MIME table says, a runnable extension never comes out.
	for _, title := range []string{"launch.desktop", "install.sh", "x.exe", "a.jar"} {
		for _, mt := range []string{"application/x-desktop", "application/x-sh", "text/x-sh", "application/x-msdownload", "application/java-archive", "text/plain"} {
			got, err := viewCopy(home, body, title, mt)
			if err != nil || runnable[strings.ToLower(filepath.Ext(got))] {
				t.Errorf("viewCopy(%q, %q) = %q, %v", title, mt, got, err)
			}
		}
	}
}
