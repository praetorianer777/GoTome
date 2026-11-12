// Package calibretest writes a small Calibre library for tests: a
// metadata.db with the tables of Calibre's schema that GOtome reads, and
// the Author/Title (id)/ folders. The books are invented.
package calibretest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Book is one row of Calibre's books and what hangs off it.
type Book struct {
	ID          int
	Title       string
	Author      string
	AuthorSort  string
	Series      string
	SeriesIndex float64
	Tags        []string
	Publisher   string
	// PubDate is a timestamp as Calibre writes it; Calibre's "no date" is
	// 0101-01-01.
	PubDate   string
	Language  string
	ISBN      string
	Comments  string
	Rating    int
	Formats   []string
	HasCover  bool
	Read      *bool
	Shelves   []string
	Finished  string
	FileNamed string
}

// Dir is the folder Calibre makes for the book below the library.
func (b Book) Dir() string {
	return filepath.ToSlash(filepath.Join(b.Author, fmt.Sprintf("%s (%d)", b.Title, b.ID)))
}

// Name is the name its files share.
func (b Book) Name() string {
	if b.FileNamed != "" {
		return b.FileNamed
	}
	return b.Title + " - " + b.Author
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

// Books are the books of the fixture library: one in two formats, with a
// series, tags, a rating, a cover and a description; a classic; a book in a
// series at 2.5 whose language Calibre writes as a bibliographic code and
// whose files are not on disk; and one with no date and no rating.
func Books() []Book {
	return []Book{
		{
			ID: 1, Title: "The Lantern at Dusk", Author: "Henrik Halden", AuthorSort: "Halden, Henrik",
			Series: "Halden Chronicles", SeriesIndex: 1, Tags: []string{"Fiction", "Sea"}, Publisher: "Brinewick Press",
			PubDate: "1965-03-04 00:00:00+00:00", Language: "eng", ISBN: "9780195001013",
			Comments: "<div><p>A <b>keeper</b> trims the light.</p><p>Nobody comes.</p></div>",
			Rating:   8, Formats: []string{"EPUB", "PDF"}, HasCover: true, Read: yes(), Shelves: []string{"favourites", "sailing"},
			Finished: "2021-03-14 00:00:00+00:00",
		},
		{
			ID: 2, Title: "Emma", Author: "Jane Austen", AuthorSort: "Austen, Jane", Tags: []string{"Classics"},
			PubDate: "1815-12-23 00:00:00+00:00", Language: "eng", Rating: 3, Formats: []string{"EPUB"}, Read: no(),
		},
		{
			ID: 3, Title: "Die Brücke am Fluss", Author: "Ivo Marek", AuthorSort: "Marek, Ivo",
			Series: "Marek Reihe", SeriesIndex: 2.5, PubDate: "1999-01-01 00:00:00+00:00", Language: "ger",
			Rating: 10, Formats: []string{"EPUB"},
		},
		{
			ID: 4, Title: "Undated Pamphlet", Author: "Unknown", AuthorSort: "Unknown",
			PubDate: "0101-01-01 00:00:00+00:00", Formats: []string{"PDF"},
		},
	}
}

const schema = `
CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL DEFAULT 'Unknown', sort TEXT,
  timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP, pubdate TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  series_index REAL NOT NULL DEFAULT 1.0, author_sort TEXT, isbn TEXT DEFAULT "", lccn TEXT DEFAULT "",
  path TEXT NOT NULL DEFAULT "", flags INTEGER NOT NULL DEFAULT 1, uuid TEXT, has_cover BOOL DEFAULT 0,
  last_modified TIMESTAMP NOT NULL DEFAULT "2000-01-01 00:00:00+00:00");
CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, sort TEXT COLLATE NOCASE, link TEXT NOT NULL DEFAULT "", UNIQUE (name));
CREATE TABLE books_authors_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, author INTEGER NOT NULL, UNIQUE (book, author));
CREATE TABLE series (id INTEGER PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, sort TEXT COLLATE NOCASE, UNIQUE (name));
CREATE TABLE books_series_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, series INTEGER NOT NULL, UNIQUE (book));
CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, UNIQUE (name));
CREATE TABLE books_tags_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, tag INTEGER NOT NULL, UNIQUE (book, tag));
CREATE TABLE publishers (id INTEGER PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE, sort TEXT COLLATE NOCASE, UNIQUE (name));
CREATE TABLE books_publishers_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, publisher INTEGER NOT NULL, UNIQUE (book));
CREATE TABLE languages (id INTEGER PRIMARY KEY, lang_code TEXT NOT NULL COLLATE NOCASE, UNIQUE (lang_code));
CREATE TABLE books_languages_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, lang_code INTEGER NOT NULL, item_order INTEGER NOT NULL DEFAULT 0, UNIQUE (book, lang_code));
CREATE TABLE identifiers (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, type TEXT NOT NULL DEFAULT "isbn" COLLATE NOCASE, val TEXT NOT NULL COLLATE NOCASE, UNIQUE (book, type));
CREATE TABLE comments (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, text TEXT NOT NULL COLLATE NOCASE, UNIQUE (book));
CREATE TABLE ratings (id INTEGER PRIMARY KEY, rating INTEGER CHECK (rating > -1 AND rating < 11), UNIQUE (rating));
CREATE TABLE books_ratings_link (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, rating INTEGER NOT NULL, UNIQUE (book, rating));
CREATE TABLE data (id INTEGER PRIMARY KEY, book INTEGER NOT NULL, format TEXT NOT NULL COLLATE NOCASE, uncompressed_size INTEGER NOT NULL, name TEXT NOT NULL, UNIQUE (book, format));
CREATE TABLE custom_columns (id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT NOT NULL, name TEXT NOT NULL, datatype TEXT NOT NULL,
  mark_for_delete BOOL DEFAULT 0 NOT NULL, editable BOOL DEFAULT 1 NOT NULL, display TEXT DEFAULT "{}" NOT NULL, is_multiple BOOL DEFAULT 0 NOT NULL, normalized BOOL NOT NULL, UNIQUE (label));
CREATE TABLE custom_column_1 (id INTEGER PRIMARY KEY AUTOINCREMENT, book INTEGER, value BOOL NOT NULL, UNIQUE (book));
CREATE TABLE custom_column_2 (id INTEGER PRIMARY KEY AUTOINCREMENT, value TEXT NOT NULL COLLATE NOCASE, UNIQUE (value));
CREATE TABLE books_custom_column_2_link (id INTEGER PRIMARY KEY AUTOINCREMENT, book INTEGER NOT NULL, value INTEGER NOT NULL, UNIQUE (book, value));
CREATE TABLE custom_column_3 (id INTEGER PRIMARY KEY AUTOINCREMENT, book INTEGER, value TIMESTAMP NOT NULL, UNIQUE (book));
INSERT INTO custom_columns (label, name, datatype, is_multiple, normalized) VALUES
  ('read', 'Read', 'bool', 0, 0),
  ('shelf', 'Shelf', 'text', 1, 1),
  ('finished', 'Finished', 'datetime', 0, 0);
`

// Write makes the library in dir: metadata.db, and for each book its folder
// with a file per format, whose content content gives, and cover.jpg when
// the book has one and cover is not nil. A format content gives nil for is
// listed in metadata.db and left off the disk.
func Write(dir string, books []Book, content func(b Book, format string) []byte, cover []byte) error {
	path := filepath.Join(dir, "metadata.db")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(path)
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	ids := map[string]map[string]int64{"authors": {}, "series": {}, "tags": {}, "publishers": {}, "languages": {}, "ratings": {}, "shelves": {}}
	lookup := func(table, column, value string) (int64, error) {
		if id, ok := ids[table][value]; ok {
			return id, nil
		}
		res, err := db.Exec("INSERT INTO "+table+" ("+column+") VALUES (?)", value)
		if err != nil {
			return 0, err
		}
		id, _ := res.LastInsertId()
		ids[table][value] = id
		return id, nil
	}
	for _, b := range books {
		if _, err := db.Exec(`INSERT INTO books (id, title, sort, pubdate, series_index, author_sort, path, uuid, has_cover) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			b.ID, b.Title, b.Title, b.PubDate, max(b.SeriesIndex, 1), b.AuthorSort, b.Dir(), fmt.Sprintf("00000000-0000-4000-8000-%012d", b.ID), b.HasCover); err != nil {
			return err
		}
		link := func(parent, linked, value string) error {
			id, err := lookup(parent, "name", value)
			if err != nil {
				return err
			}
			_, err = db.Exec("INSERT INTO books_"+parent+"_link (book, "+linked+") VALUES (?, ?)", b.ID, id)
			return err
		}
		if b.Author != "" {
			if _, ok := ids["authors"][b.Author]; !ok {
				res, err := db.Exec(`INSERT INTO authors (name, sort) VALUES (?, ?)`, b.Author, b.AuthorSort)
				if err != nil {
					return err
				}
				ids["authors"][b.Author], _ = res.LastInsertId()
			}
			if _, err := db.Exec(`INSERT INTO books_authors_link (book, author) VALUES (?, ?)`, b.ID, ids["authors"][b.Author]); err != nil {
				return err
			}
		}
		if b.Series != "" {
			if err := link("series", "series", b.Series); err != nil {
				return err
			}
		}
		for _, t := range b.Tags {
			if err := link("tags", "tag", t); err != nil {
				return err
			}
		}
		if b.Publisher != "" {
			if err := link("publishers", "publisher", b.Publisher); err != nil {
				return err
			}
		}
		if b.Language != "" {
			id, err := lookup("languages", "lang_code", b.Language)
			if err != nil {
				return err
			}
			if _, err := db.Exec(`INSERT INTO books_languages_link (book, lang_code) VALUES (?, ?)`, b.ID, id); err != nil {
				return err
			}
		}
		if b.ISBN != "" {
			if _, err := db.Exec(`INSERT INTO identifiers (book, type, val) VALUES (?, 'isbn', ?), (?, 'goodreads', ?)`, b.ID, b.ISBN, b.ID, fmt.Sprint(1000+b.ID)); err != nil {
				return err
			}
		}
		if b.Comments != "" {
			if _, err := db.Exec(`INSERT INTO comments (book, text) VALUES (?, ?)`, b.ID, b.Comments); err != nil {
				return err
			}
		}
		if b.Rating > 0 {
			id, ok := ids["ratings"][fmt.Sprint(b.Rating)]
			if !ok {
				res, err := db.Exec(`INSERT INTO ratings (rating) VALUES (?)`, b.Rating)
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				ids["ratings"][fmt.Sprint(b.Rating)] = id
			}
			if _, err := db.Exec(`INSERT INTO books_ratings_link (book, rating) VALUES (?, ?)`, b.ID, id); err != nil {
				return err
			}
		}
		if b.Read != nil {
			if _, err := db.Exec(`INSERT INTO custom_column_1 (book, value) VALUES (?, ?)`, b.ID, *b.Read); err != nil {
				return err
			}
		}
		for _, shelf := range b.Shelves {
			id, ok := ids["shelves"][shelf]
			if !ok {
				res, err := db.Exec(`INSERT INTO custom_column_2 (value) VALUES (?)`, shelf)
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				ids["shelves"][shelf] = id
			}
			if _, err := db.Exec(`INSERT INTO books_custom_column_2_link (book, value) VALUES (?, ?)`, b.ID, id); err != nil {
				return err
			}
		}
		if b.Finished != "" {
			if _, err := db.Exec(`INSERT INTO custom_column_3 (book, value) VALUES (?, ?)`, b.ID, b.Finished); err != nil {
				return err
			}
		}
		for _, format := range b.Formats {
			data := content(b, format)
			if _, err := db.Exec(`INSERT INTO data (book, format, uncompressed_size, name) VALUES (?, ?, ?, ?)`, b.ID, format, len(data), b.Name()); err != nil {
				return err
			}
			if data == nil {
				continue
			}
			full := filepath.Join(dir, filepath.FromSlash(b.Dir()), b.Name()+"."+lower(format))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, data, 0o644); err != nil {
				return err
			}
		}
		if b.HasCover && cover != nil {
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(b.Dir()), "cover.jpg"), cover, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
