package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

// runShip trains the ensemble the app ships (#528) on the public corpus and
// writes it into backend/internal/genres/shipped: numbers only. The content
// vectors are those of #15 embedded with the app's prefix ("query: "), so
// the heads see what the app stores. With -eval it trains on four fifths and
// prints how the app's ensemble decides the fifth, and writes nothing.
func runShip(args []string) error {
	fs := flag.NewFlagSet("ship", flag.ExitOnError)
	gut := fs.String("gutenberg", "gutenberg.gob", "")
	vectors := fs.String("vectors", ".cache/embed/vectors-direct-fp32-16-query.gob", "16 vectors a book, prefix query:")
	out := fs.String("out", "backend/internal/genres/shipped", "")
	workers := fs.Int("workers", 3, "")
	eval := fs.Bool("eval", false, "measure on a held-out fifth instead of writing")
	_ = fs.Parse(args)
	recs, err := loadRecs(*gut)
	if err != nil {
		return err
	}
	vecs, err := loadPassageVectors(*vectors)
	if err != nil {
		return err
	}
	var keys []string
	for k := range genresWith(recs, minGenrePos) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var books []genres.TrainBook
	for _, r := range recs {
		b := genres.TrainBook{ID: r.ID, Lang: r.Lang, Counts: wordcount.Counts{}, Metadata: r.Meta, Labels: r.Labels}
		for w, n := range r.Terms {
			if n >= wordcount.MinCount {
				b.Counts[w] = uint32(n)
			}
		}
		var id int
		fmt.Sscan(r.ID, &id)
		if v := vecs[id]; len(v) > 0 {
			b.Content = embed.Mean(v, slices.Repeat([]float64{1}, len(v)))
		}
		books = append(books, b)
	}
	opt := genres.EnsembleOptions{Model: embed.E5Small.Model.Name, WordLangs: []string{"en"}, Workers: *workers}
	if *eval {
		return evalShip(recs, books, keys, opt)
	}
	e := genres.TrainEnsemble(books, keys, opt)
	words := e.Words["en"].Encode()
	body, err := e.Encode()
	if err != nil {
		return err
	}
	h := sha256.New()
	h.Write(body)
	h.Write(words)
	e.Version = "gutenberg-" + hex.EncodeToString(h.Sum(nil))[:12]
	if body, err = e.Encode(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "ensemble.json"), append(body, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "words-en.gwt"), words, 0o644); err != nil {
		return err
	}
	fmt.Println(e)
	for _, st := range e.Stackers {
		n := 0
		for _, t := range st.Thresholds {
			if t <= 1 {
				n++
			}
		}
		fmt.Printf("stacker %04b: %d books, %d of %d genres decide\n", st.Mask, st.Books, n, len(keys))
	}
	return nil
}

// evalShip trains on the books of four folds and decides the fifth as the
// app does, every input it has and each with only the content vector.
func evalShip(recs []Rec, books []genres.TrainBook, keys []string, opt genres.EnsembleOptions) error {
	var train, test []genres.TrainBook
	for i, b := range books {
		if foldOf(&recs[i], folds) == 0 {
			test = append(test, b)
		} else {
			train = append(train, b)
		}
	}
	e := genres.TrainEnsemble(train, keys, opt)
	var trainVecs []genres.Vectors
	var trainLabels [][]string
	for _, b := range train {
		if slices.ContainsFunc(b.Labels, func(l string) bool { return slices.Contains(keys, l) }) {
			trainVecs = append(trainVecs, genres.Vectors{Content: b.Content, Metadata: b.Metadata})
			trainLabels = append(trainLabels, b.Labels)
		}
	}
	for _, setup := range []string{"all", "content+metadata", "content"} {
		var tp, fp, fn, given, n int
		for _, b := range test {
			truth := slices.DeleteFunc(slices.Clone(b.Labels), func(l string) bool { return !slices.Contains(keys, l) })
			if len(truth) == 0 {
				continue
			}
			n++
			in := genres.Input{Model: opt.Model, Content: b.Content}
			switch setup {
			case "all":
				in.Lang, in.Counts, in.Metadata = b.Lang, b.Counts, b.Metadata
				in.Shares = genres.SharesAmong(genres.Vectors{Content: b.Content, Metadata: b.Metadata}, trainVecs, trainLabels)
			case "content+metadata":
				in.Metadata = b.Metadata
			}
			got, _ := e.Decide(e.Judge(in), nil, genres.MaxGenres)
			if len(got) > 0 {
				given++
			}
			for _, g := range got {
				if slices.Contains(truth, g.Genre) {
					tp++
				} else {
					fp++
				}
			}
			for _, t := range truth {
				if !slices.ContainsFunc(got, func(s genres.Scored) bool { return s.Genre == t }) {
					fn++
				}
			}
		}
		p, r, f := prf(tp, fp, fn)
		fmt.Printf("%-18s P %.2f R %.2f F1 %.2f given %.0f %% of %d\n", setup, p, r, f, 100*float64(given)/float64(n), n)
	}
	return nil
}
