// Package search finds books by the words of their text: the chunks
// internal/textproc cuts and extraction stores, through the BM25 index the
// search decision chose (docs/decisions/search-engine.md).
package search

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// ErrEmptyQuery is a query without a word to look for.
var ErrEmptyQuery = errors.New("the query has no word to look for")

// MaxLimit is the most books one page of results holds.
const MaxLimit = 50

// Query is what to look for, and where.
type Query struct {
	// Text is the words, all of which a chunk must hold in any form, and
	// phrases in double quotes, which it must hold in order.
	Text string
	// LibraryID limits the search to one library; nil is every library the
	// caller may see.
	LibraryID *uuid.UUID
	// Filter is the rule tree of the book lists, which the books found must
	// match.
	Filter filter.Node
	// Limit and Offset cut the books found into pages.
	Limit, Offset int
}

// Result is a page of the books found, best first.
type Result struct {
	Books []Book
	// More says there are books after this page.
	More bool
}

// Book is a book found, with its best passages.
type Book struct {
	ID    uuid.UUID
	Score float64
	Hits  []Hit
}

// Hit is a passage of a book that matched.
type Hit struct {
	ChunkID int64
	FileID  uuid.UUID
	// Position is the chunk's among the book's, from 0.
	Position int
	Chapter  string
	// PageFrom and PageTo are 0 when the pages are not known.
	PageFrom, PageTo int
	// Offset is where the chunk starts in the book's text, in characters.
	Offset  int
	Score   float64
	Snippet []Part
}

// Part is a piece of a snippet: text as it is, and whether it is a match.
type Part struct {
	Text  string
	Match bool
}

// Searcher finds books by their text. What a scope may not see is never
// found.
type Searcher interface {
	Search(ctx context.Context, scope library.Scope, q Query) (Result, error)
}

// parsed is a query's words and phrases.
type parsed struct {
	// Words are the words outside quotes, joined by spaces; "" when there
	// are none.
	Words string
	// Phrases are the quoted ones that hold a word.
	Phrases []string
}

// parse splits the query into its free words and its phrases. A quote left
// open runs to the end.
func parse(text string) (parsed, error) {
	var p parsed
	var words []string
	for i, part := range strings.Split(text, `"`) {
		part = strings.Join(strings.Fields(part), " ")
		if !hasWord(part) {
			continue
		}
		if i%2 == 1 {
			p.Phrases = append(p.Phrases, part)
		} else {
			words = append(words, part)
		}
	}
	p.Words = strings.Join(words, " ")
	if p.Words == "" && len(p.Phrases) == 0 {
		return p, ErrEmptyQuery
	}
	return p, nil
}

func hasWord(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}
