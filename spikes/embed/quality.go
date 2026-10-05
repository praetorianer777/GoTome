package main

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// volumeMarker matches what tells volumes of one work apart in a title:
// "Vol. 2", "Zweiter Band", "(of 3)", a trailing number.
var volumeMarker = regexp.MustCompile(`(?i)(\b(vol(ume)?|band|teil|part|tome|book|buch|bd)\.?\s*[ivxlc\d]+\b|\b(erster|zweiter|dritter|vierter|fünfter|first|second|third|fourth|premier|deuxième|troisième)\s+(band|teil|part|volume|tome)\b|\(of \d+\)|\d+\s*$)`)

// workKey is a book's title without its volume and with its authors: two
// books with the same key are volumes of one work.
func workKey(b Book) string {
	t := strings.ToLower(volumeMarker.ReplaceAllString(b.Title, ""))
	t = strings.Join(strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
	a := slices.Clone(b.Authors)
	slices.Sort(a)
	return t + "|" + strings.Join(a, ";")
}

// Quality is how well the nearest neighbours of a book vector find the books
// related to it.
type Quality struct {
	// Volumes: books with another volume of their work in the corpus.
	Volumes Ranking `json:"volumes"`
	// Authors: books whose author has another book in the corpus.
	Authors Ranking `json:"authors"`
	// SubjectPrecision is the share of the ten nearest books that share a
	// subject heading with the book; SubjectBaseline that share among all
	// pairs of books.
	SubjectPrecision float64 `json:"subjectPrecision"`
	SubjectBaseline  float64 `json:"subjectBaseline"`
	// SameLanguage is the share of the ten nearest books in the book's own
	// language.
	SameLanguage float64 `json:"sameLanguage"`
}

// Ranking says where the nearest related book stood among all others.
type Ranking struct {
	Books   int     `json:"books"`
	At1     float64 `json:"recallAt1"`
	At10    float64 `json:"recallAt10"`
	MRR     float64 `json:"mrr"`
	Related string  `json:"related"`
}

// Measure ranks every book's neighbours by cosine of the vectors, which are
// normalised.
func Measure(books []Book, vectors [][]float32) Quality {
	n := len(books)
	works := map[string][]int{}
	byAuthor := map[string][]int{}
	for i, b := range books {
		works[workKey(b)] = append(works[workKey(b)], i)
		for _, a := range b.Authors {
			byAuthor[a] = append(byAuthor[a], i)
		}
	}
	sameWork := func(i, j int) bool { return workKey(books[i]) == workKey(books[j]) }
	sameAuthor := func(i, j int) bool {
		for _, a := range books[i].Authors {
			if slices.Contains(books[j].Authors, a) {
				return true
			}
		}
		return false
	}
	sharesSubject := func(i, j int) bool {
		for _, s := range books[i].Subjects {
			if slices.Contains(books[j].Subjects, s) {
				return true
			}
		}
		return false
	}

	var q Quality
	vol := rankAcc{name: "another volume of the same work"}
	auth := rankAcc{name: "another book by the same author"}
	var subjHits, langHits, top int
	order := make([]int, 0, n)
	scores := make([]float64, n)
	for i := range books {
		for j := range books {
			scores[j] = dot(vectors[i], vectors[j])
		}
		order = order[:0]
		for j := range books {
			if j != i {
				order = append(order, j)
			}
		}
		slices.SortFunc(order, func(a, b int) int {
			switch {
			case scores[a] > scores[b]:
				return -1
			case scores[a] < scores[b]:
				return 1
			}
			return a - b
		})
		if len(works[workKey(books[i])]) > 1 {
			vol.add(order, func(j int) bool { return sameWork(i, j) })
		}
		if hasOther(byAuthor, books[i].Authors) {
			auth.add(order, func(j int) bool { return sameAuthor(i, j) && !sameWork(i, j) })
		}
		for _, j := range order[:min(10, len(order))] {
			top++
			if sharesSubject(i, j) {
				subjHits++
			}
			if books[j].Lang == books[i].Lang {
				langHits++
			}
		}
	}
	q.Volumes, q.Authors = vol.result(), auth.result()
	q.SubjectPrecision = float64(subjHits) / float64(top)
	q.SameLanguage = float64(langHits) / float64(top)
	var pairs, shared int
	for i := range books {
		for j := i + 1; j < n; j++ {
			pairs++
			if sharesSubject(i, j) {
				shared++
			}
		}
	}
	q.SubjectBaseline = float64(shared) / float64(pairs)
	return q
}

func hasOther(byAuthor map[string][]int, authors []string) bool {
	for _, a := range authors {
		if len(byAuthor[a]) > 1 {
			return true
		}
	}
	return false
}

type rankAcc struct {
	name           string
	books          int
	at1, at10, mrr float64
}

// add records the rank of the first related book in order, if there is one.
func (r *rankAcc) add(order []int, related func(int) bool) {
	for rank, j := range order {
		if related(j) {
			r.books++
			if rank == 0 {
				r.at1++
			}
			if rank < 10 {
				r.at10++
			}
			r.mrr += 1 / float64(rank+1)
			return
		}
	}
}

func (r *rankAcc) result() Ranking {
	if r.books == 0 {
		return Ranking{Related: r.name}
	}
	b := float64(r.books)
	return Ranking{Books: r.books, At1: r.at1 / b, At10: r.at10 / b, MRR: r.mrr / b, Related: r.name}
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// BookVector is the normalised mean of a book's sample vectors.
func BookVector(samples [][]float32) []float32 {
	if len(samples) == 0 {
		return nil
	}
	out := make([]float32, len(samples[0]))
	for _, v := range samples {
		for i, x := range v {
			out[i] += x
		}
	}
	var norm float64
	for _, x := range out {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	for i := range out {
		out[i] = float32(float64(out[i]) / norm)
	}
	return out
}
