package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
)

// ShippedReport is what a default model trained on the public corpus would
// weigh and what it would say, for the decision record.
type ShippedReport struct {
	Genres     []string                       `json:"genres"`
	TrainBooks int                            `json:"trainBooks"`
	Languages  map[string]LangSize            `json:"languages"`
	TopTerms   map[string]map[string][]string `json:"topTerms"`
	HeadBytes  map[string]int                 `json:"embeddingHeadBytesFP32"`
	HeadGenres int                            `json:"embeddingHeadGenres"`
}

// LangSize is the term table of one language.
type LangSize struct {
	Docs, Vocab, TrainedGenres int
	// FullFP16Bytes stores every weight of every trained genre as 16 bits.
	FullFP16Bytes int
	// PrunedBytes keeps tfidfPruneTop terms per genre as a 32-bit term ID
	// and an 8-bit weight, plus the IDF of the kept terms (8 bits) and the
	// terms themselves (average length times the distinct kept terms).
	PrunedBytes int
	PrunedTerms int
}

func runShipped(args []string) error {
	fs := flag.NewFlagSet("shipped", flag.ExitOnError)
	gut := fs.String("gutenberg", "gutenberg.gob", "")
	out := fs.String("out", "results/shipped.json", "")
	workers := fs.Int("workers", 3, "")
	_ = fs.Parse(args)
	g, err := loadRecs(*gut)
	if err != nil {
		return err
	}
	var genres []string
	for k := range genresWith(g, minGenrePos) {
		genres = append(genres, k)
	}
	sort.Strings(genres)
	docs := labelled(g, genres)
	m := trainTFIDF(docs, genres, *workers).(*tfidfModel)
	rep := ShippedReport{Genres: genres, TrainBooks: len(docs), Languages: map[string]LangSize{},
		TopTerms: map[string]map[string][]string{}, HeadBytes: map[string]int{"multilingual-e5-small": 385 * 4 * len(genres), "bge-m3": 1025 * 4 * len(genres)},
		HeadGenres: len(genres)}
	for lang, lm := range m.langs {
		terms := make([]string, len(lm.vocab))
		for t, i := range lm.vocab {
			terms[i] = t
		}
		ls := LangSize{Vocab: len(lm.vocab)}
		kept := map[int]bool{}
		rep.TopTerms[lang] = map[string][]string{}
		for gi, w := range lm.w {
			if w == nil {
				continue
			}
			ls.TrainedGenres++
			ls.FullFP16Bytes += 2 * (len(w) - 1)
			idx := make([]int, len(w)-1)
			for i := range idx {
				idx[i] = i
			}
			sort.Slice(idx, func(a, b int) bool { return w[idx[a]] > w[idx[b]] })
			var top []string
			for _, i := range idx[:12] {
				top = append(top, terms[i])
			}
			rep.TopTerms[lang][genres[gi]] = top
			for _, i := range idx[:tfidfPruneTop] {
				kept[i] = true
			}
			ls.PrunedBytes += 5 * tfidfPruneTop
		}
		var chars int
		for i := range kept {
			chars += len(terms[i]) + 1
		}
		ls.PrunedTerms = len(kept)
		ls.PrunedBytes += len(kept) + chars
		for _, d := range docs {
			if d.Lang == lang {
				ls.Docs++
			}
		}
		rep.Languages[lang] = ls
	}
	b, _ := json.MarshalIndent(rep, "", " ")
	fmt.Println(string(b))
	return os.WriteFile(*out, b, 0o644)
}
