// Command coal stores Intent Manifests and exposes them to AI tools.
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/paths"
)

const appVersion = "0.1.0-poc"

var homeFlag string

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "coal",
		Short:         "A local record of the work you make with AI tools",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVar(&homeFlag, "home", "", "data directory (overrides COAL_HOME and ~/.coal)")
	root.AddCommand(versionCmd(), initCmd(), validateCmd(), addCmd(), showCmd(), rmCmd(), findCmd(), reindexCmd())
	return root
}

// dataHome resolves the data directory for the current invocation.
func dataHome() (string, error) {
	return paths.Home(homeFlag)
}
