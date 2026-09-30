package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
)

// langStats is what one language directory of a corpus holds.
type langStats struct {
	Lang   string
	Books  int
	Bytes  int64
	Median int64
}

// collectStats reads a corpus laid out as <dir>/<lang>/*.epub.
func collectStats(dir string) ([]langStats, error) {
	files, err := listEPUBs(dir)
	if err != nil {
		return nil, err
	}
	sizes := map[string][]int64{}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		lang := filepath.Base(filepath.Dir(file))
		sizes[lang] = append(sizes[lang], info.Size())
	}

	stats := make([]langStats, 0, len(sizes))
	for lang, list := range sizes {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		s := langStats{Lang: lang, Books: len(list), Median: list[len(list)/2]}
		for _, size := range list {
			s.Bytes += size
		}
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Lang < stats[j].Lang })
	return stats, nil
}

func printStats(w io.Writer, dir string) error {
	stats, err := collectStats(dir)
	if err != nil {
		return err
	}
	if len(stats) == 0 {
		return fmt.Errorf("no EPUBs under %s", dir)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "language\tbooks\ttotal MB\tmedian KB\t")
	total := langStats{Lang: "all"}
	for _, s := range stats {
		fmt.Fprintf(tw, "%s\t%d\t%.1f\t%d\t\n", s.Lang, s.Books, mb(s.Bytes), s.Median>>10)
		total.Books += s.Books
		total.Bytes += s.Bytes
	}
	fmt.Fprintf(tw, "%s\t%d\t%.1f\t\t\n", total.Lang, total.Books, mb(total.Bytes))
	return tw.Flush()
}

func mb(n int64) float64 { return float64(n) / (1 << 20) }
