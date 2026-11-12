package catalog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// EbookFormats are the formats an e-reader reads.
func EbookFormats() []string {
	var out []string
	for format, kind := range kindOf {
		if kind == KindEbook {
			out = append(out, format)
		}
	}
	slices.Sort(out)
	return out
}

// HasEbook is the rule that keeps the books with a file an e-reader reads.
func HasEbook() filter.Node {
	return filter.Node{Field: FieldFormat, Op: OpIn, Values: EbookFormats()}
}

// Name is an author or a series as a list of them shows it, with how many of
// the listed books the viewer sees.
type Name struct {
	Name string
	Key  string
	// Sort is what the list is ordered by: an author's sort name, a
	// series' key.
	Sort  string
	Books int
}

// NamePage is one page of names, and the cursor of the next; Next is empty
// on the last page.
type NamePage struct {
	Names []Name
	Next  string
}

// Name lists.
const (
	// NamesAuthors lists each spelling apart, as the OPDS feeds' links
	// compare names.
	NamesAuthors = "authors"
	// NamesPeople lists each person once, under the spelling most of their
	// visible books use, where the first of their spellings stands.
	NamesPeople = "people"
	NamesSeries = "series"
)

// nameLists are what each list of names reads: the table, the column it is
// ordered by, how one name's books are found, and how its name and key are
// read. name may use what counted adds to the count; only keeps some rows.
var nameLists = map[string]struct{ table, order, books, name, key, counted, only string }{
	NamesAuthors: {
		table: "authors", order: "n.sort_name", name: "n.name", key: "n.name_key",
		books: `book_contributors c JOIN books b ON b.id = c.book_id
		WHERE c.author_id = n.id AND c.role = '` + RoleAuthor + `'`,
	},
	NamesPeople: {
		table: "authors", order: "n.sort_name", name: "counted.name", key: "n.person_key",
		books: `authors p JOIN book_contributors c ON c.author_id = p.id JOIN books b ON b.id = c.book_id
		WHERE p.person_key = n.person_key AND c.role = '` + RoleAuthor + `'`,
		counted: ", mode() WITHIN GROUP (ORDER BY p.name) AS name",
		only: `NOT EXISTS (SELECT 1 FROM authors f
		WHERE f.person_key = n.person_key AND (f.sort_name, f.id) < (n.sort_name, n.id))`,
	},
	NamesSeries: {table: "series", order: "n.name_key", name: "n.name", key: "n.name_key", books: "books b WHERE b.series_id = n.id"},
}

// NameList returns a page of the authors or series that have books the scope
// may see and the tree keeps, in alphabetical order. Pages are cut by the
// last name, as lists of books are. The names are walked in their index's
// order and each one's books counted through its own index, so a page reads
// as many names as it needs and never the whole catalogue.
func (s *Service) NameList(ctx context.Context, scope library.Scope, list string, tree filter.Node, after string, limit int) (NamePage, error) {
	l, ok := nameLists[list]
	if !ok {
		return NamePage{}, fmt.Errorf("unknown list %q", list)
	}
	limit = min(max(limit, 1), MaxPage)
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	visible, err := visibleBooks(scope, nil, tree, false, arg)
	if err != nil {
		return NamePage{}, err
	}
	where := []string{"counted.books > 0"}
	if l.only != "" {
		where = append(where, l.only)
	}
	if after != "" {
		c, err := decodeCursor(after)
		if err != nil || c.Order != list || len(c.Keys) != 1 {
			return NamePage{}, ErrBadCursor
		}
		id, err := uuid.Parse(c.ID)
		if err != nil {
			return NamePage{}, ErrBadCursor
		}
		where = append(where, "("+l.order+", n.id) > ("+arg(c.Keys[0])+", "+arg(id)+")")
	}
	query := `
SELECT n.id, ` + l.name + `, ` + l.key + `, ` + l.order + `, counted.books
FROM ` + l.table + ` n
CROSS JOIN LATERAL (
    SELECT count(DISTINCT b.id) AS books` + l.counted + `
    FROM ` + l.books + ` AND ` + strings.Join(visible, " AND ") + `
) counted
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY ` + l.order + `, n.id
LIMIT ` + arg(limit+1)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return NamePage{}, err
	}
	defer rows.Close()
	page := NamePage{Names: []Name{}}
	var lastID uuid.UUID
	for rows.Next() {
		var n Name
		var id uuid.UUID
		if err := rows.Scan(&id, &n.Name, &n.Key, &n.Sort, &n.Books); err != nil {
			return NamePage{}, err
		}
		if len(page.Names) == limit {
			last := page.Names[limit-1]
			page.Next = encodeCursor(cursor{Order: list, Keys: []string{last.Sort}, ID: lastID.String()})
			break
		}
		page.Names = append(page.Names, n)
		lastID = id
	}
	return page, rows.Err()
}

// FeedFile is a book's file as a catalogue offers it for download.
type FeedFile struct {
	ID     uuid.UUID
	Format string
	Size   int64
}

// FeedBook is what a catalogue says of a book besides its summary.
type FeedBook struct {
	Description string
	Language    string
	UpdatedAt   time.Time
	// Files are its ebook files on disk, the best format first.
	Files []FeedFile
}

// FeedBooks returns, for the books among ids the scope may see, their
// description and the ebook files that may be downloaded.
func (s *Service) FeedBooks(ctx context.Context, scope library.Scope, ids []uuid.UUID) (map[uuid.UUID]FeedBook, error) {
	rows, err := s.pool.Query(ctx, `
SELECT b.id, COALESCE(b.description, ''), COALESCE(b.language, ''), b.updated_at,
       COALESCE(array_agg(f.id ORDER BY f.id) FILTER (WHERE f.id IS NOT NULL), '{}'),
       COALESCE(array_agg(f.format ORDER BY f.id) FILTER (WHERE f.id IS NOT NULL), '{}'),
       COALESCE(array_agg(f.size_bytes ORDER BY f.id) FILTER (WHERE f.id IS NOT NULL), '{}')
FROM books b
LEFT JOIN book_files f ON f.book_id = b.id AND f.kind = '`+KindEbook+`'
     AND f.trashed_at IS NULL AND f.missing_at IS NULL
WHERE b.id = ANY($1::uuid[]) AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids($2, $3))
GROUP BY b.id`, ids, scope.Viewer, scope.SeesAll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]FeedBook{}
	for rows.Next() {
		var id uuid.UUID
		var b FeedBook
		var fileIDs []uuid.UUID
		var formats []string
		var sizes []int64
		if err := rows.Scan(&id, &b.Description, &b.Language, &b.UpdatedAt, &fileIDs, &formats, &sizes); err != nil {
			return nil, err
		}
		for i := range fileIDs {
			b.Files = append(b.Files, FeedFile{ID: fileIDs[i], Format: formats[i], Size: sizes[i]})
		}
		slices.SortStableFunc(b.Files, func(x, y FeedFile) int { return FormatRank(y.Format) - FormatRank(x.Format) })
		out[id] = b
	}
	return out, rows.Err()
}
