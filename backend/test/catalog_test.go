//go:build integration

package test

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/migrations"
)

// shelf is a database with two users and two libraries, which is what the
// catalogue tests need around their books.
type shelf struct {
	pool              *pgxpool.Pool
	books             *catalog.Service
	owner, stranger   uuid.UUID
	shared, private   uuid.UUID
	everything, other library.Scope
}

func newShelf(t *testing.T) *shelf {
	t.Helper()
	ctx := context.Background()
	s := &shelf{pool: dbtest.New(t)}
	s.books = catalog.NewService(s.pool)

	user := func(name string) uuid.UUID {
		var id uuid.UUID
		err := s.pool.QueryRow(ctx, "INSERT INTO users (username, role) VALUES ($1, 'reader') RETURNING id", name).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	lib := func(name, visibility string, owner uuid.UUID) uuid.UUID {
		var id uuid.UUID
		err := s.pool.QueryRow(ctx,
			`INSERT INTO libraries (name, root_path, mode, writable, visibility, owner_id)
			 VALUES ($1, '/books/' || $1, 'managed', true, $2, $3) RETURNING id`, name, visibility, owner).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	s.owner, s.stranger = user("owner"), user("stranger")
	s.shared = lib("shared", "shared", s.owner)
	s.private = lib("private", "private", s.owner)
	s.everything = library.Scope{SeesAll: true}
	s.other = library.Scope{Viewer: s.stranger}
	return s
}

func (s *shelf) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func hash(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func TestBookRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newShelf(t)
	index := 1.0
	pages := int32(1007)

	id, err := s.books.CreateBook(ctx, catalog.NewBook{
		LibraryID:   s.shared,
		Title:       "  The Way of   Kings ",
		Subtitle:    "Book One of the Stormlight Archive",
		Description: "Roshar is a world of stone and storms.",
		Language:    "en",
		Published:   "2010-08-31",
		Publisher:   "Tor Books",
		Series:      "The Stormlight Archive",
		SeriesIndex: &index,
		PageCount:   &pages,
		Contributors: []catalog.NewContributor{
			{Name: "Brandon Sanderson"},
			{Name: "Michael Kramer", Role: catalog.RoleNarrator},
			{Name: "Kate Reading", SortName: "Reading, Kate", Role: catalog.RoleNarrator},
		},
		Tags:        []string{"Fantasy", "Epic", "fantasy", "  "},
		Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780765326355"}},
	})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	book, err := s.books.Get(ctx, s.everything, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if book.Title != "The Way of Kings" || book.SortTitle != "Way of Kings, The" || book.Subtitle != "Book One of the Stormlight Archive" {
		t.Errorf("title %q, sort title %q, subtitle %q", book.Title, book.SortTitle, book.Subtitle)
	}
	if book.PublishedOn == nil || book.PublishedOn.Format(time.DateOnly) != "2010-08-31" || book.PublishedPrecision != catalog.PrecisionDay {
		t.Errorf("published %v (%s)", book.PublishedOn, book.PublishedPrecision)
	}
	if book.Publisher != "Tor Books" || book.Series != "The Stormlight Archive" || book.SeriesIndex == nil || *book.SeriesIndex != 1 {
		t.Errorf("publisher %q, series %q #%v", book.Publisher, book.Series, book.SeriesIndex)
	}
	if got := book.Authors(); !slices.Equal(got, []string{"Brandon Sanderson"}) {
		t.Errorf("authors = %v", got)
	}
	var credits []string
	for _, c := range book.Contributors {
		credits = append(credits, c.Role+":"+c.SortName)
	}
	if !slices.Equal(credits, []string{"author:Sanderson, Brandon", "narrator:Kramer, Michael", "narrator:Reading, Kate"}) {
		t.Errorf("credits = %v", credits)
	}
	// "Fantasy" and "fantasy" are one tag, under the spelling that came first.
	if !slices.Equal(book.Tags, []string{"Epic", "Fantasy"}) {
		t.Errorf("tags = %v", book.Tags)
	}
	if len(book.Identifiers) != 1 || book.Identifiers[0].Value != "9780765326355" || book.Identifiers[0].FileID != nil {
		t.Errorf("identifiers = %+v", book.Identifiers)
	}
	// A book may be wished for before it is owned.
	if len(book.Files) != 0 {
		t.Errorf("a new book has %d files", len(book.Files))
	}
}

func TestNamesMeetUnderOneSpelling(t *testing.T) {
	ctx := context.Background()
	s := newShelf(t)
	for _, b := range []catalog.NewBook{
		{Title: "Jane Eyre", Contributors: []catalog.NewContributor{{Name: "Charlotte Brontë"}}, Publisher: "Penguin Classics", Series: "The Brontës", Published: "1847"},
		{Title: "Villette", Contributors: []catalog.NewContributor{{Name: "charlotte bronte"}}, Publisher: "PENGUIN CLASSICS", Series: "the brontes", Published: "1853-01"},
		{Title: "Shirley", Contributors: []catalog.NewContributor{{Name: "Brontë,  Charlotte"}}},
	} {
		b.LibraryID = s.shared
		if _, err := s.books.CreateBook(ctx, b); err != nil {
			t.Fatalf("%s: %v", b.Title, err)
		}
	}
	// "Charlotte Brontë" and "charlotte bronte" are one author; "Brontë,
	// Charlotte" has other words in another order and is, for now, another.
	if got := s.count(t, "authors"); got != 2 {
		t.Errorf("%d authors, want 2", got)
	}
	var name string
	if err := s.pool.QueryRow(ctx, "SELECT name FROM authors WHERE name_key = 'charlotte bronte'").Scan(&name); err != nil || name != "Charlotte Brontë" {
		t.Errorf("the author is spelled %q (%v), want the spelling that came first", name, err)
	}
	if s.count(t, "publishers") != 1 || s.count(t, "series") != 1 {
		t.Errorf("%d publishers and %d series, want one of each", s.count(t, "publishers"), s.count(t, "series"))
	}

	var precisions []string
	rows, _ := s.pool.Query(ctx, "SELECT coalesce(published_precision, 'none') FROM books ORDER BY title")
	for rows.Next() {
		var p string
		rows.Scan(&p)
		precisions = append(precisions, p)
	}
	if !slices.Equal(precisions, []string{"year", "none", "month"}) {
		t.Errorf("date precisions by title = %v", precisions)
	}
}

func TestFilesOfABook(t *testing.T) {
	ctx := context.Background()
	s := newShelf(t)
	id, err := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.shared, Title: "Dune"})
	if err != nil {
		t.Fatal(err)
	}
	part := func(n int32) *int32 { return &n }
	now := time.Now().UTC().Truncate(time.Second)

	files := []catalog.NewFile{
		{RelPath: "Herbert/Dune/Dune.epub", Format: "EPUB", Size: 1000, ModifiedAt: now, SHA256: hash("epub"),
			Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780441172719"}}},
		{RelPath: "Herbert/Dune/Dune.pdf", Format: ".pdf", Size: 2000, ModifiedAt: now},
		{RelPath: "Herbert/Dune/audio/02.mp3", Format: "mp3", Size: 3000, ModifiedAt: now, PartIndex: part(1)},
		{RelPath: "Herbert/Dune/audio/01.mp3", Format: "mp3", Size: 3000, ModifiedAt: now, PartIndex: part(0),
			Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9781427201430"}}},
	}
	for _, f := range files {
		if _, err := s.books.AddFile(ctx, id, s.shared, f); err != nil {
			t.Fatalf("%s: %v", f.RelPath, err)
		}
	}

	book, err := s.books.Get(ctx, s.everything, id)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, f := range book.Files {
		listed = append(listed, f.Kind+"/"+f.Format+"/"+f.RelPath)
	}
	// Audio parts in playing order, not in the order they were found.
	want := []string{
		"audio/mp3/Herbert/Dune/audio/01.mp3", "audio/mp3/Herbert/Dune/audio/02.mp3",
		"ebook/epub/Herbert/Dune/Dune.epub", "ebook/pdf/Herbert/Dune/Dune.pdf",
	}
	if !slices.Equal(listed, want) {
		t.Errorf("files = %v, want %v", listed, want)
	}
	// The EPUB and the audiobook carry different ISBNs, each tied to its file.
	if len(book.Identifiers) != 2 || book.Identifiers[0].FileID == nil || book.Identifiers[1].FileID == nil ||
		*book.Identifiers[0].FileID == *book.Identifiers[1].FileID {
		t.Errorf("identifiers = %+v", book.Identifiers)
	}

	// What the catalogue refuses.
	if _, err := s.books.AddFile(ctx, id, s.shared, files[0]); !errors.Is(err, catalog.ErrPathTaken) {
		t.Errorf("a second file at the same path: %v, want ErrPathTaken", err)
	}
	if _, err := s.books.AddFile(ctx, id, s.private, catalog.NewFile{RelPath: "x.epub", Format: "epub", ModifiedAt: now}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("a file in another library than its book: %v, want ErrNotFound", err)
	}
	if _, err := s.books.AddFile(ctx, id, s.shared, catalog.NewFile{RelPath: "x.docx", Format: "docx", ModifiedAt: now}); err == nil {
		t.Error("a format GOtome does not know was accepted")
	}
	// The same path in another library is another file.
	otherBook, _ := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.private, Title: "Dune"})
	if _, err := s.books.AddFile(ctx, otherBook, s.private, files[0]); err != nil {
		t.Errorf("the same path in another library: %v", err)
	}
}

func TestBooksFollowTheirLibrary(t *testing.T) {
	ctx := context.Background()
	s := newShelf(t)
	open, _ := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.shared, Title: "Open", Contributors: []catalog.NewContributor{{Name: "A. Writer"}}})
	hidden, _ := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.private, Title: "Hidden", Contributors: []catalog.NewContributor{{Name: "A. Writer"}}})
	s.books.AddFile(ctx, hidden, s.private, catalog.NewFile{RelPath: "h.epub", Format: "epub", ModifiedAt: time.Now()})

	if _, err := s.books.Get(ctx, s.other, open); err != nil {
		t.Errorf("a book in a shared library: %v", err)
	}
	if _, err := s.books.Get(ctx, s.other, hidden); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("a book in somebody else's private library: %v, want ErrNotFound", err)
	}
	if _, err := s.books.Get(ctx, library.Scope{Viewer: s.owner}, hidden); err != nil {
		t.Errorf("the owner's own book: %v", err)
	}
	if _, err := s.books.Get(ctx, s.everything, uuid.New()); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("a book that does not exist: %v, want ErrNotFound", err)
	}

	// A deleted book is not a book any more, even to those who see everything.
	if _, err := s.pool.Exec(ctx, "UPDATE books SET deleted_at = now() WHERE id = $1", open); err != nil {
		t.Fatal(err)
	}
	if _, err := s.books.Get(ctx, s.everything, open); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("a deleted book: %v, want ErrNotFound", err)
	}

	// Removing a library takes its books and their files along; the author,
	// who also wrote elsewhere, stays.
	if _, err := s.pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", s.private); err != nil {
		t.Fatal(err)
	}
	if s.count(t, "books") != 1 || s.count(t, "book_files") != 0 || s.count(t, "authors") != 1 {
		t.Errorf("%d books, %d files, %d authors left", s.count(t, "books"), s.count(t, "book_files"), s.count(t, "authors"))
	}
}

func TestCatalogueConstraints(t *testing.T) {
	ctx := context.Background()
	s := newShelf(t)
	a, _ := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.shared, Title: "A"})
	b, _ := s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.shared, Title: "B"})
	low, high := a, b
	if low.String() > high.String() {
		low, high = high, low
	}

	refused := map[string]string{
		"a date without its precision": "UPDATE books SET published_on = '2010-01-01' WHERE id = '" + a.String() + "'",
		"a hash of the wrong length":   "INSERT INTO book_files (book_id, library_id, kind, format, rel_path, size_bytes, modified_at, sha256) VALUES ('" + a.String() + "', '" + s.shared.String() + "', 'ebook', 'epub', 'x.epub', 1, now(), '\\x00')",
		"an absolute path":             "INSERT INTO book_files (book_id, library_id, kind, format, rel_path, size_bytes, modified_at) VALUES ('" + a.String() + "', '" + s.shared.String() + "', 'ebook', 'epub', '/etc/passwd', 1, now())",
		"a relation stored backwards":  "INSERT INTO book_relations (book_a, book_b, kind) VALUES ('" + high.String() + "', '" + low.String() + "', 'edition')",
		"a book related to itself":     "INSERT INTO book_relations (book_a, book_b, kind) VALUES ('" + a.String() + "', '" + a.String() + "', 'edition')",
		"a role that is none":          "INSERT INTO book_contributors (book_id, author_id, role, position) SELECT '" + a.String() + "', id, 'ghostwriter', 0 FROM authors LIMIT 1",
		"a rating above five":          "UPDATE books SET external_rating = 5.5 WHERE id = '" + a.String() + "'",
	}
	// The role check needs an author to exist.
	s.books.CreateBook(ctx, catalog.NewBook{LibraryID: s.shared, Title: "C", Contributors: []catalog.NewContributor{{Name: "Somebody"}}})
	for name, statement := range refused {
		if _, err := s.pool.Exec(ctx, statement); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO book_relations (book_a, book_b, kind) VALUES ($1, $2, 'translation')", low, high); err != nil {
		t.Errorf("a relation stored the right way round: %v", err)
	}
}

// The catalogue migration has to apply to a database that is already in use,
// not only to an empty one.
func TestCatalogueMigrationOnAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewEmpty(t)

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatalf("migrate to the libraries: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (username, role) VALUES ('steve', 'admin');
		INSERT INTO libraries (name, root_path, mode, writable, visibility) VALUES ('Books', '/books', 'external', false, 'shared')`); err != nil {
		t.Fatal(err)
	}

	applied, err := db.Migrate(ctx, pool)
	if err != nil || applied == 0 {
		t.Fatalf("migrate the rest: %d applied, %v", applied, err)
	}
	var libraries, books int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM libraries), (SELECT count(*) FROM books)").Scan(&libraries, &books); err != nil {
		t.Fatal(err)
	}
	if libraries != 1 || books != 0 {
		t.Errorf("%d libraries and %d books after migrating, want the one library kept", libraries, books)
	}
}
