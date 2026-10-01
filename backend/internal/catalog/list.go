package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Orders a list of books can be in.
const (
	OrderTitle  = "title"
	OrderAuthor = "author"
	OrderAdded  = "added"
)

// orderKeys are the columns each order sorts by, before the ID that breaks
// ties. The indexes on books cover them.
var orderKeys = map[string][]string{
	OrderTitle:  {"b.sort_title"},
	OrderAuthor: {"b.author_sort", "b.sort_title"},
	OrderAdded:  {"b.created_at"},
}

// MaxPage is the most books one page of a list holds.
const MaxPage = 200

// ErrBadCursor is returned for a cursor that this list did not hand out.
var ErrBadCursor = errors.New("the cursor does not belong to this list")

// ListParams says which books to list, in which order, from where.
type ListParams struct {
	// LibraryID narrows the list to one library; nil lists every library the
	// scope may see.
	LibraryID *uuid.UUID
	// Filter keeps the books its rules match; the zero Node keeps all.
	Filter filter.Node
	Order  string
	Desc   bool
	// After is the cursor of the page before, empty for the first page.
	After string
	Limit int
}

// Summary is a book as a list shows it.
type Summary struct {
	ID            uuid.UUID
	LibraryID     uuid.UUID
	Title         string
	Subtitle      string
	Authors       []string
	Series        string
	SeriesIndex   *float64
	PublishedYear *int32
	CoverKey      string
	// Formats are those of the book's files, each once.
	Formats []string
	AddedAt time.Time
	// Status and Rating are where the viewer stands with the book.
	Status string
	Rating *int16
}

// summaryColumns are what a Summary is read from, in the order of dest. The
// query has books as b, their series as s and summaryJoin.
const summaryColumns = `b.id, b.library_id, b.title, COALESCE(b.subtitle, ''), b.created_at,
       COALESCE(b.cover_key, ''), s.name, b.series_index, extract(year FROM b.published_on)::int,
       COALESCE((SELECT array_agg(a.name ORDER BY c.position)
                 FROM book_contributors c JOIN authors a ON a.id = c.author_id
                 WHERE c.book_id = b.id AND c.role = 'author'), '{}'),
       COALESCE((SELECT array_agg(DISTINCT f.format ORDER BY f.format)
                 FROM book_files f WHERE f.book_id = b.id AND f.trashed_at IS NULL), '{}'),
       COALESCE(ub.status, 'unread'), ub.rating`

// summaryJoin brings in where the viewer, a placeholder, stands with each
// book.
func summaryJoin(viewer string) string {
	return "LEFT JOIN user_books ub ON ub.book_id = b.id AND ub.user_id = " + viewer
}

// dest is where rows.Scan puts summaryColumns; the series name goes to
// series, which may be NULL.
func (b *Summary) dest(series **string) []any {
	return []any{&b.ID, &b.LibraryID, &b.Title, &b.Subtitle, &b.AddedAt,
		&b.CoverKey, series, &b.SeriesIndex, &b.PublishedYear, &b.Authors, &b.Formats, &b.Status, &b.Rating}
}

// Page is one page of a list, and the cursor of the next; Next is empty on
// the last page.
type Page struct {
	Books []Summary
	Next  string
}

// cursor is where a page ends: the sort keys of its last book, and its ID.
type cursor struct {
	Order string   `json:"o"`
	Keys  []string `json:"k"`
	ID    string   `json:"i"`
}

// List returns one page of the books the scope may see. Pages are cut by the
// sort keys of the last book, not by an offset, so that the thousandth page
// costs what the first does and a book added meanwhile moves nothing.
func (s *Service) List(ctx context.Context, scope library.Scope, p ListParams) (Page, error) {
	keys, ok := orderKeys[p.Order]
	if !ok {
		return Page{}, fmt.Errorf("unknown order %q", p.Order)
	}
	limit := min(max(p.Limit, 1), MaxPage)

	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where, err := visibleBooks(scope, p.LibraryID, p.Filter, arg)
	if err != nil {
		return Page{}, err
	}
	if p.After != "" {
		c, err := decodeCursor(p.After)
		if err != nil || c.Order != p.Order || len(c.Keys) != len(keys) {
			return Page{}, ErrBadCursor
		}
		id, err := uuid.Parse(c.ID)
		if err != nil {
			return Page{}, ErrBadCursor
		}
		var values []string
		for i, k := range c.Keys {
			if keys[i] == "b.created_at" {
				t, err := time.Parse(time.RFC3339Nano, k)
				if err != nil {
					return Page{}, ErrBadCursor
				}
				values = append(values, arg(t))
				continue
			}
			values = append(values, arg(k))
		}
		values = append(values, arg(id))
		op := ">"
		if p.Desc {
			op = "<"
		}
		where = append(where, fmt.Sprintf("(%s, b.id) %s (%s)", strings.Join(keys, ", "), op, strings.Join(values, ", ")))
	}
	direction := " ASC"
	if p.Desc {
		direction = " DESC"
	}
	var order []string
	for _, k := range append(keys, "b.id") {
		order = append(order, k+direction)
	}

	viewer := arg(scope.Viewer)
	query := `
SELECT ` + summaryColumns + `, b.sort_title, b.author_sort
FROM books b
LEFT JOIN series s ON s.id = b.series_id
` + summaryJoin(viewer) + `
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY ` + strings.Join(order, ", ") + `
LIMIT ` + arg(limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var page Page
	var sortTitle, authorSort, lastSortTitle, lastAuthorSort string
	for rows.Next() {
		var b Summary
		var series *string
		if err := rows.Scan(append(b.dest(&series), &sortTitle, &authorSort)...); err != nil {
			return Page{}, err
		}
		b.Series = deref(series)
		if len(page.Books) == limit {
			last := page.Books[limit-1]
			c := cursor{Order: p.Order, ID: last.ID.String()}
			switch p.Order {
			case OrderTitle:
				c.Keys = []string{lastSortTitle}
			case OrderAuthor:
				c.Keys = []string{lastAuthorSort, lastSortTitle}
			case OrderAdded:
				c.Keys = []string{last.AddedAt.Format(time.RFC3339Nano)}
			}
			page.Next = encodeCursor(c)
			break
		}
		page.Books = append(page.Books, b)
		lastSortTitle, lastAuthorSort = sortTitle, authorSort
	}
	return page, rows.Err()
}

func encodeCursor(c cursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	return c, err
}

// ErrFileGone is returned for a file the catalogue knows but the disk no
// longer has.
var ErrFileGone = errors.New("the file is not on disk")

// VisibleFile is a file the scope may download, with where it lies.
type VisibleFile struct {
	ID     uuid.UUID
	BookID uuid.UUID
	Format string
	// Name is the file's name, without the folders above it.
	Name   string
	Path   string
	SHA256 []byte
}

// VisibleFileID returns the file's ID if it is a file of a book the scope may
// see, and ErrNotFound otherwise.
func (s *Service) VisibleFileID(ctx context.Context, scope library.Scope, fileID uuid.UUID) (uuid.UUID, error) {
	row, err := sqlc.New(s.pool).GetVisibleFile(ctx, sqlc.GetVisibleFileParams{
		ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return row.ID, err
}

// OpenFile finds a file of a book the scope may see. A file the scope may
// not see is ErrNotFound, like one that does not exist.
func (s *Service) OpenFile(ctx context.Context, scope library.Scope, fileID uuid.UUID) (VisibleFile, *os.File, error) {
	row, err := sqlc.New(s.pool).GetVisibleFile(ctx, sqlc.GetVisibleFileParams{
		ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return VisibleFile{}, nil, ErrNotFound
	}
	if err != nil {
		return VisibleFile{}, nil, err
	}
	if row.MissingAt != nil {
		return VisibleFile{}, nil, ErrFileGone
	}
	path := filepath.Join(row.RootPath, filepath.FromSlash(row.RelPath))
	// The path came from a scan, but a library's folder may have changed
	// since: nothing outside it is served, and no link is followed.
	if rel, err := filepath.Rel(row.RootPath, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return VisibleFile{}, nil, ErrFileGone
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return VisibleFile{}, nil, ErrFileGone
	}
	f, err := os.Open(path)
	if err != nil {
		return VisibleFile{}, nil, ErrFileGone
	}
	return VisibleFile{
		ID: row.ID, BookID: row.BookID, Format: row.Format, Name: filepath.Base(path), Path: path, SHA256: row.Sha256,
	}, f, nil
}
