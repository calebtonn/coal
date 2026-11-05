package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

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
