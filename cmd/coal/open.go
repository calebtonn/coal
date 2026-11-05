package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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

// withExtension returns a path the default handler can recognise. Bodies
// are stored without an extension, so an HTML body is copied once to
// tmp/<hex>.html in the data directory (scratch, not history). Other
// bodies are opened in place.
func withExtension(home, body, typ, mime string) (string, error) {
	if typ != "html" && !strings.HasPrefix(mime, "text/html") {
		return body, nil
	}
	dst := filepath.Join(home, "tmp", filepath.Base(body)+".html")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // content-addressed, so an existing copy is current
	}
	data, err := os.ReadFile(body)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

func openCmd() *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "open <id>",
		Short: "Open an artifact's stored body with the default application",
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
			if n < 0 || n >= len(m.Artifacts) {
				return fmt.Errorf("manifest %s has %d artifacts; --artifact takes 0 to %d", id, len(m.Artifacts), len(m.Artifacts)-1)
			}
			art := m.Artifacts[n]
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
			if path, err = withExtension(a.Store.Dir(), path, art.Type, art.MIME); err != nil {
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
	cmd.Flags().IntVar(&n, "artifact", 0, "which artifact to open, counting from 0 in manifest order")
	return cmd
}
