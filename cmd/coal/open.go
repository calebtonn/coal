package main

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// opener starts a command without waiting for it. Tests replace it.
var opener = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

// openCommand returns the OS default-handler command for path.
func openCommand(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "cmd", []string{"/c", "start", "", path}
	}
	return "xdg-open", []string{path}
}

// viewCopy copies a stored body to tmp/<hex8>-<title> in the data
// directory and returns that path. Viewers get the copy, never the
// content-addressed original, and the name carries an extension (from
// the MIME type when the title has none) so the default handler knows
// the type. tmp/ is scratch; the store's history covers only manifests/
// and sessions/.
func viewCopy(home, body, title, mimeType string) (string, error) {
	name := sanitize(title)
	// The extension comes from the MIME type. The title's own extension is
	// kept only when the type lists it and it cannot run anything.
	ext := strings.ToLower(filepath.Ext(name))
	if allowed, _ := mime.ExtensionsByType(mimeType); !slices.Contains(allowed, ext) || runnable[ext] {
		if runnable[ext] {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if name = strings.Trim(name, ".-"); name == "" {
			name = "artifact"
		}
		name += extFor(mimeType)
	}
	hex := filepath.Base(body)
	dst := filepath.Join(home, "tmp", hex[:min(8, len(hex))]+"-"+name)
	data, err := os.ReadFile(body)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	// A fresh copy each time, so edits made in a viewer do not stick.
	f, err := os.CreateTemp(filepath.Dir(dst), ".open-*")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err = errors.Join(err, f.Close()); err == nil {
		err = os.Rename(f.Name(), dst)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return dst, nil
}

// sanitize keeps letters, digits, '.', '-' and '_' from a title, turns
// runs of anything else into one '-', and never returns a hidden or
// empty name.
func sanitize(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), ".-")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	if s == "" {
		return "artifact"
	}
	return s
}

// runnable extensions are never handed to the default handler, which may
// run them instead of showing them.
var runnable = map[string]bool{
	".desktop": true, ".sh": true, ".bash": true, ".zsh": true, ".command": true, ".tool": true, ".app": true,
	".exe": true, ".bat": true, ".cmd": true, ".com": true, ".ps1": true, ".msi": true, ".scr": true,
	".jar": true, ".appimage": true, ".run": true, ".deb": true, ".rpm": true, ".pkg": true, ".dmg": true,
}

// extFor picks a file extension for a MIME type, preferring the common
// one, and ".txt" for any type whose extension could run.
func extFor(mimeType string) string {
	ext := extForType(mimeType)
	if runnable[ext] {
		return ".txt"
	}
	return ext
}

func extForType(mimeType string) string {
	exts, _ := mime.ExtensionsByType(mimeType)
	for _, pref := range []string{".html", ".txt", ".md", ".json", ".jpg", ".svg"} {
		if slices.Contains(exts, pref) {
			return pref
		}
	}
	if len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func openCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "open <id>",
		Short: "Open a copy of an artifact with the default application",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, id, err := openApp(args[0])
			if err != nil {
				return err
			}
			defer a.Close()
			m, err := a.Get(id)
			if err != nil {
				return err
			}
			if len(m.Artifacts) == 0 {
				return fmt.Errorf("manifest %s has no artifacts", id)
			}
			if n < 1 || n > len(m.Artifacts) {
				return fmt.Errorf("manifest %s has %d artifacts; --artifact takes 1 to %d", id, len(m.Artifacts), len(m.Artifacts))
			}
			art := m.Artifacts[n-1]
			if art.Skipped != "" {
				return fmt.Errorf("artifact %d (%s) has no stored body: %s", n, art.Title, art.Skipped)
			}
			path := a.Store.ArtifactPath(art.Hash)
			if path == "" {
				return fmt.Errorf("artifact %d has an invalid hash %q", n, art.Hash)
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("artifact %d (%s): %w", n, art.Title, err)
			}
			if path, err = viewCopy(a.Store.Dir(), path, art.Title, art.MIME); err != nil {
				return fmt.Errorf("artifact %d (%s): %w", n, art.Title, err)
			}
			name, cargs := openCommand(runtime.GOOS, path)
			if err := opener(name, cargs...); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
	cmd.Flags().IntVar(&n, "artifact", 1, "which artifact to open, counting from 1 in manifest order")
	return cmd
}
