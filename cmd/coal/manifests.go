package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/app"
	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/store"
)

var cliProvenance = manifest.Provenance{Tool: "coal-cli", CapturedBy: "coal-cli@" + appVersion}

// openApp opens the data directory and, when arg is set, resolves it as
// an id or a unique id prefix. The caller closes the app.
func openApp(arg string) (*app.App, string, error) {
	dir, err := dataHome()
	if err != nil {
		return nil, "", err
	}
	a, err := app.Open(dir)
	if err != nil || arg == "" {
		return a, "", err
	}
	id, err := a.Resolve(arg)
	if err != nil {
		a.Close()
		return nil, "", err
	}
	return a, id, nil
}

func addCmd() *cobra.Command {
	var intent, goal, status string
	var tags, artifacts []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Record a new Intent Manifest in your own words",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			m := manifest.New(intent, manifest.SourceUser, cliProvenance)
			m.Intent.Goal, m.Intent.Tags, m.Status = goal, tags, status
			if err := a.Create(m, store.Op{Tool: "coal-cli", Action: "add manifest"}); err != nil {
				return err
			}
			for _, p := range artifacts {
				art, err := a.RegisterArtifact(m.ID, store.ArtifactInput{Path: p, Producer: cliProvenance}, store.Op{Tool: "coal-cli"})
				if err != nil {
					return fmt.Errorf("manifest %s created, but registering %s failed: %w", m.ID, p, err)
				}
				if art.Skipped != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: body not copied (%s)\n", p, art.Skipped)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), m.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&intent, "intent", "", "what you are trying to do, in your words (required)")
	f.StringVar(&goal, "goal", "", "what done looks like")
	f.StringVar(&status, "status", manifest.StatusActive, "active, paused, done or abandoned")
	f.StringArrayVar(&tags, "tag", nil, "tag, repeatable")
	f.StringArrayVar(&artifacts, "artifact", nil, "file to register as an artifact, repeatable")
	cmd.MarkFlagRequired("intent")
	return cmd
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a manifest as JSON (a unique id prefix of 8 or more characters works)",
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
			b, err := m.Marshal()
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(b)
			return err
		},
	}
}

func rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Move a manifest to the trash (nothing is deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, id, err := openApp(args[0])
			if err != nil {
				return err
			}
			defer a.Close()
			dst, err := a.Remove(id, store.Op{Tool: "coal-cli", Action: "remove manifest"})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), dst)
			return nil
		},
	}
}
