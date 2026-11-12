package catalog

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What a quick search hit matched.
const (
	MatchTitle  = "title"
	MatchAuthor = "author"
	MatchSeries = "series"
)

const (
	// MaxHits is the most a quick search returns.
	MaxHits = 50
	// minSearchLen is the shortest search, in letters and digits: one or two
	// make no trigram worth looking up.
	minSearchLen = 3
	// maxSearchLen keeps a pasted paragraph from becoming a slow search.
	maxSearchLen = 100
	// matchThreshold is how alike the search and some stretch of a title or
	// name must be, from 0 to 1. Postgres says 0.6 by default; "lundquist"
	// and "lundqvist" are 0.54 alike, and a quick search should forgive one
	// wrong letter in nine. quick_search_matches sets the same.
	matchThreshold = 0.45
)

// Hit is a book a quick search found, and what of it matched.
type Hit struct {
	Summary
	Match string
}

// Search finds the books the scope may see whose title, author or series
// looks like the words: misspelt, or only part of it typed. The best matches
// come first. Fewer than three letters find nothing.
func (s *Service) Search(ctx context.Context, scope library.Scope, words string, limit int) ([]Hit, error) {
	key, ok := searchKey(words)
	if !ok {
		return []Hit{}, nil
	}
	limit = min(max(limit, 1), MaxHits)

	// The SQL function quick_search_matches is the one place that says what
	// matches, and finds each of the three through its trigram index.
	query := `
SELECT ` + summaryColumns + `, m.match
FROM quick_search_matches($3) m
JOIN books b ON b.id = m.book_id
LEFT JOIN series s ON s.id = b.series_id
` + summaryJoin("$1") + `
WHERE b.library_id IN (SELECT * FROM visible_library_ids($1, $2)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit($1, $2))) AND NOT b.placeholder
ORDER BY m.score DESC, b.sort_title, b.id
LIMIT $4`

	rows, err := s.pool.Query(ctx, query, scope.Viewer, scope.SeesAll, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []Hit{}
	for rows.Next() {
		var h Hit
		var series *string
		if err := rows.Scan(append(h.dest(&series), &h.Match)...); err != nil {
			return nil, err
		}
		h.Series = deref(series)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// searchKey is the words as a quick search compares them, and false when
// they have too few letters to find anything by.
func searchKey(words string) (string, bool) {
	key := Key(words)
	if utf8.RuneCountInString(strings.ReplaceAll(key, " ", "")) < minSearchLen {
		return "", false
	}
	if len(key) > maxSearchLen {
		key = strings.TrimSpace(key[:maxSearchLen])
	}
	return key, true
}

// trigramTx runs fn in a transaction set up for a search with <%.
func trigramTx(ctx context.Context, s *Service, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL pg_trgm.word_similarity_threshold = %g", matchThreshold)); err != nil {
			return err
		}
		// The planner takes a trigram comparison for as cheap as any other
		// and reads all 50,000 titles of a library rather than the index:
		// 150 ms instead of 6.
		if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
			return err
		}
		return fn(tx)
	})
}

// NameHit is an author or a series a quick search found: Match says which,
// Key is what their page goes by, Books how many of their books the scope
// sees.
type NameHit struct {
	Match string
	Name  string
	Key   string
	Books int
}

// SearchNames finds the authors and series with books the scope may see
// whose name looks like the words, the best first. A person is found once,
// under the spelling most of those books use.
func (s *Service) SearchNames(ctx context.Context, scope library.Scope, words string, limit int) ([]NameHit, error) {
	hits := []NameHit{}
	key, ok := searchKey(words)
	if !ok {
		return hits, nil
	}
	// Ordered before the books are counted, and counted in that order, so
	// that the limit stops the counting; a name of three letters may look
	// like hundreds.
	query := `
WITH matched AS (
    SELECT '` + MatchAuthor + `' AS match, a.person_key AS key, max(word_similarity($3, a.name_key)) AS score
    FROM authors a
    WHERE $3 <% a.name_key
    GROUP BY a.person_key
  UNION ALL
    SELECT '` + MatchSeries + `', sr.name_key, word_similarity($3, sr.name_key)
    FROM series sr
    WHERE $3 <% sr.name_key
)
SELECT m.match, counted.name, m.key, counted.books
FROM (SELECT * FROM matched ORDER BY score DESC, key) m
CROSS JOIN LATERAL (
    SELECT count(DISTINCT b.id)::int AS books, mode() WITHIN GROUP (ORDER BY n.name) AS name
    FROM (SELECT a.name, c.book_id
          FROM authors a
          JOIN book_contributors c ON c.author_id = a.id AND c.role = '` + RoleAuthor + `'
          WHERE m.match = '` + MatchAuthor + `' AND a.person_key = m.key
          UNION ALL
          SELECT sr.name, bs.id
          FROM series sr
          JOIN books bs ON bs.series_id = sr.id
          WHERE m.match = '` + MatchSeries + `' AND sr.name_key = m.key) n
    JOIN books b ON b.id = n.book_id
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible_library_ids($1, $2)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit($1, $2)))
) counted
WHERE counted.books > 0
LIMIT $4`
	err := trigramTx(ctx, s, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, scope.Viewer, scope.SeesAll, key, min(max(limit, 1), MaxHits))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h NameHit
			if err := rows.Scan(&h.Match, &h.Name, &h.Key, &h.Books); err != nil {
				return err
			}
			hits = append(hits, h)
		}
		return rows.Err()
	})
	return hits, err
}
