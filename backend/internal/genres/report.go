package genres

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// PrintCheck writes a check's report as tables a person reads.
func PrintCheck(w io.Writer, r CheckReport, precision float64) {
	pct := func(n, of int) string {
		if of == 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
	}
	fmt.Fprintf(w, "Model                        %s\n", r.Model)
	fmt.Fprintf(w, "Books                        %d\n", r.Books)
	fmt.Fprintf(w, "  with a vector              %d (%s)\n", r.Embedded, pct(r.Embedded, r.Books))
	fmt.Fprintf(w, "  with tags                  %d (%s)\n", r.Tagged, pct(r.Tagged, r.Books))
	fmt.Fprintf(w, "  with a genre from tags     %d (%s)\n", r.FromTags, pct(r.FromTags, r.Books))
	fmt.Fprintf(w, "Tagged books asked about     %d\n\n", r.Sample)

	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "Genre\tBooks from tags\t")
	for _, g := range List {
		fmt.Fprintf(t, "%s\t%d\t\n", g.Key, r.PerGenre[g.Key])
	}
	t.Flush()

	if len(r.Unmapped) > 0 {
		fmt.Fprintln(w, "\nTags of the most books that give no genre (add them to Spellings):")
		t = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, u := range r.Unmapped {
			fmt.Fprintf(t, "  %d\t%s\n", u.Books, u.Tag)
		}
		t.Flush()
	}

	for _, m := range r.Methods {
		fmt.Fprintf(w, "\n== %s\n", m.Method)
		fmt.Fprintf(w, "With the thresholds now: precision %.2f, recall %.2f, F1 %.2f, books given a genre %.0f%%\n",
			m.Current.Precision, m.Current.Recall, m.Current.F1, 100*m.Current.Covered)
		if len(m.Sweep) > 0 {
			fmt.Fprintf(w, "One threshold for every genre, at most %d a book:\n", MaxGenres)
			t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
			fmt.Fprintln(t, "threshold\tprecision\trecall\tF1\tgiven a genre\t")
			for _, p := range m.Sweep {
				fmt.Fprintf(t, "%.4f\t%.2f\t%.2f\t%.2f\t%.0f%%\t\n", p.Threshold, p.Precision, p.Recall, p.F1, 100*p.Covered)
			}
			t.Flush()
		}
		if len(m.Genres) > 0 {
			fmt.Fprintf(w, "Per genre, the threshold with the most recall at a precision of %.2f (* not reached: the best F1):\n", precision)
			t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
			fmt.Fprintln(t, "genre\tbooks\tthreshold\tprecision\trecall\tF1\t")
			for _, g := range m.Genres {
				mark := ""
				if !g.Reached {
					mark = "*"
				}
				fmt.Fprintf(t, "%s%s\t%d\t%.4f\t%.2f\t%.2f\t%.2f\t\n", mark, g.Genre, g.Positives, g.Threshold, g.Precision, g.Recall, g.F1)
			}
			t.Flush()
		}
	}
}
