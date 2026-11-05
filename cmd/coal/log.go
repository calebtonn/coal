package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/store"
)

func init() { register(logCmd) }

var fullID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func withoutTZ(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TZ=") {
			out = append(out, kv)
		}
	}
	return out
}

func logCmd() *cobra.Command {
	var patch bool
	cmd := &cobra.Command{
		Use:   "log <id>",
		Short: "Show a manifest's change history (ADR-014)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := dataHome()
			if err != nil {
				return err
			}
			// A removed manifest keeps its history, so a full id is
			// accepted even when manifests/ no longer has the file.
			id := strings.ToLower(args[0])
			if !fullID.MatchString(id) {
				a, rid, err := openApp(args[0])
				if err != nil {
					return err
				}
				a.Close()
				id = rid
			}
			if _, err := exec.LookPath("git"); err != nil {
				return errors.New("git is not installed, so manifest history is off; install git and new writes will be recorded")
			}
			if _, err := os.Stat(filepath.Join(home, ".git")); err != nil {
				return fmt.Errorf("history is off for %s: it has no .git directory", home)
			}
			gitArgs := store.GitArgs(home, "log", "--format=%h %ad %s", "--date=iso-strict-local")
			if patch {
				gitArgs = append(gitArgs, "--patch", "--no-ext-diff", "--no-textconv")
			}
			gitArgs = append(gitArgs, "--", "manifests/"+id+".json")
			c := exec.Command("git", gitArgs...)
			c.Env = append(withoutTZ(store.GitEnv()), "TZ=UTC") // dates in UTC, like every other timestamp
			var out, errb bytes.Buffer
			c.Stdout, c.Stderr = &out, &errb
			if err := c.Run(); err != nil {
				return fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(errb.String()))
			}
			if out.Len() == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "no recorded history for %s\n", id)
				return nil
			}
			_, err = cmd.OutOrStdout().Write(out.Bytes())
			return err
		},
	}
	cmd.Flags().BoolVar(&patch, "patch", false, "show the change made by each commit")
	return cmd
}
