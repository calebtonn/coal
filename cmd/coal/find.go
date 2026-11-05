package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/index"
	"github.com/calebtonn/coal/internal/store"
)

// openIndex opens index.db in the store's data directory. An empty index
// is rebuilt from the store first, so a new or deleted index.db never
// hides manifests that already exist.
func openIndex(s *store.Store) (*index.Index, error) {
	ix, err := index.Open(filepath.Join(s.Dir(), "index.db"))
	if err != nil {
		return nil, err
	}
	n, err := ix.Count()
	if err == nil && n == 0 {
		_, _, err = rebuild(s, ix)
	}
	if err != nil {
		ix.Close()
		return nil, err
	}
	return ix, nil
}

// withIndex runs fn on the store's index after a store write. The write
// already succeeded, so a failure here is a warning, not an error: the
// index is derived and `coal reindex` repairs it.
func withIndex(cmd *cobra.Command, s *store.Store, fn func(*index.Index) error) {
	ix, err := openIndex(s)
	if err == nil {
		err = fn(ix)
		ix.Close()
	}
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: search index not updated (%v); run coal reindex\n", err)
	}
}

// indexManifest re-reads id from the store and upserts it.
func indexManifest(cmd *cobra.Command, s *store.Store, id string) {
	withIndex(cmd, s, func(ix *index.Index) error {
		m, err := s.Get(id)
		if err != nil {
			return err
		}
		return ix.Upsert(m)
	})
}

// rebuild indexes every readable manifest and returns how many were
// indexed and the errors for the ones skipped as corrupt.
func rebuild(s *store.Store, ix *index.Index) (int, []error, error) {
	ms, errs := s.List()
	return len(ms), errs, ix.Rebuild(ms)
}

func findCmd() *cobra.Command {
	var opts index.SearchOpts
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "find [query...]",
		Short: "Search manifests; with no query, list them newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, _, err := openStore("")
			if err != nil {
				return err
			}
			ix, err := openIndex(s)
			if err != nil {
				return err
			}
			defer ix.Close()
			rs, err := ix.Search(strings.Join(args, " "), opts)
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
			all, err := ix.IDs()
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
			s, _, err := openStore("")
			if err != nil {
				return err
			}
			ix, err := openIndex(s)
			if err != nil {
				return err
			}
			defer ix.Close()
			n, errs, err := rebuild(s, ix)
			if err != nil {
				return err
			}
			for _, e := range errs {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped: %v\n", e)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "indexed %d manifests, skipped %d as corrupt\n", n, len(errs))
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
		rows[i] = []string{shortID(r.ID, all), truncate(oneLine(r.Summary), 60), r.Status, relTime(r.LastActivityAt, now), count}
		for j, c := range rows[i] {
			widths[j] = max(widths[j], utf8.RuneCountInString(c))
		}
	}
	bold, plain := "", ""
	if tty {
		bold, plain = "\x1b[1m", "\x1b[22m"
	}
	marks := strings.NewReplacer("<b>", bold, "</b>", plain)
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
			fmt.Fprintf(w, "          %s\n", html.UnescapeString(marks.Replace(oneLine(sn))))
		}
	}
}

// shortID returns the shortest prefix of id, at least 8 characters, that
// no other id in all shares. UUIDv7 ids made in the same minute share
// their first 8 characters, so a fixed 8 would not tell them apart.
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
