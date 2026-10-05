package search

import (
	"context"
	"errors"
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
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
	// The markers a snippet's matches are wrapped in. textproc.Normalize
	// takes control characters out of the text, so neither occurs in it.
	matchStart = "\x02"
	matchEnd   = "\x03"
)

// columns hold a chunk's text by language; a query asks all of them.
var columns = []string{"body_en", "body_de", "body_xx"}

// PGSearch is the Searcher on pg_search.
type PGSearch struct {
	pool  *pgxpool.Pool
	books *catalog.Service
}

// NewPGSearch returns the Searcher on the pool's book_chunks index.
func NewPGSearch(pool *pgxpool.Pool, books *catalog.Service) *PGSearch {
	return &PGSearch{pool: pool, books: books}
}

// match is the condition a chunk meets: in one of the language columns, all
// the free words in any form, and each phrase in order. The texts are
// parameters; pg_search needs them as constants, which the transaction's
// custom plans keep them.
func match(p parsed, arg func(any) string) string {
	var words, phrases []string
	if p.Words != "" {
		words = append(words, arg(p.Words))
	}
	for _, ph := range p.Phrases {
		phrases = append(phrases, arg(ph))
	}
	var alts []string
	for _, c := range columns {
		var conds []string
		for _, w := range words {
			conds = append(conds, c+" &&& "+w)
		}
		for _, ph := range phrases {
			conds = append(conds, c+" ### "+ph)
		}
		alts = append(alts, "("+strings.Join(conds, " AND ")+")")
	}
	return "(" + strings.Join(alts, " OR ") + ")"
}

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

// Search finds the chunks that match in the libraries the scope may see,
// takes the best of them, keeps those of books the filter matches, and
// groups them by book, best book first.
func (s *PGSearch) Search(ctx context.Context, scope library.Scope, q Query) (Result, error) {
	p, err := parse(q.Text)
	if err != nil {
		return Result{}, err
	}
	limit := min(max(q.Limit, 1), MaxLimit)
	offset := max(q.Offset, 0)

	var res Result
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// A generic plan would hand pg_search the query text as a parameter
		// it cannot read, and fail.
		if _, err := tx.Exec(ctx, "SET LOCAL plan_cache_mode = force_custom_plan"); err != nil {
			return err
		}
		chunks, err := s.best(ctx, tx, scope, q, p)
		if err != nil {
			return err
		}
		res, err = s.group(ctx, tx, chunks, p, limit, offset)
		return err
	})
	return res, err
}

// best is the window of the best chunks of visible books that match. The
// libraries, and the books when a filter picks few enough, are filters
// inside the index, so the window is taken among those alone; the filter's
// own conditions are checked again on the books found.
func (s *PGSearch) best(ctx context.Context, tx pgx.Tx, scope library.Scope, q Query, p parsed) ([]found, error) {
	var libs []uuid.UUID
	rows, err := tx.Query(ctx, "SELECT visible_library_ids($1, $2)", scope.Viewer, scope.SeesAll)
	if err != nil {
		return nil, err
	}
	if libs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return nil, err
	}
	if q.LibraryID != nil {
		if !slices.Contains(libs, *q.LibraryID) {
			return nil, nil
		}
		libs = []uuid.UUID{*q.LibraryID}
	}
	if len(libs) == 0 {
		return nil, nil
	}

	var a args
	inner := []string{match(p, a.add), "library_id = ANY(" + a.add(libs) + "::uuid[])"}
	if !matchesAll(q.Filter) {
		ids, err := s.books.Select(ctx, scope, q.LibraryID, q.Filter, nil)
		switch {
		case errors.Is(err, catalog.ErrTooMany):
			// Too many to name; the outer conditions keep them apart.
		case err != nil:
			return nil, err
		case len(ids) == 0:
			return nil, nil
		default:
			inner = append(inner, "book_id = ANY("+a.add(ids)+"::uuid[])")
		}
	}
	outer, err := catalog.BookConditions(scope, q.LibraryID, q.Filter, a.add)
	if err != nil {
		return nil, err
	}
	sql := `SELECT c.id, c.book_id, c.score FROM (
		SELECT id, book_id, pdb.score(id) AS score FROM book_chunks
		WHERE ` + strings.Join(inner, " AND ") + `
		ORDER BY pdb.score(id) DESC LIMIT ` + a.add(window) + `
	) c JOIN books b ON b.id = c.book_id
	WHERE ` + strings.Join(outer, " AND ") + `
	ORDER BY c.score DESC, c.id`
	rows, err = tx.Query(ctx, sql, a.values...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		var score float32
		err := r.Scan(&f.id, &f.bookID, &score)
		f.score = float64(score)
		return f, err
	})
}

// group makes books of the chunks, in the order of each book's best chunk,
// and reads the passages of the page's books.
func (s *PGSearch) group(ctx context.Context, tx pgx.Tx, chunks []found, p parsed, limit, offset int) (Result, error) {
	var order []uuid.UUID
	byBook := map[uuid.UUID][]found{}
	for _, c := range chunks {
		if byBook[c.bookID] == nil {
			order = append(order, c.bookID)
		}
		if len(byBook[c.bookID]) < hitsPerBook {
			byBook[c.bookID] = append(byBook[c.bookID], c)
		}
	}
	res := Result{More: len(order) > offset+limit}
	if offset >= len(order) {
		return res, nil
	}
	page := order[offset:min(offset+limit, len(order))]
	var ids []int64
	for _, b := range page {
		for _, c := range byBook[b] {
			ids = append(ids, c.id)
		}
	}
	hits, err := s.passages(ctx, tx, ids, p)
	if err != nil {
		return Result{}, err
	}
	for _, b := range page {
		book := Book{ID: b, Score: byBook[b][0].score}
		for _, c := range byBook[b] {
			if h, ok := hits[c.id]; ok {
				h.Score = c.score
				book.Hits = append(book.Hits, h)
			}
		}
		res.Books = append(res.Books, book)
	}
	return res, nil
}

// passages reads the chunks with a snippet of each around its matches.
func (s *PGSearch) passages(ctx context.Context, tx pgx.Tx, ids []int64, p parsed) (map[int64]Hit, error) {
	var a args
	snippets := make([]string, len(columns))
	for i, c := range columns {
		snippets[i] = fmt.Sprintf("NULLIF(pdb.snippet(%s, %s, %s, %s), '')", c, a.add(matchStart), a.add(matchEnd), a.add(snippetChars))
	}
	sql := `SELECT id, file_id, position, chapter, page_from, page_to, char_offset,
		coalesce(` + strings.Join(snippets, ", ") + `, '')
	FROM book_chunks
	WHERE ` + match(p, a.add) + ` AND id = ANY(` + a.add(ids) + `::bigint[])`
	rows, err := tx.Query(ctx, sql, a.values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Hit{}
	for rows.Next() {
		var h Hit
		var from, to *int32
		var snippet string
		var position, offset int32
		if err := rows.Scan(&h.ChunkID, &h.FileID, &position, &h.Chapter, &from, &to, &offset, &snippet); err != nil {
			return nil, err
		}
		h.Position, h.Offset = int(position), int(offset)
		if from != nil && to != nil {
			h.PageFrom, h.PageTo = int(*from), int(*to)
		}
		h.Snippet = parts(snippet)
		out[h.ChunkID] = h
	}
	return out, rows.Err()
}

// parts splits a snippet at its markers. pg_search escapes the text for
// HTML; the parts are plain text, for the client to show as it shows text.
func parts(snippet string) []Part {
	var out []Part
	for i, piece := range strings.Split(snippet, matchStart) {
		text, rest, closed := strings.Cut(piece, matchEnd)
		if i == 0 || !closed {
			if s := html.UnescapeString(piece); s != "" {
				out = append(out, Part{Text: s})
			}
			continue
		}
		if s := html.UnescapeString(text); s != "" {
			out = append(out, Part{Text: s, Match: true})
		}
		if s := html.UnescapeString(rest); s != "" {
			out = append(out, Part{Text: s})
		}
	}
	return out
}

// matchesAll says the tree has no rule: an All without rules, which every
// book matches.
func matchesAll(n filter.Node) bool {
	return n.Field == "" && n.Not == nil && len(n.Any) == 0 && len(n.All) == 0
}
