package discover

import (
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// What a reason weighs before the person's taste is added: a signal the
// data states plainly comes before a guess from the vectors.
const (
	signalSeriesNext = 0.6
	signalSeriesGap  = 0.35
	signalSeriesMore = 0.2
	// signalAuthor is for an author whose best book is a 5 star one; the
	// weight of the person's taste (similar.DefaultTaste) scales it.
	signalAuthor = 0.5
	// signalGenre is the least a genre gives, and spread the share of
	// the person's taste it makes adds up to.
	signalGenre = 0.15
	spread      = 0.15
	// tasteShare is what a book wholly like the person's taste adds.
	tasteShare = 0.4
)

// signalOf is what the ask gives a book of the reason.
func signalOf(a ask, reason string) float64 {
	switch reason {
	case ReasonSeriesNext:
		return signalSeriesNext
	case ReasonSeriesGap:
		return signalSeriesGap
	case ReasonSeriesMore:
		return signalSeriesMore
	case ReasonAuthor:
		return signalAuthor * a.weight
	}
	return signalGenre + spread*a.share
}

// score blends the signal with how near the person's taste the book is, if
// that is known: a cosine under zero adds nothing.
func score(signal float64, cosine *float64) float64 {
	if cosine == nil {
		return signal
	}
	return signal + tasteShare*max(*cosine, 0)
}

// maxDescribed bounds the text a book is embedded from.
const maxDescribed = 600

// describe is what the taste profile is compared with: the title, the
// authors, the subjects and the start of the description, as a book's
// metadata is embedded.
func describe(r metadata.Record) string {
	parts := []string{r.Title}
	var authors []string
	for _, c := range r.Contributors {
		if c.Role == "" || c.Role == "author" {
			authors = append(authors, c.Name)
		}
	}
	if len(authors) > 0 {
		parts = append(parts, strings.Join(authors, ", "))
	}
	if r.Series != "" {
		parts = append(parts, r.Series)
	}
	if len(r.Tags) > 0 {
		parts = append(parts, strings.Join(r.Tags, ", "))
	}
	if d := strings.Join(strings.Fields(r.Description), " "); d != "" {
		parts = append(parts, string([]rune(d)[:min(len([]rune(d)), maxDescribed)]))
	}
	return strings.Join(parts, ". ")
}

// sameBook says whether two records are one book: editions of one work,
// or one title with an author in common.
func sameBook(a, b metadata.Record) bool {
	if a.Work != "" && a.Provider == b.Provider && a.Work == b.Work {
		return true
	}
	ka, kb := keysOf(a), keysOf(b)
	if !slices.ContainsFunc(ka.titles, func(k string) bool { return slices.Contains(kb.titles, k) }) {
		return false
	}
	if len(ka.authors) == 0 || len(kb.authors) == 0 {
		return true
	}
	return slices.ContainsFunc(ka.authors, func(k string) bool { return slices.Contains(kb.authors, k) })
}
