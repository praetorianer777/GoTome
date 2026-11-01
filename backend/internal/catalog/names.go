package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What Names looks in, beside FieldAuthor, FieldSeries and FieldTag.
const NamePublisher = "publisher"

// nameSources joins each kind of name to the books that carry it.
var nameSources = map[string]string{
	FieldAuthor:   `authors n JOIN book_contributors c ON c.author_id = n.id JOIN books b ON b.id = c.book_id`,
	FieldSeries:   `series n JOIN books b ON b.series_id = n.id`,
	NamePublisher: `publishers n JOIN books b ON b.publisher_id = n.id`,
	FieldTag:      `tags n JOIN book_tags t ON t.tag_id = n.id JOIN books b ON b.id = t.book_id`,
}

// Names returns names of authors, series, publishers or tags whose words
// begin as typed, for a person filling in a book. Only names on books the
// scope may see count: the rest would tell what a private library holds.
func (s *Service) Names(ctx context.Context, scope library.Scope, kind, typed string, limit int) ([]string, error) {
	from, ok := nameSources[kind]
	if !ok {
		return nil, fmt.Errorf("no kind of name %q", kind)
	}
	key := Key(typed)
	if key == "" {
		return []string{}, nil
	}
	// A key holds letters, digits and single spaces only: nothing in it is
	// special to LIKE.
	rows, err := s.pool.Query(ctx, `
SELECT n.name
FROM `+from+`
WHERE b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids($1, $2))
  AND (n.name_key LIKE $3 || '%' OR n.name_key LIKE '% ' || $3 || '%')
GROUP BY n.name, n.name_key
ORDER BY n.name_key LIKE $3 || '%' DESC, count(*) DESC, n.name_key
LIMIT $4`, scope.Viewer, scope.SeesAll, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Author is one person, however their name is spelled, as the books the
// viewer sees name them.
type Author struct {
	// Name is the spelling most of those books use.
	Name string
	// Key is the person's PersonKey.
	Key string
	// Spellings are every spelling among those books, Name included.
	Spellings []string
	Books     int
}

// Author returns the person a name or a person's key stands for. One without
// a book the scope may see is ErrNotFound, like a name nobody has.
func (s *Service) Author(ctx context.Context, scope library.Scope, key string) (Author, error) {
	a := Author{Key: PersonKey(key)}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where, err := visibleBooks(scope, nil, filter.Node{}, false, arg)
	if err != nil {
		return Author{}, err
	}
	err = s.pool.QueryRow(ctx, `
SELECT mode() WITHIN GROUP (ORDER BY a.name), array_agg(DISTINCT a.name), count(DISTINCT b.id)
FROM authors a
JOIN book_contributors c ON c.author_id = a.id AND c.role = '`+RoleAuthor+`'
JOIN books b ON b.id = c.book_id
WHERE a.person_key = `+arg(a.Key)+` AND `+strings.Join(where, " AND ")+`
GROUP BY a.person_key`, args...).Scan(&a.Name, &a.Spellings, &a.Books)
	if errors.Is(err, pgx.ErrNoRows) {
		return Author{}, ErrNotFound
	}
	return a, err
}

// Series is a series as far as the viewer sees it.
type Series struct {
	Name string
	Key  string
	// Books counts its books the viewer sees, Read those they completed.
	Books int
	Read  int
	// Indexes are the places in it of the books that have one.
	Indexes []float64
	// LibraryID is the library that holds most of those books, where a book
	// wished for would join them.
	LibraryID uuid.UUID
	// Next is the first book in series order the viewer has not read or
	// begun; nil when there is none.
	Next *Summary
}

// Series returns the series a name or its key stands for. One without a book
// the scope may see is ErrNotFound, like a name nothing has.
func (s *Service) Series(ctx context.Context, scope library.Scope, key string) (Series, error) {
	out := Series{Key: Key(key), Indexes: []float64{}}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where, err := visibleBooks(scope, nil, filter.Node{}, false, arg)
	if err != nil {
		return Series{}, err
	}
	viewer := arg(scope.Viewer)
	err = s.pool.QueryRow(ctx, `
SELECT s.name, count(*), count(*) FILTER (WHERE ub.status = '`+StatusCompleted+`'),
       COALESCE(array_agg(b.series_index) FILTER (WHERE b.series_index IS NOT NULL), '{}'),
       mode() WITHIN GROUP (ORDER BY b.library_id)
FROM series s
JOIN books b ON b.series_id = s.id
`+summaryJoin(viewer)+`
WHERE s.name_key = `+arg(out.Key)+` AND `+strings.Join(where, " AND ")+`
GROUP BY s.id`, args...).Scan(&out.Name, &out.Books, &out.Read, &out.Indexes, &out.LibraryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Series{}, ErrNotFound
	}
	if err != nil {
		return Series{}, err
	}
	next, err := s.List(ctx, scope, ListParams{
		Filter: filter.Node{All: []filter.Node{
			{Field: FieldSeries, Op: OpIn, Values: []string{out.Key}},
			{Field: FieldStatus, Op: OpIn, Values: []string{StatusUnread, StatusWishlist}},
		}},
		Order: OrderSeries, Limit: 1,
	})
	if err != nil {
		return Series{}, err
	}
	if len(next.Books) > 0 {
		out.Next = &next.Books[0]
	}
	return out, nil
}

// Gap is a stretch of a series no book stands for, both ends included.
type Gap struct {
	From int
	To   int
}

// Gaps says which whole places from 1 up to the last of the indexes are
// taken by none of them. A book at 2.5 fills no place: it lies between two.
func Gaps(indexes []float64) []Gap {
	held := map[int]bool{}
	last := 0
	for _, index := range indexes {
		if place := int(index); float64(place) == index {
			held[place] = true
		}
		last = max(last, int(math.Ceil(index)))
	}
	gaps := []Gap{}
	for place := 1; place < last; place++ {
		switch {
		case held[place]:
		case len(gaps) > 0 && gaps[len(gaps)-1].To == place-1:
			gaps[len(gaps)-1].To = place
		default:
			gaps = append(gaps, Gap{From: place, To: place})
		}
	}
	return gaps
}
