package main

import (
	"errors"
	"fmt"
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
				return switchMarker(cmd, root, switchTo)
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
			note, err := pointMarker(root, m.ID, valid, cfg.Markers)
			if note != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), note)
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

// pointMarker makes id the active manifest in root's marker. A marker
// that exists is always updated: matching reads it, so leaving it on the
// old id would keep sessions there, and the markers setting only stops
// COAL creating new marker files. valid says whether root held a valid
// marker when the caller looked; if a capture writes the first marker
// after that, Write reports ErrOtherManifest and the id is added to the
// now valid marker instead. note is a line for the user, or "".
func pointMarker(root, id string, valid bool, markers string) (note string, err error) {
	if valid {
		if markers == fold.MarkersNone {
			note = "markers are off in config.json, but this project already has a .coal marker; it now points at the new manifest"
		}
		return note, marker.Activate(root, id)
	}
	if markers == fold.MarkersNone {
		return "markers are off in config.json; no .coal written, sessions match by path", nil
	}
	err = marker.Write(root, id)
	if errors.Is(err, marker.ErrOtherManifest) {
		err = marker.Activate(root, id)
	}
	return "", err
}

// switchMarker makes a manifest active in root's marker: one the
// marker lists, or any active or paused manifest whose path context is
// this project root, which is then added to the list.
func switchMarker(cmd *cobra.Command, root, prefix string) error {
	prefix = strings.ToLower(prefix)
	if len(prefix) < 8 {
		return fmt.Errorf("id prefix %q is too short; give at least 8 characters", prefix)
	}
	_, ids, hasMarker := marker.Read(root)
	var hits []string
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			hits = append(hits, id)
		}
	}
	if len(hits) > 1 {
		return fmt.Errorf("id prefix %s matches %d of this project's manifests", prefix, len(hits))
	}
	a, _, err := openApp("")
	if err != nil {
		return err
	}
	defer a.Close()
	id := ""
	if len(hits) == 1 {
		id = hits[0]
	} else {
		if id, err = a.Resolve(prefix); err != nil {
			return err
		}
		m, err := a.Get(id)
		if err != nil {
			return err
		}
		if !forProject(m, root) {
			return fmt.Errorf("%s is not an active or paused manifest for %s", id, root)
		}
	}
	switch {
	case hasMarker:
		err = marker.Activate(root, id)
	case fold.LoadConfig(a.Store.Dir()).Markers == fold.MarkersNone:
		return errors.New("this project has no .coal marker and markers are off in config.json; nothing to switch")
	default:
		err = marker.Write(root, id)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), id)
	return nil
}

// forProject reports whether m is active or paused and has a path
// context equal to root.
func forProject(m *manifest.Manifest, root string) bool {
	if m.Status != manifest.StatusActive && m.Status != manifest.StatusPaused {
		return false
	}
	for _, c := range m.Contexts {
		if c.Kind == marker.KindPath && filepath.Clean(c.Key) == root {
			return true
		}
	}
	return false
}
