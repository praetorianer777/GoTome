package catalog

import (
	"context"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MatchBooks finds the books of the libraries the scope sees that a title,
// authors and ISBNs may be: those with one of the ISBNs, else those with
// the title and an author. Placeholders are among them if asked for. Under
// one name each book appears once, and the same rule is the import's and
// the join of a placeholder to the book that came later.
func MatchBooks(ctx context.Context, q *sqlc.Queries, scope library.Scope, isbns []string, titleKey string, personKeys []string, placeholders bool) ([]uuid.UUID, error) {
	if isbns == nil {
		isbns = []string{}
	}
	if personKeys == nil {
		personKeys = []string{}
	}
	found, err := q.MatchImportBooks(ctx, sqlc.MatchImportBooksParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Isbns: isbns, TitleKey: titleKey, PersonKeys: personKeys,
		WithPlaceholders: placeholders,
	})
	if err != nil {
		return nil, err
	}
	var byISBN, byTitle []uuid.UUID
	for _, f := range found {
		if f.ByIsbn {
			byISBN = append(byISBN, f.ID)
		} else {
			byTitle = append(byTitle, f.ID)
		}
	}
	if len(byISBN) > 0 {
		return byISBN, nil
	}
	return byTitle, nil
}
