package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/metrics"
	"github.com/calebtonn/coal/internal/store"
)

func init() { register(renameCmd) }

// maxSummary matches the Claude Code adapter's 200-character trim.
const maxSummary = 200

func renameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `rename <id> "<summary>"`,
		Short: "Set a manifest's intent summary in your own words",
		Long: "Set intent.summary to the text you give and mark it as written by you (source user).\n" +
			"Search ranks your own summaries above model-written ones.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary := strings.TrimSpace(strings.Join(args[1:], " "))
			if n := utf8.RuneCountInString(summary); n == 0 || n > maxSummary {
				return fmt.Errorf("the summary must be 1 to %d characters; this one is %d", maxSummary, n)
			}
			a, id, err := openApp(args[0])
			if err != nil {
				return err
			}
			defer a.Close()
			var previous string
			changed := false
			_, err = a.Update(id, 0, func(m *manifest.Manifest) error {
				previous = m.Intent.Source
				if m.Intent.Summary == summary && m.Intent.Source == manifest.SourceUser {
					return store.ErrNoChange
				}
				m.Intent.Summary, m.Intent.Source = summary, manifest.SourceUser
				changed = true
				return nil
			}, store.Op{Tool: "coal-cli", Action: "rename"})
			if err != nil {
				return err
			}
			if previous == "" {
				previous = manifest.SourceModel
			}
			if changed {
				metrics.Record(a.Store.Dir(), metrics.Rename, map[string]any{"manifest": id, "previousSource": previous})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", id, summary)
			return nil
		},
	}
}
