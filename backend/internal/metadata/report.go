package metadata

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// rankBands are the places a check's report groups the book's place in a
// ranking into, when it is not first.
var rankBands = []struct {
	name     string
	from, to int
}{{"2", 2, 2}, {"3", 3, 3}, {"4-5", 4, 5}, {"6-10", 6, 10}, {"11+", 11, 1 << 30}}

// PrintCheck writes a match-check's report as tables a person reads.
func PrintCheck(w io.Writer, r CheckReport) {
	pct := func(n, of int) string {
		if of == 0 {
			return "-"
		}
		return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(of))
	}
	fmt.Fprintf(w, "Books with an ISBN           %d\n", r.Books)
	fmt.Fprintf(w, "Looked up without it         %d, at %s\n\n", r.Sample, strings.Join(r.Providers, ", "))

	tallies := append(append([]Tally{r.All}, r.PerProvider...), r.PerLanguage...)
	fmt.Fprintln(w, "The first candidate, by title, authors and language alone:")
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "ranking\tbooks\tright\tsame work\twrong\tnothing\tfailed\t")
	for _, x := range tallies {
		asked := x.Books - x.Failed
		fmt.Fprintf(t, "%s\t%d\t%s\t%s\t%s\t%s\t%d\t\n", x.Name, x.Books,
			pct(x.Right, asked), pct(x.SameWork, asked), pct(x.Wrong, asked), pct(x.None, asked), x.Failed)
	}
	t.Flush()

	fmt.Fprintln(w, "\nWhere the book stood when a wrong one was first (right or same work):")
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	header := "ranking\twrong first\t"
	for _, b := range rankBands {
		header += b.name + "\t"
	}
	fmt.Fprintln(t, header+"not found\t")
	for _, x := range tallies {
		line := fmt.Sprintf("%s\t%d\t", x.Name, x.Wrong)
		for _, b := range rankBands {
			n := 0
			for rank, books := range x.Ranks {
				if rank >= b.from && rank <= b.to {
					n += books
				}
			}
			line += fmt.Sprintf("%d\t", n)
		}
		fmt.Fprintf(t, "%s%d\t\n", line, x.Missing)
	}
	t.Flush()

	fmt.Fprintln(w, "\nThe first candidate's score (all):")
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "score\tright\tsame work\twrong\t")
	for _, b := range r.Scores {
		fmt.Fprintf(t, "%.2f-%.2f\t%d\t%d\t%d\t\n", b.From, b.To, b.Right, b.SameWork, b.Wrong)
	}
	t.Flush()

	fmt.Fprintf(w, "\nTaken at a threshold (all; * metadata.matchThreshold now, %.2f):\n", r.Threshold)
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "threshold\ttaken\tright\tsame work\twrong\tright or same work\t")
	for _, p := range r.Thresholds {
		mark := ""
		if p.Threshold == r.Threshold {
			mark = "*"
		}
		fmt.Fprintf(t, "%s%.2f\t%d\t%d\t%d\t%d\t%s\t\n", mark, p.Threshold, p.Taken, p.Right, p.SameWork, p.Wrong,
			pct(p.Right+p.SameWork, p.Taken))
	}
	t.Flush()
}
