package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/index"
)

func findCmd() *cobra.Command {
	var opts index.SearchOpts
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "find [query...]",
		Short: "Search manifests; with no query, list them newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			rs, err := a.Search(strings.Join(args, " "), opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				return enc.Encode(rs)
			}
			if len(rs) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "no manifests found")
				return nil
			}
			all, err := a.Index.IDs()
			if err != nil {
				return err
			}
			printResults(out, rs, all, isTTY(out), time.Now())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Status, "status", "", "only active, paused, done or abandoned manifests")
	f.IntVar(&opts.Limit, "limit", index.DefaultLimit, fmt.Sprintf("most results to show (at most %d)", index.MaxLimit))
	f.BoolVar(&asJSON, "json", false, "print results as JSON in the API shape")
	return cmd
}

func reindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the search index from manifests/ alone",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			n, skipped, err := a.Reindex()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "indexed %d manifests, skipped %d as corrupt\n", n, skipped)
			if skipped > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "the skipped files are listed in logs/coal.log")
			}
			return nil
		},
	}
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// printResults writes a padded table, each row followed by an indented
// snippet line. On a TTY the snippet's <b> becomes bold; elsewhere it is dropped.
func printResults(w io.Writer, rs []index.Result, all []string, tty bool, now time.Time) {
	rows := make([][]string, len(rs))
	widths := make([]int, 5)
	for i, r := range rs {
		count := fmt.Sprintf("%d artifact", r.ArtifactCount)
		if r.ArtifactCount != 1 {
			count += "s"
		}
		rows[i] = []string{shortID(r.ID, all), truncate(oneLine(printable(r.Summary)), 60), printable(r.Status), relTime(r.LastActivityAt, now), count}
		for j, c := range rows[i] {
			widths[j] = max(widths[j], utf8.RuneCountInString(c))
		}
	}
	bold, plain := "", ""
	if tty {
		bold, plain = "\x1b[1m", "\x1b[22m"
	}
	for i, row := range rows {
		var line strings.Builder
		for j, c := range row {
			line.WriteString(c)
			if j < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(c)+2))
			}
		}
		fmt.Fprintln(w, line.String())
		if sn := rs[i].Snippet; sn != "" {
			fmt.Fprintf(w, "          %s\n", terminalSnippet(sn, bold, plain))
		}
	}
}

// shortID returns the shortest prefix of id, at least 8 characters, that
// no other id in all shares. UUIDv7 ids made in the same minute share
// their first 8 characters, so a fixed 8 would not tell them apart.
// printable drops control and format characters (escapes, carriage
// returns, bidi overrides), so stored text cannot drive the terminal.
// Newlines, carriage returns and tabs become spaces.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// terminalSnippet turns an HTML-escaped snippet into terminal text. The
// <b> markers become private-use runes (the index strips those from the
// text it stores, so these are the only ones), the rest is unescaped once
// and made printable, and only then do the markers become bold codes.
func terminalSnippet(sn, bold, plain string) string {
	s := strings.NewReplacer("<b>", "\ue000", "</b>", "\ue001").Replace(sn)
	s = printable(html.UnescapeString(s))
	return oneLine(strings.NewReplacer("\ue000", bold, "\ue001", plain).Replace(s))
}

func shortID(id string, all []string) string {
	n := min(8, len(id))
	for _, o := range all {
		if o == id {
			continue
		}
		l := 0
		for l < len(id) && l < len(o) && id[l] == o[l] {
			l++
		}
		n = max(n, min(l+1, len(id)))
	}
	return id[:n]
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// relTime formats an ISO 8601 timestamp as "3h ago".
func relTime(ts string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
}
