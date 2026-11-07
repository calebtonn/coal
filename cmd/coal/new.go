package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/fold"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/store"
)

func init() { register(newCmd) }

func newCmd() *cobra.Command {
	var intent, switchTo string
	cmd := &cobra.Command{
		Use:   "new --intent \"...\" | --switch <id>",
		Short: "Start a new manifest in this project and make it the active one",
		Long: "Start a new Intent Manifest for the project you are in, in your own words, and make it the one " +
			"later sessions attach to. The project's .coal marker keeps every manifest it has held. " +
			"--switch makes one of those the active manifest again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root := marker.ProjectRoot(cwd)
			if marker.Unanchored(root) {
				return fmt.Errorf("%s is the home directory, above it, or the root; run coal new inside a project", root)
			}
			out := cmd.OutOrStdout()
			if switchTo != "" {
				if intent != "" {
					return errors.New("use --intent or --switch, not both")
				}
				return switchMarker(out, root, switchTo)
			}
			if strings.TrimSpace(intent) == "" {
				return errors.New("--intent is required: say what you are starting, in your words")
			}
			_, present := os.Lstat(filepath.Join(root, marker.FileName))
			_, _, valid := marker.Read(root)
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			cfg := fold.LoadConfig(a.Store.Dir())
			if cfg.Markers != fold.MarkersNone && present == nil && !valid {
				return fmt.Errorf("%s exists but is not a COAL marker; move it aside first", filepath.Join(root, marker.FileName))
			}
			m := manifest.New(strings.TrimSpace(intent), manifest.SourceUser, cliProvenance)
			m.Contexts = marker.Contexts(cwd)
			if err := a.Create(m, store.Op{Tool: "coal-cli", Action: "new"}); err != nil {
				return err
			}
			switch {
			case cfg.Markers == fold.MarkersNone:
				fmt.Fprintln(cmd.ErrOrStderr(), "markers are off in config.json; no .coal written, sessions match by path")
			case valid:
				err = marker.Activate(root, m.ID)
			default:
				err = marker.Write(root, m.ID)
			}
			if err != nil {
				return fmt.Errorf("created %s but could not update the marker: %w", m.ID, err)
			}
			fmt.Fprintln(out, m.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&intent, "intent", "", "what you are starting, in your words")
	cmd.Flags().StringVar(&switchTo, "switch", "", "make this manifest from the project's marker active (id or unique prefix)")
	return cmd
}

// switchMarker makes the listed manifest matching prefix active.
func switchMarker(w io.Writer, root, prefix string) error {
	_, ids, ok := marker.Read(root)
	if !ok {
		return fmt.Errorf("no COAL marker in %s", root)
	}
	prefix = strings.ToLower(prefix)
	if len(prefix) < 8 {
		return fmt.Errorf("id prefix %q is too short; give at least 8 characters", prefix)
	}
	var hits []string
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			hits = append(hits, id)
		}
	}
	switch len(hits) {
	case 0:
		return fmt.Errorf("%s is not one of this project's manifests: %s", prefix, strings.Join(ids, ", "))
	case 1:
	default:
		return fmt.Errorf("id prefix %s matches %d of this project's manifests", prefix, len(hits))
	}
	if err := marker.Activate(root, hits[0]); err != nil {
		return err
	}
	fmt.Fprintln(w, hits[0])
	return nil
}
