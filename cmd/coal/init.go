package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/store"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the data directory if it does not exist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataHome()
			if err != nil {
				return err
			}
			if err := initHome(dir); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		},
	}
}

// initHome creates the data directory layout and history through the
// store. It never overwrites an existing config.json, so running it
// twice is safe.
func initHome(dir string) error {
	_, err := store.Open(dir)
	return err
}
