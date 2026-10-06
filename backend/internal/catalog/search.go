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
	// name must be, from 0 to 1. Postgres says 0.6 by default; "sandersen"
	// and "sanderson" are 0.54 alike, and a quick search should forgive one
	// wrong letter in nine.
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
	key := Key(words)
	if utf8.RuneCountInString(strings.ReplaceAll(key, " ", "")) < minSearchLen {
		return []Hit{}, nil
	}
	if len(key) > maxSearchLen {
		key = strings.TrimSpace(key[:maxSearchLen])
	}
	limit = min(max(limit, 1), MaxHits)

	// Each of the three is found through its trigram index with <%, which
	// compares the search with the most alike stretch of the title or name;
	// the threshold that operator uses is only set per transaction.
	query := `
WITH matched AS (
    SELECT b.id, word_similarity($3, b.title_key) AS score, '` + MatchTitle + `' AS match
    FROM books b
    WHERE $3 <% b.title_key AND b.deleted_at IS NULL
  UNION ALL
    SELECT c.book_id, word_similarity($3, a.name_key), '` + MatchAuthor + `'
    FROM authors a
    JOIN book_contributors c ON c.author_id = a.id AND c.role = '` + RoleAuthor + `'
    WHERE $3 <% a.name_key
  UNION ALL
    SELECT b.id, word_similarity($3, sr.name_key), '` + MatchSeries + `'
    FROM series sr
    JOIN books b ON b.series_id = sr.id AND b.deleted_at IS NULL
    WHERE $3 <% sr.name_key
), best AS (
    SELECT DISTINCT ON (id) id, score, match
    FROM matched
    ORDER BY id, score DESC, match = '` + MatchTitle + `' DESC
)
SELECT ` + summaryColumns + `, best.match
FROM best
JOIN books b ON b.id = best.id
LEFT JOIN series s ON s.id = b.series_id
` + summaryJoin("$1") + `
WHERE b.library_id IN (SELECT * FROM visible_library_ids($1, $2)) AND NOT b.placeholder
ORDER BY best.score DESC, b.sort_title, b.id
LIMIT $4`

	hits := []Hit{}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL pg_trgm.word_similarity_threshold = %g", matchThreshold)); err != nil {
			return err
		}
		// The planner takes a trigram comparison for as cheap as any other
		// and reads all 50,000 titles of a library rather than the index:
		// 150 ms instead of 6.
		if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, query, scope.Viewer, scope.SeesAll, key, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Hit
			var series *string
			if err := rows.Scan(append(h.dest(&series), &h.Match)...); err != nil {
				return err
			}
			h.Series = deref(series)
			hits = append(hits, h)
		}
		return rows.Err()
	})
	return hits, err
}
