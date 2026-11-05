package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/paths"
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

// initHome creates the data directory layout. It never overwrites
// an existing config.json, so running it twice is safe.
func initHome(dir string) error {
	for _, sub := range paths.Subdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	cfg := filepath.Join(dir, "config.json")
	f, err := os.OpenFile(cfg, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString("{\"version\":\"0.1\"}\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
