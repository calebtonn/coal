package main

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/metrics"
)

func init() { register(statsCmd) }

func statsCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Count local usage events from logs/metrics.jsonl (nothing leaves this machine)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := dataHome()
			if err != nil {
				return err
			}
			if days < 0 {
				return fmt.Errorf("--days must be 0 (all time) or more")
			}
			var since time.Time
			if days > 0 {
				since = time.Now().Add(-time.Duration(days) * 24 * time.Hour)
			}
			s, err := metrics.Summarize(home, since)
			if err != nil {
				return err
			}
			printStats(cmd.OutOrStdout(), s, days)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "how many days back to count; 0 counts everything")
	return cmd
}

func printStats(w io.Writer, s metrics.Summary, days int) {
	if days > 0 {
		fmt.Fprintf(w, "Last %d days, since %s\n\n", days, s.Since.UTC().Format(time.RFC3339))
	} else {
		fmt.Fprint(w, "All time\n\n")
	}
	for _, e := range append(append([]string{}, metrics.Events...), s.Others()...) {
		fmt.Fprintf(w, "  %-18s %5d\n", e, s.Counts[e])
		if e == metrics.Find {
			tools := make([]string, 0, len(s.FindTools))
			for t := range s.FindTools {
				tools = append(tools, t)
			}
			sort.Strings(tools)
			for _, t := range tools {
				fmt.Fprintf(w, "    %-16s %5d\n", t, s.FindTools[t])
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  resume acted on / shown           %s\n", ratio(s.ResumeActed()))
	fmt.Fprintf(w, "  attached / (attached + created)   %s\n", ratio(s.Attached()))
	if s.Bad > 0 {
		fmt.Fprintf(w, "\n  %d lines in the metrics file could not be read\n", s.Bad)
	}
}

func ratio(r metrics.Ratio) string {
	if r.Total == 0 {
		return "no data"
	}
	return fmt.Sprintf("%d / %d (%d%%)", r.N, r.Total, (100*r.N+r.Total/2)/r.Total)
}
