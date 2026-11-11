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
	fmt.Fprintf(w, "Looked up without it         %d, at %s\n", r.Sample, strings.Join(r.Providers, ", "))
	if r.Using != "" {
		fmt.Fprintf(w, "Score in use                 %s\n", r.Using)
	}
	fmt.Fprintln(w)

	tallies := append(append([]Tally{r.All}, r.PerProvider...), r.PerLanguage...)
	fmt.Fprintln(w, "The first candidate, by title, authors and language alone:")
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "ranking\tbooks\tright\tsame work\tedition\twrong\tnothing\tfailed\t")
	for _, x := range tallies {
		asked := x.Books - x.Failed
		fmt.Fprintf(t, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%d\t\n", x.Name, x.Books,
			pct(x.Right, asked), pct(x.SameWork, asked), pct(x.Edition, asked), pct(x.Wrong, asked), pct(x.None, asked), x.Failed)
	}
	t.Flush()

	fmt.Fprintln(w, "\nWhere the book stood when another edition or a wrong one was first (right or same work):")
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	header := "ranking\tnot it first\t"
	for _, b := range rankBands {
		header += b.name + "\t"
	}
	fmt.Fprintln(t, header+"not found\t")
	for _, x := range tallies {
		line := fmt.Sprintf("%s\t%d\t", x.Name, x.Edition+x.Wrong)
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
	fmt.Fprintln(t, "score\tright\tsame work\tedition\twrong\t")
	for _, b := range r.Scores {
		fmt.Fprintf(t, "%.2f-%.2f\t%d\t%d\t%d\t%d\t\n", b.From, b.To, b.Right, b.SameWork, b.Edition, b.Wrong)
	}
	t.Flush()

	fmt.Fprintf(w, "\nTaken at a threshold (all; * metadata.matchThreshold now, %.2f):\n", r.Threshold)
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "threshold\ttaken\tright\tsame work\tedition\twrong\tright or same work\t")
	for _, p := range r.Thresholds {
		mark := ""
		if p.Threshold == r.Threshold {
			mark = "*"
		}
		fmt.Fprintf(t, "%s%.2f\t%d\t%d\t%d\t%d\t%d\t%s\t\n", mark, p.Threshold, p.Taken, p.Right, p.SameWork, p.Edition, p.Wrong,
			pct(p.Right+p.SameWork, p.Taken))
	}
	t.Flush()

	a := r.Agreement
	fmt.Fprintln(w, "\nTaken by the rule of an automatic run (two providers' first candidates name one ISBN or one work):")
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "books\taccepted\tright\tsame work\tedition\twrong\tright or same work\t")
	fmt.Fprintf(t, "%d\t%d\t%d\t%d\t%d\t%d\t%s\t\n", r.Sample, a.Accepted, a.Right, a.SameWork, a.Edition, a.Wrong, pct(a.Right+a.SameWork, a.Accepted))
	t.Flush()

	e := r.Editions
	fmt.Fprintln(w, "\nTaken as well with curation.matches.edition on (one provider's first candidate has the book's title, an author and its publisher):")
	t = tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "books\taccepted\tright\tsame work\tedition\twrong\tright or same work\t")
	fmt.Fprintf(t, "%d\t%d\t%d\t%d\t%d\t%d\t%s\t\n", r.Sample, e.Accepted, e.Right, e.SameWork, e.Edition, e.Wrong, pct(e.Right+e.SameWork, e.Accepted))
	t.Flush()
	if len(r.Variants) > 1 {
		printVariants(w, r.Variants, r.Threshold)
	}
}

// printVariants writes the hand-set and the learned scores side by side,
// and each one's calibration: the score it predicts against the share found.
func printVariants(w io.Writer, variants []Variant, threshold float64) {
	pct := func(n, of int) string {
		if of == 0 {
			return "-"
		}
		return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(of))
	}
	fmt.Fprintf(w, "\nThe same books ranked by each score (all; taken at metadata.matchThreshold, %.2f):\n", threshold)
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "score\tright first\tsame work\tedition\twrong\tnothing\ttaken\tof them right\tof them wrong\t")
	for _, v := range variants {
		asked := v.All.Books - v.All.Failed
		var taken ThresholdPoint
		for _, p := range v.Thresholds {
			if p.Threshold == threshold {
				taken = p
			}
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t\n", v.Name,
			pct(v.All.Right, asked), pct(v.All.SameWork, asked), pct(v.All.Edition, asked), pct(v.All.Wrong, asked), pct(v.All.None, asked),
			taken.Taken, pct(taken.Right, taken.Taken), pct(taken.Wrong, taken.Taken))
	}
	t.Flush()
	for _, v := range variants {
		fmt.Fprintf(w, "\nCalibration of %s (first candidates; found is the share carrying one of the book's ISBNs):\n", v.Name)
		t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(t, "score\tcandidates\tpredicted\tfound\t")
		for _, b := range v.Calibration {
			if b.Count == 0 {
				continue
			}
			fmt.Fprintf(t, "%.2f-%.2f\t%d\t%.0f%%\t%.0f%%\t\n", b.From, b.To, b.Count, 100*b.Predicted, 100*b.Found)
		}
		t.Flush()
	}
}

// PrintTrain writes a learning's report as tables a person reads.
func PrintTrain(w io.Writer, r TrainReport) {
	fmt.Fprintf(w, "Books with an ISBN           %d\n", r.Books)
	fmt.Fprintf(w, "Looked up without it         %d, at %s (%d to fit, %d held out)\n", r.Sample, strings.Join(r.Providers, ", "), r.TrainBooks, r.TestBooks)
	fmt.Fprintf(w, "Candidates                   %d, of which %d carry one of the book's ISBNs\n", r.Candidates, r.Right)
	fmt.Fprintf(w, "Log loss                     %.3f fitted on, %.3f held out\n\n", r.LogLossTrain, r.LogLossTest)

	fmt.Fprintln(w, "Weights, hand-set beside learned:")
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(t, "feature\thand-set\tlearned\t")
	for _, row := range r.Weights {
		hand := "-"
		if row.HandSet != nil {
			hand = fmt.Sprintf("%.2f", *row.HandSet)
		}
		fmt.Fprintf(t, "%s\t%s\t%.2f\t\n", row.Feature, hand, row.Learned)
	}
	t.Flush()

	fmt.Fprintln(w, "\nOn the books held out:")
	printVariants(w, []Variant{r.HandSet, r.Learned}, r.Threshold)
}
