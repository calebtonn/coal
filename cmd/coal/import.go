package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/importer/claudecode"
)

func init() { register(importCmd) }

func importCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Propose manifests from session records already on disk (ADR-012)",
	}
	cmd.AddCommand(importClaudeCodeCmd())
	return cmd
}

func importClaudeCodeCmd() *cobra.Command {
	var dir string
	var accept, markers, asJSON bool
	cmd := &cobra.Command{
		Use:   "claude-code --dir <projects-dir>",
		Short: "Propose one manifest per project from Claude Code transcripts",
		Long: "Read Claude Code transcripts (<projects-dir>/<project>/<session>.jsonl), group sessions by project " +
			"directory and print one proposed manifest per project. Nothing is written without --accept. " +
			"Projects that already have a manifest are reported as already recorded and skipped.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if markers && !accept {
				return errors.New("--markers needs --accept")
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			plan, err := claudecode.Propose(a, abs)
			if err != nil {
				return err
			}
			if accept {
				err = claudecode.Accept(a, plan, markers)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if e := enc.Encode(plan); e != nil {
					return e
				}
			} else {
				printPlan(out, plan, accept)
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", "", "Claude Code projects directory to read (required)")
	f.BoolVar(&accept, "accept", false, "create the proposed manifests")
	f.BoolVar(&markers, "markers", false, "with --accept, also write a .coal marker in each project that still exists")
	f.BoolVar(&asJSON, "json", false, "print the plan as JSON")
	cmd.MarkFlagRequired("dir")
	return cmd
}

func printPlan(w io.Writer, p *claudecode.Plan, accepted bool) {
	sessions, todo := 0, 0
	for _, pr := range p.Proposals {
		sessions += len(pr.Sessions)
		if pr.AlreadyRecorded == "" {
			todo++
		}
	}
	fmt.Fprintf(w, "%s: %d session(s) from %d file(s) in %s", p.Importer, sessions, p.Files, p.Dir)
	if p.SkippedFiles+p.SkippedLines > 0 {
		fmt.Fprintf(w, "; skipped %d file(s) and %d line(s) that did not parse", p.SkippedFiles, p.SkippedLines)
	}
	fmt.Fprintln(w)
	for i, pr := range p.Proposals {
		fmt.Fprintf(w, "\n%d. %s (%d session(s), last activity %s)", i+1, pr.Root, len(pr.Sessions), pr.LastActivityAt)
		if !pr.Exists {
			fmt.Fprint(w, " [directory gone]")
		}
		fmt.Fprintln(w)
		if pr.AlreadyRecorded != "" {
			fmt.Fprintf(w, "   already recorded as %s\n", pr.AlreadyRecorded)
			continue
		}
		fmt.Fprintf(w, "   summary (%s): %s\n", pr.Source, pr.Summary)
		if pr.NextStep != "" {
			fmt.Fprintf(w, "   next step: %s\n", pr.NextStep)
		}
		names := make([]string, len(pr.Artifacts))
		for j, f := range pr.Artifacts {
			names[j] = filepath.Base(f)
		}
		fmt.Fprintf(w, "   artifacts: %d", len(names))
		if len(names) > 0 {
			fmt.Fprintf(w, " (%s)", strings.Join(names, ", "))
		}
		if pr.MissingFiles > 0 {
			fmt.Fprintf(w, "; %d written file(s) no longer exist or sit outside the project", pr.MissingFiles)
		}
		fmt.Fprintln(w)
		if pr.Created != "" {
			fmt.Fprintf(w, "   created %s\n", pr.Created)
		}
	}
	switch {
	case accepted:
	case todo == 0:
		fmt.Fprintln(w, "\nNothing new to import.")
	default:
		fmt.Fprintf(w, "\nDry run: nothing written. Run again with --accept to create %d manifest(s).\n", todo)
	}
}
