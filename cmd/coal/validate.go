package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/manifest"
)

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>...",
		Short: "Check manifest files against the Intent Manifest schema",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			failed := 0
			for _, path := range args {
				data, err := os.ReadFile(path)
				if err == nil {
					err = manifest.Validate(data)
				}
				if err != nil {
					failed++
					for line := range strings.SplitSeq(err.Error(), "\n") {
						fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", path, line)
					}
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "ok %s\n", path)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d files failed", failed, len(args))
			}
			return nil
		},
	}
}
