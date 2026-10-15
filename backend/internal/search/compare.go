package search

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
)

// CompareQueries are the queries of the search decision's benchmark
// (spikes/search): words in English, German and French, phrases, and typos.
var CompareQueries = []string{
	"whales harpooned", "running horses", "ancient temples ruins", "detective murdered",
	"love letters", "Häuser Flüsse", "verlorene Liebe", "Kinder spielten", "maison jardin",
	"morning", `"white whale"`, `"once upon a time"`, `"in the morning"`, `"der alte Mann"`,
	`"es war einmal"`, "detectve", "whael harpon", "Kinnder", "elefant", "jardn",
}

// Comparison is how two Searchers answered one query: the first page of
// books each found, and how long each took.
type Comparison struct {
	Query string
	// A and B are the books each found, best first.
	A, B []uuid.UUID
	// Shared is how many books are on both pages.
	Shared         int
	TimeA, TimeB   time.Duration
	ErrorA, ErrorB error
}

// Compare asks both Searchers each query as the scope, a page of limit
// books, for comparing one engine with another on a library (#265).
func Compare(ctx context.Context, a, b Searcher, scope library.Scope, queries []string, limit int) []Comparison {
	var out []Comparison
	for _, text := range queries {
		c := Comparison{Query: text}
		c.A, c.TimeA, c.ErrorA = page(ctx, a, scope, text, limit)
		c.B, c.TimeB, c.ErrorB = page(ctx, b, scope, text, limit)
		inA := map[uuid.UUID]bool{}
		for _, id := range c.A {
			inA[id] = true
		}
		for _, id := range c.B {
			if inA[id] {
				c.Shared++
			}
		}
		out = append(out, c)
	}
	return out
}

func page(ctx context.Context, s Searcher, scope library.Scope, text string, limit int) ([]uuid.UUID, time.Duration, error) {
	start := time.Now()
	res, err := s.Search(ctx, scope, Query{Text: text, Limit: limit})
	took := time.Since(start)
	var ids []uuid.UUID
	for _, b := range res.Books {
		ids = append(ids, b.ID)
	}
	return ids, took, err
}
