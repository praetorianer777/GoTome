package metadata

import (
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// How much each part of a book counts towards a score. The title counts
// most; a matching author makes the difference between two books of one
// title; year and language settle editions.
const (
	weightTitle    = 0.55
	weightAuthors  = 0.30
	weightYear     = 0.10
	weightLanguage = 0.05
	// neutral is what a part counts for when one side says nothing about it.
	neutral = 0.5
	// bestWithoutIdentifier keeps every score short of what a shared
	// identifier gives: the same title and author can be another edition.
	bestWithoutIdentifier = 0.99
)

// Score says from 0 to 1 how well a record fits the book. A record that
// carries one of the book's identifiers is the book, and scores 1.
func Score(book catalog.Book, r Record) float64 {
	for _, mine := range book.Identifiers {
		for _, theirs := range r.Identifiers {
			if mine.Identifier == theirs {
				return 1
			}
		}
	}
	title := similarity(book.Title, r.Title)
	if r.Subtitle != "" {
		title = max(title, similarity(book.Title, r.Title+" "+r.Subtitle))
	}
	if book.Subtitle != "" {
		title = max(title, similarity(book.Title+" "+book.Subtitle, r.Title+" "+r.Subtitle))
	}
	score := weightTitle*title + weightAuthors*authorsMatch(book, r) +
		weightYear*yearsMatch(book.Published(), r.Published) + weightLanguage*languagesMatch(book.Language, r.Language)
	return min(score, bestWithoutIdentifier)
}

// similarity is the Dice coefficient of the two texts' words, compared as
// keys: 1 for the same words, 0 for none in common.
func similarity(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	common := 0
	for w := range wa {
		if wb[w] {
			common++
		}
	}
	return 2 * float64(common) / float64(len(wa)+len(wb))
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(catalog.Key(s)) {
		out[w] = true
	}
	return out
}

// authorsMatch is the share of the book's authors the record names too. Two
// names are one author when their keys are equal or, for "J. R. R. Tolkien"
// and "John Ronald Reuel Tolkien", their last words are.
func authorsMatch(book catalog.Book, r Record) float64 {
	mine := book.Authors()
	var theirs []string
	for _, c := range r.Contributors {
		if c.Role == "" || c.Role == catalog.RoleAuthor {
			theirs = append(theirs, c.Name)
		}
	}
	if len(mine) == 0 || len(theirs) == 0 {
		return neutral
	}
	matched := 0
	for _, m := range mine {
		for _, t := range theirs {
			if sameAuthor(m, t) {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(mine))
}

func sameAuthor(a, b string) bool {
	ka, kb := catalog.Key(a), catalog.Key(b)
	if ka == kb {
		return ka != ""
	}
	// "Austen, Jane" against "Jane Austen".
	if ka == catalog.Key(catalog.SortName(b)) || kb == catalog.Key(catalog.SortName(a)) {
		return true
	}
	fa, fb := strings.Fields(ka), strings.Fields(kb)
	return len(fa) > 1 && len(fb) > 1 && fa[len(fa)-1] == fb[len(fb)-1] && fa[0][0] == fb[0][0]
}

func yearsMatch(a, b string) float64 {
	ya, oka := year(a)
	yb, okb := year(b)
	switch {
	case !oka || !okb:
		return neutral
	case ya == yb:
		return 1
	case ya-yb <= 1 && yb-ya <= 1:
		return 0.5
	}
	return 0
}

func year(s string) (int, bool) {
	if len(s) < 4 {
		return 0, false
	}
	y, err := strconv.Atoi(s[:4])
	return y, err == nil
}

// languagesMatch compares the primary languages: en-GB and en are one.
func languagesMatch(a, b string) float64 {
	if a == "" || b == "" {
		return neutral
	}
	primary := func(s string) string {
		p, _, _ := strings.Cut(strings.ToLower(s), "-")
		return p
	}
	if primary(a) == primary(b) {
		return 1
	}
	return 0
}
