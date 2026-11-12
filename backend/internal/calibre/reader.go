// Package calibre brings the metadata of a Calibre library into GOtome (#394).
// metadata.db is only ever read, opened immutable, as Calibre may be running.
package calibre

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Database is the file in the root of a Calibre library.
const Database = "metadata.db"

// ErrNotCalibre is a folder without a readable metadata.db of Calibre's.
var ErrNotCalibre = errors.New("not a Calibre library")

// Custom column datatypes, as Calibre stores them.
const (
	TypeText        = "text"
	TypeComments    = "comments"
	TypeSeries      = "series"
	TypeEnumeration = "enumeration"
	TypeDatetime    = "datetime"
	TypeFloat       = "float"
	TypeInt         = "int"
	TypeRating      = "rating"
	TypeBool        = "bool"
	TypeComposite   = "composite"
)

// Reader reads a Calibre library's metadata.db.
type Reader struct {
	db *sql.DB
}

// Open opens the library folder's metadata.db read-only and immutable: no
// lock is taken and nothing is written, not even a journal.
func Open(folder string) (*Reader, error) {
	path := filepath.Join(folder, Database)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrNotCalibre
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Reader{db: db}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('books', 'data', 'custom_columns')`).Scan(&n); err != nil || n != 3 {
		_ = db.Close()
		return nil, ErrNotCalibre
	}
	return r, nil
}

// Close releases the file.
func (r *Reader) Close() error { return r.db.Close() }

// Author is a person as Calibre files them.
type Author struct {
	Name, Sort string
}

// Ident is one identifier of a book, such as an ISBN.
type Ident struct {
	Type, Value string
}

// Format is one file of a book: <Path>/<Name>.<format in lower case>.
type Format struct {
	Format, Name string
}

// Column is a custom column.
type Column struct {
	ID       int64
	Label    string
	Name     string
	Datatype string
	// Books is how many books have a value in it.
	Books int
}

// Book is a row of Calibre's books with everything that hangs off it.
type Book struct {
	ID          int64
	Title       string
	Path        string
	SeriesIndex float64
	HasCover    bool
	PubDate     string
	Authors     []Author
	Series      string
	Tags        []string
	Publisher   string
	Languages   []string
	Identifiers []Ident
	Comments    string
	// Rating is Calibre's 0 to 10.
	Rating  int
	Formats []Format
	// Custom holds each custom column's values by column label, as text.
	Custom map[string][]string
}

// IDs lists every book's id.
func (r *Reader) IDs(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM books ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Columns lists the custom columns, with how many books have a value in
// each when count is set; composite columns,
// which Calibre works out from others, are left out.
func (r *Reader) Columns(ctx context.Context, count bool) ([]Column, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, label, name, datatype FROM custom_columns WHERE mark_for_delete = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var columns []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.Label, &c.Name, &c.Datatype); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if c.Datatype != TypeComposite {
			columns = append(columns, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	if !count {
		return columns, nil
	}
	for i, c := range columns {
		values, err := r.columnValues(ctx, c, nil)
		if err != nil {
			return nil, err
		}
		columns[i].Books = len(values)
	}
	return columns, nil
}

// columnValues reads one custom column's values by book, for the books
// named or all of them. Calibre keeps text, series, enumeration and rating
// columns normalised in a table of values and a link table, and the others
// beside the book.
func (r *Reader) columnValues(ctx context.Context, c Column, ids []int64) (map[int64][]string, error) {
	n := strconv.FormatInt(c.ID, 10)
	var query string
	switch c.Datatype {
	case TypeText, TypeSeries, TypeEnumeration, TypeRating:
		query = `SELECT l.book, v.value FROM books_custom_column_` + n + `_link l JOIN custom_column_` + n + ` v ON v.id = l.value`
		if ids != nil {
			query += ` WHERE l.book IN (` + placeholders(len(ids)) + `)`
		}
		query += ` ORDER BY l.id`
	default:
		query = `SELECT book, value FROM custom_column_` + n + ` WHERE value IS NOT NULL`
		if ids != nil {
			query += ` AND book IN (` + placeholders(len(ids)) + `)`
		}
		query += ` ORDER BY id`
	}
	out := map[int64][]string{}
	err := r.each(ctx, query, ids, func(rows *sql.Rows) error {
		var book int64
		var value any
		if err := rows.Scan(&book, &value); err != nil {
			return err
		}
		if s := text(value); s != "" {
			out[book] = append(out[book], s)
		}
		return nil
	})
	return out, err
}

func text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	}
	return fmt.Sprint(v)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// each runs a query whose last placeholders are the ids and passes its rows on.
func (r *Reader) each(ctx context.Context, query string, ids []int64, f func(*sql.Rows) error) error {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := f(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// chunk is how many ids one query is given; SQLite's limit is 32,766.
const chunk = 500

// Books reads the books with these ids. Books that do not exist are not
// returned.
func (r *Reader) Books(ctx context.Context, ids []int64, columns []Column) ([]Book, error) {
	var out []Book
	for part := range slices.Chunk(ids, chunk) {
		books, err := r.books(ctx, part, columns)
		if err != nil {
			return nil, err
		}
		out = append(out, books...)
	}
	return out, nil
}

func (r *Reader) books(ctx context.Context, ids []int64, columns []Column) ([]Book, error) {
	in := ` IN (` + placeholders(len(ids)) + `)`
	byID := map[int64]*Book{}
	var order []int64
	err := r.each(ctx, `SELECT id, title, path, series_index, has_cover, pubdate FROM books WHERE id`+in+` ORDER BY id`, ids, func(rows *sql.Rows) error {
		var b Book
		var index, pub, cover any
		if err := rows.Scan(&b.ID, &b.Title, &b.Path, &index, &cover, &pub); err != nil {
			return err
		}
		if f, ok := index.(float64); ok {
			b.SeriesIndex = f
		}
		b.HasCover = text(cover) == "1" || text(cover) == "true"
		b.PubDate = text(pub)
		b.Custom = map[string][]string{}
		byID[b.ID] = &b
		order = append(order, b.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	pair := func(query string, f func(b *Book, a, c string)) error {
		return r.each(ctx, query, ids, func(rows *sql.Rows) error {
			var id int64
			var a, c any
			if err := rows.Scan(&id, &a, &c); err != nil {
				return err
			}
			if b := byID[id]; b != nil {
				f(b, text(a), text(c))
			}
			return nil
		})
	}
	queries := []struct {
		sql string
		f   func(b *Book, a, c string)
	}{
		{`SELECT l.book, a.name, a.sort FROM books_authors_link l JOIN authors a ON a.id = l.author WHERE l.book` + in + ` ORDER BY l.id`,
			func(b *Book, a, c string) { b.Authors = append(b.Authors, Author{Name: a, Sort: c}) }},
		{`SELECT l.book, s.name, '' FROM books_series_link l JOIN series s ON s.id = l.series WHERE l.book` + in + ` ORDER BY l.id`,
			func(b *Book, a, _ string) { b.Series = a }},
		{`SELECT l.book, t.name, '' FROM books_tags_link l JOIN tags t ON t.id = l.tag WHERE l.book` + in + ` ORDER BY l.id`,
			func(b *Book, a, _ string) { b.Tags = append(b.Tags, a) }},
		{`SELECT l.book, p.name, '' FROM books_publishers_link l JOIN publishers p ON p.id = l.publisher WHERE l.book` + in + ` ORDER BY l.id`,
			func(b *Book, a, _ string) { b.Publisher = a }},
		{`SELECT l.book, g.lang_code, '' FROM books_languages_link l JOIN languages g ON g.id = l.lang_code WHERE l.book` + in + ` ORDER BY l.item_order, l.id`,
			func(b *Book, a, _ string) { b.Languages = append(b.Languages, a) }},
		{`SELECT book, type, val FROM identifiers WHERE book` + in + ` ORDER BY id`,
			func(b *Book, a, c string) { b.Identifiers = append(b.Identifiers, Ident{Type: a, Value: c}) }},
		{`SELECT book, text, '' FROM comments WHERE book` + in + ` ORDER BY id`,
			func(b *Book, a, _ string) { b.Comments = a }},
		{`SELECT l.book, v.rating, '' FROM books_ratings_link l JOIN ratings v ON v.id = l.rating WHERE l.book` + in + ` ORDER BY l.id`,
			func(b *Book, a, _ string) { b.Rating, _ = strconv.Atoi(a) }},
		{`SELECT book, name, format FROM data WHERE book` + in + ` ORDER BY id`,
			func(b *Book, name, format string) {
				b.Formats = append(b.Formats, Format{Format: strings.ToLower(format), Name: name})
			}},
	}
	for _, q := range queries {
		if err := pair(q.sql, q.f); err != nil {
			return nil, err
		}
	}
	for _, c := range columns {
		values, err := r.columnValues(ctx, c, ids)
		if err != nil {
			return nil, err
		}
		for id, v := range values {
			if b := byID[id]; b != nil {
				b.Custom[c.Label] = v
			}
		}
	}
	out := make([]Book, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}
