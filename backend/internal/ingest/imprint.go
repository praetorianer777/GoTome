package ingest

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// imprintEdge is how many documents (or pages) from each end of a book are
// looked through: the imprint is on the back of the title page, or at the end.
const imprintEdge = 6

// advertisedISBNs is the most ISBNs a document may name and still be the
// book's own imprint; more is a list of other titles.
const advertisedISBNs = 4

var (
	isbnCandidate = regexp.MustCompile(`(?:97[89](?:[\s\-‐‑–]?\d){10}|\d(?:[\s\-‐‑–]?\d){8}[\s\-‐‑–]?[\dXx])`)
	isbnWord      = regexp.MustCompile(`(?i)isbn`)
	ebookLabel    = regexp.MustCompile(`(?i)e-?\s?book|e-?pub|e-?isbn|digital`)
	printLabel    = regexp.MustCompile(`(?i)print|taschenbuch|gebunden|hardcover|hardback|paperback|softcover|broschiert|broschur`)

	copyrightYear = regexp.MustCompile(`(?i)(?:©|\(c\)|copyright)\s*(?:by\s+)?((?:1[5-9]|20)\d\d)\b`)
	editionYear   = regexp.MustCompile(`(?i)\d+\.\s*(?:auflage|aufl\.)[^\n\d]{0,20}((?:1[5-9]|20)\d\d)\b|first (?:published|edition)[^\n\d]{0,40}((?:1[5-9]|20)\d\d)\b|erstausgabe[^\n\d]{0,20}((?:1[5-9]|20)\d\d)\b`)

	publisherAfterYear = regexp.MustCompile(`(?i)(?:©|\(c\)|copyright)\s*(?:by\s+)?(?:1[5-9]|20)\d\d\s+(?:by\s+)?([^\n]{3,80})`)
	publisherLabelled  = regexp.MustCompile(`(?i)(?:published by|publisher:?|verlag:|erschienen\s+(?:im|bei)|herausgegeben\s+von)\s+([^\n]{3,80})`)
	publisherWord      = regexp.MustCompile(`(?i)\b(?:\w*verlag\w*|press|books|publish\w*|editions?|edizioni|libros|media|publications|house|gmbh|ag|ltd|llc|inc)\b|éditions`)
	publisherEnd       = regexp.MustCompile(`(?i)\s*(?:[.,;(]|\d|alle rechte|all rights|www\.|http|isbn).*$`)
)

// ImprintOf reads the ISBN, the publisher and the year a book prints about
// itself from the first and last documents of its text. It only reports what
// it is sure of; a field it cannot tell is left empty.
func ImprintOf(sections []Section) catalog.Imprint {
	if len(sections) == 0 {
		return catalog.Imprint{}
	}
	head, tail := sections, []Section(nil)
	if len(sections) > 2*imprintEdge {
		head, tail = sections[:imprintEdge], sections[len(sections)-imprintEdge:]
	}
	var imprint catalog.Imprint
	imprint.ISBN = isbnOf(head, false)
	if imprint.ISBN == "" {
		imprint.ISBN = isbnOf(tail, true)
	}
	for _, group := range [][]Section{head, tail} {
		for _, s := range group {
			if imprint.Year == "" {
				imprint.Year = yearIn(s.Text)
			}
			if imprint.Publisher == "" {
				imprint.Publisher = publisherIn(s.Text)
			}
		}
	}
	return imprint
}

type isbnFound struct {
	value string
	kind  int
}

const (
	isbnPlain = iota
	isbnEbook
	isbnPrint
)

// isbnOf picks the book's own ISBN among those the documents print after the
// word ISBN: an e-book's first, then an unlabelled one, then a print
// edition's. A document that names many is a list of other titles. When
// alone is set, only a single ISBN counts, as at the end of a book it is as
// likely an advertisement as the imprint.
func isbnOf(group []Section, alone bool) string {
	var found []isbnFound
	for _, s := range group {
		found = append(found, isbnsIn(s.Text)...)
	}
	distinct := map[string]bool{}
	for _, f := range found {
		distinct[f.value] = true
	}
	if len(distinct) == 0 || (alone && len(distinct) > 1) {
		return ""
	}
	for _, kind := range []int{isbnEbook, isbnPlain, isbnPrint} {
		for _, f := range found {
			if f.kind == kind {
				return f.value
			}
		}
	}
	return ""
}

func isbnsIn(text string) []isbnFound {
	var found []isbnFound
	seen := map[string]bool{}
	floor := 0
	for _, loc := range isbnCandidate.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && isDigitOrX(text[start-1]) || end < len(text) && isDigitOrX(text[end]) {
			continue
		}
		id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, text[start:end])
		if !ok {
			continue
		}
		lo := min(max(floor, start-40), start)
		words := isbnWord.FindAllStringIndex(text[lo:start], -1)
		if len(words) == 0 {
			continue
		}
		label := min(max(lo+words[len(words)-1][0]-14, floor), start)
		before := text[label:start]
		if nl := strings.LastIndexByte(before, '\n'); nl >= 0 {
			before = before[nl:]
		}
		after := text[end:min(len(text), end+24)]
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			after = after[:nl]
		}
		if w := isbnWord.FindStringIndex(after); w != nil {
			after = after[:w[0]]
		}
		floor = end + len(after)
		if seen[id.Value] {
			continue
		}
		seen[id.Value] = true
		label2 := before + " " + after
		kind := isbnPlain
		switch {
		case ebookLabel.MatchString(label2):
			kind = isbnEbook
		case printLabel.MatchString(label2):
			kind = isbnPrint
		}
		found = append(found, isbnFound{id.Value, kind})
	}
	if len(found) > advertisedISBNs {
		return nil
	}
	return found
}

func isDigitOrX(b byte) bool { return b >= '0' && b <= '9' || b == 'X' || b == 'x' }

func yearIn(text string) string {
	if m := copyrightYear.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if m := editionYear.FindStringSubmatch(text); m != nil {
		return m[1] + m[2] + m[3]
	}
	return ""
}

// publisherIn reads a publisher's name only where it carries a word that
// makes it one: a name after the copyright sign is as often the author's.
func publisherIn(text string) string {
	for _, re := range []*regexp.Regexp{publisherAfterYear, publisherLabelled} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			name := strings.TrimSpace(publisherEnd.ReplaceAllString(m[1], ""))
			if words := strings.Fields(name); len(words) >= 1 && len(words) <= 5 &&
				utf8.RuneCountInString(name) <= 60 && publisherWord.MatchString(name) {
				return name
			}
		}
	}
	return ""
}
