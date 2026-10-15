package search

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

const (
	// window is how many of the best chunks a search takes from the index
	// before they are grouped into books; pages are cut from those.
	window = 1000
	// hitsPerBook is how many passages a book found shows.
	hitsPerBook = 3
	// snippetChars is how long a snippet is, about.
	snippetChars = 200
)

type args struct{ values []any }

func (a *args) add(v any) string {
	a.values = append(a.values, v)
	return fmt.Sprintf("$%d", len(a.values))
}

type found struct {
	id     int64
	bookID uuid.UUID
	score  float64
}

// reading is the best chunks one reading of the query found, and the
// reading, which the snippets are made with.
type reading struct {
	chunks    []found
	query     parsed
	corrected bool
}

// where is what a search looks among: the libraries the scope sees, of
// the query's when it names one, and the books of its filter when the
// filter picks few enough to name; nil books when it has none or picks too
// many, whose conditions the outer query keeps.
func where(ctx context.Context, tx pgx.Tx, books *catalog.Service, scope library.Scope, q Query) ([]uuid.UUID, []uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT visible_library_ids($1, $2)", scope.Viewer, scope.SeesAll)
	if err != nil {
		return nil, nil, err
	}
	libs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, nil, err
	}
	if q.LibraryID != nil {
		if !slices.Contains(libs, *q.LibraryID) {
			return nil, nil, nil
		}
		libs = []uuid.UUID{*q.LibraryID}
	}
	if len(libs) == 0 || matchesAll(q.Filter) {
		return libs, nil, nil
	}
	ids, err := books.Select(ctx, scope, q.LibraryID, q.Filter, nil)
	switch {
	case errors.Is(err, catalog.ErrTooMany):
		// Too many to name; the outer conditions keep them apart.
		return libs, nil, nil
	case err != nil:
		return nil, nil, err
	case len(ids) == 0:
		return libs, []uuid.UUID{}, nil
	}
	return libs, ids, nil
}

// passagesFunc reads the chunks of ids with a snippet of each around what
// the query matches.
type passagesFunc func(ctx context.Context, tx pgx.Tx, ids []int64, p parsed) (map[int64]Hit, error)

// group makes books of the readings' chunks, the first reading's books
// first, each in the order of its best chunk, and reads the passages of the
// page's books. A book a reading before found is not found again.
func group(ctx context.Context, tx pgx.Tx, readings []reading, limit, offset int, passages passagesFunc) (Result, error) {
	type entry struct {
		id      uuid.UUID
		reading int
		chunks  []found
	}
	var order []*entry
	byBook := map[uuid.UUID]*entry{}
	for r, rd := range readings {
		for _, c := range rd.chunks {
			e := byBook[c.bookID]
			if e == nil {
				e = &entry{id: c.bookID, reading: r}
				byBook[c.bookID] = e
				order = append(order, e)
			}
			if e.reading == r && len(e.chunks) < hitsPerBook {
				e.chunks = append(e.chunks, c)
			}
		}
	}
	res := Result{More: len(order) > offset+limit}
	if offset >= len(order) {
		return res, nil
	}
	page := order[offset:min(offset+limit, len(order))]
	hits := map[int64]Hit{}
	for r, rd := range readings {
		var ids []int64
		for _, e := range page {
			if e.reading == r {
				for _, c := range e.chunks {
					ids = append(ids, c.id)
				}
			}
		}
		if len(ids) == 0 {
			continue
		}
		got, err := passages(ctx, tx, ids, rd.query)
		if err != nil {
			return Result{}, err
		}
		maps.Copy(hits, got)
	}
	for _, e := range page {
		book := Book{ID: e.id, Score: e.chunks[0].score, Corrected: readings[e.reading].corrected}
		for _, c := range e.chunks {
			if h, ok := hits[c.id]; ok {
				h.Score = c.score
				book.Hits = append(book.Hits, h)
			}
		}
		res.Books = append(res.Books, book)
	}
	return res, nil
}

func distinctBooks(chunks []found) int {
	seen := map[uuid.UUID]bool{}
	for _, c := range chunks {
		seen[c.bookID] = true
	}
	return len(seen)
}

// matchesAll says the tree has no rule: an All without rules, which every
// book matches.
func matchesAll(n filter.Node) bool {
	return n.Field == "" && n.Not == nil && len(n.Any) == 0 && len(n.All) == 0
}
