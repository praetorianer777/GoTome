package main

import (
	"encoding/gob"
	"os"
	"strings"
	"unicode"
)

// Rec is everything the methods need to know of one book, made once.
type Rec struct {
	ID string
	// Group keeps books of one folder (or one book) in one fold.
	Group  string
	Lang   string
	Labels []string
	// Terms are the lower-cased words of the whole text and how often each
	// occurs.
	Terms map[string]uint16
	// Vecs are the vectors of the sampled passages (prefix "passage: ").
	Vecs [][]float32
	// Meta is the vector of the title, authors and, where the file has one,
	// the description (prefix "query: ").
	Meta    []float32
	HasDesc bool
	// Chars is the text's length, Chunks its number of 8,000-character chunks.
	Chars, Chunks int
	// Tagged says the file carries any subject at all.
	Tagged bool
	// Costs of making the record, in nanoseconds.
	ParseNS, TokenizeNS, EmbedNS int64
}

func saveRecs(path string, recs []Rec) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := gob.NewEncoder(f).Encode(recs); err != nil {
		return err
	}
	return f.Close()
}

func loadRecs(path string) ([]Rec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []Rec
	return recs, gob.NewDecoder(f).Decode(&recs)
}

// countTerms adds the words of text to m.
func countTerms(m map[string]uint16, text string) {
	for w := range strings.FieldsFuncSeq(text, func(r rune) bool { return !unicode.IsLetter(r) }) {
		n := len([]rune(w))
		if n < 3 || n > 25 {
			continue
		}
		w = strings.ToLower(w)
		if m[w] < 65535 {
			m[w]++
		}
	}
}

func pickEven[T any](xs []T, k int) []T {
	if len(xs) <= k {
		return xs
	}
	out := make([]T, k)
	for i := range out {
		out[i] = xs[(2*i+1)*len(xs)/(2*k)]
	}
	return out
}
