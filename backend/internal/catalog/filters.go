package catalog

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Fields books can be filtered by, as a rule names them.
const (
	FieldAuthor    = "author"
	FieldSeries    = "series"
	FieldTag       = "tag"
	FieldLanguage  = "language"
	FieldPublished = "published"
	FieldFormat    = "format"
	// FieldStatus and FieldRating are where the viewer stands with a book:
	// each person filters by their own.
	FieldStatus = "status"
	FieldRating = "rating"
)

// Comparisons, as a rule names them.
const (
	// OpIn is one of the values: an author among these names.
	OpIn = "in"
	// OpEmpty is no value at all: in no series, no language known.
	OpEmpty = "empty"
	// OpBetween takes two years, either of which may be empty for no limit,
	// and matches the books published from the first to the second.
	OpBetween = "between"
)

// languageOf is a book's language as filters and facets compare it: the
// language without its region, so that en-GB and en-US are both English.
const languageOf = `lower(split_part(replace(b.language, '_', '-'), '-', 1))`

// Filters is every field a filter on books may use.
var Filters = filter.Registry{
	FieldAuthor: {
		OpIn: {Arity: -1, Value: nameKey, SQL: func(v []string, arg func(any) string) string {
			return `EXISTS (SELECT 1 FROM book_contributors c JOIN authors a ON a.id = c.author_id
				WHERE c.book_id = b.id AND c.role = '` + RoleAuthor + `' AND a.name_key = ANY(` + arg(v) + `::text[]))`
		}},
		OpEmpty: {SQL: func([]string, func(any) string) string {
			return `NOT EXISTS (SELECT 1 FROM book_contributors c WHERE c.book_id = b.id AND c.role = '` + RoleAuthor + `')`
		}},
	},
	FieldSeries: {
		OpIn: {Arity: -1, Value: nameKey, SQL: func(v []string, arg func(any) string) string {
			return `b.series_id IN (SELECT id FROM series WHERE name_key = ANY(` + arg(v) + `::text[]))`
		}},
		OpEmpty: {SQL: func([]string, func(any) string) string { return "b.series_id IS NULL" }},
	},
	FieldTag: {
		OpIn: {Arity: -1, Value: nameKey, SQL: func(v []string, arg func(any) string) string {
			return `EXISTS (SELECT 1 FROM book_tags bt JOIN tags t ON t.id = bt.tag_id
				WHERE bt.book_id = b.id AND t.name_key = ANY(` + arg(v) + `::text[]))`
		}},
		OpEmpty: {SQL: func([]string, func(any) string) string {
			return "NOT EXISTS (SELECT 1 FROM book_tags bt WHERE bt.book_id = b.id)"
		}},
	},
	FieldLanguage: {
		OpIn: {Arity: -1, Value: languageKey, SQL: func(v []string, arg func(any) string) string {
			return languageOf + " = ANY(" + arg(v) + "::text[])"
		}},
		OpEmpty: {SQL: func([]string, func(any) string) string { return "b.language IS NULL" }},
	},
	FieldPublished: {
		OpBetween: {Arity: 2, Value: year, SQL: func(v []string, arg func(any) string) string {
			conditions := []string{"b.published_on IS NOT NULL"}
			if v[0] != "" {
				conditions = append(conditions, "b.published_on >= make_date("+arg(v[0])+"::int, 1, 1)")
			}
			if v[1] != "" {
				conditions = append(conditions, "b.published_on < make_date("+arg(v[1])+"::int + 1, 1, 1)")
			}
			return strings.Join(conditions, " AND ")
		}},
		OpEmpty: {SQL: func([]string, func(any) string) string { return "b.published_on IS NULL" }},
	},
	FieldFormat: {
		OpIn: {Arity: -1, Value: format, SQL: func(v []string, arg func(any) string) string {
			return `EXISTS (SELECT 1 FROM book_files f
				WHERE f.book_id = b.id AND f.trashed_at IS NULL AND f.format = ANY(` + arg(v) + `::text[]))`
		}},
	},
}

// filtersFor is Filters with the fields that depend on who is looking.
func filtersFor(viewer uuid.UUID) filter.Registry {
	own := func(column string, arg func(any) string) string {
		return `(SELECT ub.` + column + ` FROM user_books ub WHERE ub.book_id = b.id AND ub.user_id = ` + arg(viewer) + `)`
	}
	registry := maps.Clone(Filters)
	registry[FieldStatus] = filter.Field{
		OpIn: {Arity: -1, Value: status, SQL: func(v []string, arg func(any) string) string {
			return "COALESCE(" + own("status", arg) + ", '" + StatusUnread + "') = ANY(" + arg(v) + "::text[])"
		}},
	}
	registry[FieldRating] = filter.Field{
		OpIn: {Arity: -1, Value: rating, SQL: func(v []string, arg func(any) string) string {
			return own("rating", arg) + " = ANY(" + arg(v) + "::smallint[])"
		}},
		OpBetween: {Arity: 2, Value: ratingOrNone, SQL: func(v []string, arg func(any) string) string {
			conditions := []string{own("rating", arg) + " IS NOT NULL"}
			if v[0] != "" {
				conditions = append(conditions, own("rating", arg)+" >= "+arg(v[0])+"::smallint")
			}
			if v[1] != "" {
				conditions = append(conditions, own("rating", arg)+" <= "+arg(v[1])+"::smallint")
			}
			return strings.Join(conditions, " AND ")
		}},
		OpEmpty: {SQL: func(_ []string, arg func(any) string) string { return own("rating", arg) + " IS NULL" }},
	}
	return registry
}

func status(v string) (string, error) {
	if !slices.Contains(Statuses, v) {
		return "", fmt.Errorf("%q is not a status; one is %s", v, strings.Join(Statuses, ", "))
	}
	return v, nil
}

func rating(v string) (string, error) {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n < 1 || n > MaxRating {
		return "", fmt.Errorf("%q is not a rating from 1 to %d", v, MaxRating)
	}
	return strings.TrimSpace(v), nil
}

func ratingOrNone(v string) (string, error) {
	if strings.TrimSpace(v) == "" {
		return "", nil
	}
	return rating(v)
}

func nameKey(v string) (string, error) {
	if k := Key(v); k != "" {
		return k, nil
	}
	return "", fmt.Errorf("%q has no letter or digit to compare by", v)
}

func languageKey(v string) (string, error) {
	lang, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "-")), "-")
	if len(lang) < 2 || len(lang) > 3 || strings.IndexFunc(lang, func(r rune) bool { return r < 'a' || r > 'z' }) >= 0 {
		return "", fmt.Errorf("%q is not a language code such as en or de", v)
	}
	return lang, nil
}

func year(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 9999 {
		return "", fmt.Errorf("%q is not a year", v)
	}
	return v, nil
}

func format(v string) (string, error) {
	f := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "."))
	if _, ok := KindOf(f); !ok {
		return "", fmt.Errorf("%q is not a format GOtome knows", v)
	}
	return f, nil
}

// visibleBooks are the conditions every list and count of books starts from:
// the books the scope may see, in one library or all, that the filter keeps;
// placeholders only when asked for, as they are not in the library yet.
func visibleBooks(scope library.Scope, libraryID *uuid.UUID, tree filter.Node, placeholders bool, arg func(any) string) ([]string, error) {
	where := []string{
		"b.deleted_at IS NULL",
		"b.library_id IN (SELECT visible_library_ids(" + arg(scope.Viewer) + ", " + arg(scope.SeesAll) + "))",
	}
	if libraryID != nil {
		where = append(where, "b.library_id = "+arg(*libraryID))
	}
	if !placeholders {
		where = append(where, "NOT b.placeholder")
	}
	cond, err := filtersFor(scope.Viewer).Compile(tree, arg)
	if err != nil {
		return nil, err
	}
	if cond != "TRUE" {
		where = append(where, cond)
	}
	return where, nil
}

// FacetValue is one value of a field and how many books have it.
type FacetValue struct {
	// Value is what a rule on the field takes: a name's key, a language
	// code, the first year of a decade, a format.
	Value string
	// Label is the value as people read it; for languages and decades the
	// same as Value, which the reader's language spells out.
	Label string
	Count int
}

// Facet is the values of one field among the books a filter leaves.
type Facet struct {
	Field  string
	Values []FacetValue
}

// MaxFacetValues is how many values of a name a facet lists at most, beside
// those the filter already picked.
const MaxFacetValues = 50

// facetQueries are, per field, the values and counts among the books the
// conditions keep. {where} is the conditions, {picked} the values the filter
// picked, which a long list puts first, {limit} the limit.
var facetQueries = []struct {
	field string
	query string
}{
	{FieldAuthor, `
SELECT a.name_key, a.name, count(DISTINCT b.id)
FROM books b
JOIN book_contributors c ON c.book_id = b.id AND c.role = '` + RoleAuthor + `'
JOIN authors a ON a.id = c.author_id
WHERE {where}
GROUP BY a.name_key, a.name, a.sort_name
ORDER BY a.name_key = ANY({picked}::text[]) DESC, 3 DESC, a.sort_name
LIMIT {limit}`},
	{FieldSeries, `
SELECT s.name_key, s.name, count(*)
FROM books b
JOIN series s ON s.id = b.series_id
WHERE {where}
GROUP BY s.name_key, s.name
ORDER BY s.name_key = ANY({picked}::text[]) DESC, 3 DESC, s.name_key
LIMIT {limit}`},
	{FieldTag, `
SELECT t.name_key, t.name, count(*)
FROM books b
JOIN book_tags bt ON bt.book_id = b.id
JOIN tags t ON t.id = bt.tag_id
WHERE {where}
GROUP BY t.name_key, t.name
ORDER BY t.name_key = ANY({picked}::text[]) DESC, 3 DESC, t.name_key
LIMIT {limit}`},
	{FieldLanguage, `
SELECT ` + languageOf + `, ` + languageOf + `, count(*)
FROM books b
WHERE {where} AND b.language IS NOT NULL
GROUP BY 1
ORDER BY 3 DESC, 1
LIMIT {limit}`},
	{FieldPublished, `
SELECT d::text, d::text, count(*)
FROM books b, LATERAL (SELECT extract(year FROM b.published_on)::int / 10 * 10 AS d) decade
WHERE {where} AND b.published_on IS NOT NULL
GROUP BY d
ORDER BY d
LIMIT {limit}`},
	{FieldStatus, `
SELECT st, st, count(*)
FROM books b
LEFT JOIN user_books ub ON ub.book_id = b.id AND ub.user_id = {viewer},
LATERAL (SELECT COALESCE(ub.status, '` + StatusUnread + `') AS st) own
WHERE {where}
GROUP BY st
ORDER BY array_position(ARRAY['` + strings.Join(Statuses, "','") + `'], st)
LIMIT {limit}`},
	{FieldRating, `
SELECT ub.rating::text, ub.rating::text, count(*)
FROM books b
JOIN user_books ub ON ub.book_id = b.id AND ub.user_id = {viewer} AND ub.rating IS NOT NULL
WHERE {where}
GROUP BY ub.rating
ORDER BY ub.rating DESC
LIMIT {limit}`},
	{FieldFormat, `
SELECT f.format, f.format, count(DISTINCT b.id)
FROM books b
JOIN book_files f ON f.book_id = b.id AND f.trashed_at IS NULL
WHERE {where}
GROUP BY f.format
ORDER BY 3 DESC, 1
LIMIT {limit}`},
}

// Facets counts, for each field, how many of the books the filter leaves
// have each value. A field's own rules are left out of its count, so that
// the counts say what picking one more value would add.
func (s *Service) Facets(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, tree filter.Node) ([]Facet, error) {
	// The whole tree first, so that a broken one is refused with its own
	// message rather than with that of a smaller tree.
	if _, err := filtersFor(scope.Viewer).Compile(tree, func(any) string { return "NULL" }); err != nil {
		return nil, err
	}
	batch := &pgx.Batch{}
	for _, fq := range facetQueries {
		var args []any
		arg := func(v any) string {
			args = append(args, v)
			return fmt.Sprintf("$%d", len(args))
		}
		where, err := visibleBooks(scope, libraryID, tree.Without(fq.field), false, arg)
		if err != nil {
			return nil, err
		}
		picked := picked(tree, fq.field)
		query := strings.ReplaceAll(fq.query, "{where}", strings.Join(where, " AND "))
		if strings.Contains(query, "{picked}") {
			query = strings.ReplaceAll(query, "{picked}", arg(picked))
		}
		if strings.Contains(query, "{viewer}") {
			query = strings.ReplaceAll(query, "{viewer}", arg(scope.Viewer))
		}
		query = strings.ReplaceAll(query, "{limit}", arg(MaxFacetValues+len(picked)))
		batch.Queue(query, args...)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	facets := make([]Facet, 0, len(facetQueries))
	for _, fq := range facetQueries {
		rows, err := results.Query()
		if err != nil {
			return nil, err
		}
		facet := Facet{Field: fq.field, Values: []FacetValue{}}
		for rows.Next() {
			var v FacetValue
			if err := rows.Scan(&v.Value, &v.Label, &v.Count); err != nil {
				rows.Close()
				return nil, err
			}
			facet.Values = append(facet.Values, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		facets = append(facets, facet)
	}
	return facets, nil
}

// picked are the values the tree's "in" rules on the field ask for, as they
// are compared.
func picked(tree filter.Node, field string) []string {
	out := []string{}
	op := Filters[field][OpIn]
	var walk func(filter.Node)
	walk = func(n filter.Node) {
		if n.Field == field && n.Op == OpIn && op.Value != nil {
			for _, v := range n.Values {
				if k, err := op.Value(v); err == nil && !slices.Contains(out, k) {
					out = append(out, k)
				}
			}
		}
		for _, c := range n.All {
			walk(c)
		}
		for _, c := range n.Any {
			walk(c)
		}
	}
	walk(tree)
	return out
}

// IsFilterError reports whether err is a filter that cannot be used, as
// opposed to a failure of the database.
func IsFilterError(err error) bool {
	var fe *filter.Error
	return errors.As(err, &fe)
}
