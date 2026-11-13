package main

import (
	"flag"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

// runPort checks the port of the word weights into internal/genres (#526)
// against the spike's own: both are trained on the same folds of the public
// corpus and judged at the best single threshold, top three genres a book,
// micro-averaged, as the record's last column is.
func runPort(args []string) error {
	fs := flag.NewFlagSet("port", flag.ExitOnError)
	chunks := fs.String("chunks", ".cache/corpus/chunks.jsonl.gz", "the chunk file")
	workers := fs.Int("workers", 3, "")
	_ = fs.Parse(args)

	var recs []Rec
	var counted []wordcount.Counts
	var countNS int64
	err := readBooks(*chunks, func(b Book) error {
		r := Rec{ID: fmt.Sprint(b.ID), Group: fmt.Sprint(b.ID), Lang: b.Lang, Labels: LabelsOf(b.Subjects), Terms: map[string]uint16{}}
		for _, c := range b.Chunks {
			countTerms(r.Terms, c)
		}
		t := time.Now()
		counted = append(counted, wordcount.Count(b.Chunks))
		countNS += time.Since(t).Nanoseconds()
		recs = append(recs, r)
		return nil
	})
	if err != nil {
		return err
	}
	var genreList []string
	for k := range genresWith(recs, minGenrePos) {
		genreList = append(genreList, k)
	}
	sort.Strings(genreList)
	var docs []*Rec
	var cs []wordcount.Counts
	for i := range recs {
		var ls []string
		for _, l := range recs[i].Labels {
			if contains(genreList, l) {
				ls = append(ls, l)
			}
		}
		if len(ls) > 0 && len(recs[i].Terms) > 0 {
			c := recs[i]
			c.Labels = ls
			docs = append(docs, &c)
			cs = append(cs, counted[i])
		}
	}
	fmt.Printf("%d books with one of %d genres; port counts %.1f ms a book\n", len(docs), len(genreList), float64(countNS)/float64(len(recs))/1e6)

	const folds = 5
	old := newScores(len(docs), len(genreList))
	port := newScores(len(docs), len(genreList))
	for k := range folds {
		var train, test []*Rec
		var trainBooks []genres.WordBook
		var testIdx []int
		for i, d := range docs {
			if foldOf(d, folds) == k {
				test = append(test, d)
				testIdx = append(testIdx, i)
				continue
			}
			train = append(train, d)
			trainBooks = append(trainBooks, genres.WordBook{ID: d.ID, Counts: cs[i], Labels: d.Labels})
		}
		m := trainTFIDF(train, genreList, *workers)
		sc := m.Predict(test)["tfidf"]
		for j, i := range testIdx {
			copy(old[i], sc[j])
		}
		for lang, group := range byLang(train, trainBooks) {
			tab := genres.TrainWords(lang, group, genreList, genres.WordOptions{Workers: *workers})
			if tab == nil {
				continue
			}
			for _, i := range testIdx {
				if docs[i].Lang == lang {
					copy(port[i], tab.Score(cs[i]))
				}
			}
		}
	}
	y := labelMatrix(docs, genreList)
	for _, row := range []struct {
		name string
		s    Scores
	}{{"spike", old}, {"port", port}} {
		bestF, bestT, bp, br := 0.0, 0.0, 0.0, 0.0
		for t := 0.05; t < 0.96; t += 0.05 {
			dec := decide(row.s, uniformThreshold(len(genreList), t))
			var tl Tally
			tl.add(dec, y, genreList)
			p, r, f := prf(tl.TP, tl.FP, tl.FN)
			if !math.IsNaN(f) && f > bestF {
				bestF, bestT, bp, br = f, t, p, r
			}
		}
		fmt.Printf("%-6s best single threshold %.2f: F1 %.3f (P %.3f, R %.3f)\n", row.name, bestT, bestF, bp, br)
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func byLang(train []*Rec, books []genres.WordBook) map[string][]genres.WordBook {
	out := map[string][]genres.WordBook{}
	for i, d := range train {
		out[d.Lang] = append(out[d.Lang], books[i])
	}
	return out
}
