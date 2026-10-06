package catalog

import (
	"context"
	"fmt"

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
