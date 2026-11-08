package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/manifest"
	"github.com/calebtonn/coal/internal/marker"
	"github.com/calebtonn/coal/internal/metrics"
)

func init() { register(resumeCmd) }

const (
	noRecord        = "No COAL record for this directory. Start claude normally; capture will create one on the first file write."
	maxResumePrompt = 1500
	resumeLaunch    = "resume_launch"
)

// exitCode is an error that carries the process exit status. The
// command prints the message itself, so cobra's "Error:" prefix stays off.
type exitCode struct {
	code int
	msg  string
}

func (e *exitCode) Error() string { return e.msg }

// exit and execve are swapped out by tests.
var (
	exit   = os.Exit
	execve = syscall.Exec
)

func resumeCmd() *cobra.Command {
	var tool string
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Start the tool in this directory with a first prompt that picks up where you left off",
		Long: "Finds the COAL record for the current directory, the same way the session-start hook does, " +
			"and starts claude with a first prompt that opens from the next step (ADR-016).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runResume(cmd, tool, printOnly)
			var ec *exitCode
			if errors.As(err, &ec) {
				fmt.Fprintln(cmd.ErrOrStderr(), ec.msg)
				exit(ec.code)
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&tool, "tool", "claude-code", "harness to start; only claude-code is supported")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the command, one argument per line, instead of running it")
	return cmd
}

func runResume(cmd *cobra.Command, tool string, printOnly bool) error {
	if tool != "claude-code" {
		return &exitCode{2, fmt.Sprintf("unknown --tool %q; supported: claude-code", tool)}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	a, _, err := openApp("")
	if err != nil {
		return err
	}
	home := a.Store.Dir()
	ms, _ := a.Store.List()
	m := marker.Match(cwd, ms)
	a.Close() // before exec, which never returns
	if m == nil {
		return &exitCode{1, noRecord}
	}
	argv := []string{"claude", resumePrompt(m)}
	if printOnly {
		for _, s := range argv {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		}
		return nil
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return &exitCode{1, "claude was not found on PATH. Install Claude Code, or run coal resume --print to see the command."}
	}
	metrics.Record(home, resumeLaunch, map[string]any{"manifest": m.ID})
	return execve(path, argv, os.Environ())
}

// resumePrompt is the first user turn for a resumed session: plain text,
// one line, under maxResumePrompt characters. Missing parts are left out.
func resumePrompt(m *manifest.Manifest) string {
	parts := []string{"Resume my work on: " + sentence(m.Intent.Summary, 300)}
	if next := sentence(m.State.NextStep, 300); next != "" {
		parts = append(parts, "Last time we left off at: "+next)
	}
	var qs []string
	for _, q := range m.State.OpenQuestions {
		if q = clip(q, 200); q != "" {
			qs = append(qs, strings.TrimRight(q, "."))
		}
	}
	if len(qs) > 0 {
		parts = append(parts, "Open questions: "+sentence(strings.Join(qs, "; "), 600))
	}
	parts = append(parts, "Start by telling me in one sentence where I left off and ask whether to continue.")
	p := strings.Join(parts, " ")
	if r := []rune(p); len(r) > maxResumePrompt {
		p = string(r[:maxResumePrompt-1]) + "…"
	}
	return p
}

// sentence flattens s to one line, clips it to n runes and ends it with
// one period, or returns "" when s is blank.
func sentence(s string, n int) string {
	s = strings.TrimRight(clip(s, n), ".")
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "…") {
		return s
	}
	return s + "."
}

// clip drops control characters (so no terminal escapes reach --print
// or the prompt), collapses whitespace and cuts s to n runes.
func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}
