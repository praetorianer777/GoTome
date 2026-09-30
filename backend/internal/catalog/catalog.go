// Package catalog is the books: what a book is, which files it has, who wrote
// it. A book is the title a person shelves and reads; a file is one file on
// disk. The two are kept apart because merging, replacing and finding
// duplicates all move files between books or compare them across books.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Contributor roles, as stored.
const (
	RoleAuthor      = "author"
	RoleNarrator    = "narrator"
	RoleTranslator  = "translator"
	RoleEditor      = "editor"
	RoleIllustrator = "illustrator"
)

// File kinds, as stored.
const (
	KindEbook = "ebook"
	KindAudio = "audio"
)

var (
	// ErrNotFound covers a book that does not exist and one the viewer may
	// not see alike.
	ErrNotFound = errors.New("no such book")
	// ErrPathTaken is returned when the library already has a file at the path.
	ErrPathTaken = errors.New("the library already has a file at this path")
)

// kindOf says whether a format is read or listened to.
var kindOf = map[string]string{
	"epub": KindEbook, "pdf": KindEbook, "mobi": KindEbook, "azw3": KindEbook, "azw": KindEbook,
	"m4b": KindAudio, "m4a": KindAudio, "mp3": KindAudio, "flac": KindAudio, "ogg": KindAudio, "opus": KindAudio,
}

// KindOf returns the kind of a format, and whether GOtome knows the format.
func KindOf(format string) (string, bool) {
	kind, ok := kindOf[strings.ToLower(format)]
	return kind, ok
}

// Contributor is somebody credited on a book.
type Contributor struct {
	ID       uuid.UUID
	Name     string
	SortName string
	Role     string
}

// NewContributor is a credit as a file or a provider gives it.
type NewContributor struct {
	Name string
	// SortName is how the name is filed, when the source says; otherwise it
	// is worked out from Name.
	SortName string
	Role     string
}

// NewBook is what creating a book takes. Only the title is required.
type NewBook struct {
	LibraryID    uuid.UUID
	Title        string
	Subtitle     string
	Description  string
	Language     string
	Published    string
	Publisher    string
	Series       string
	SeriesIndex  *float64
	PageCount    *int32
	Contributors []NewContributor
	Tags         []string
	Identifiers  []Identifier
	// Source says where these values come from, such as SourceFilename. It is
	// recorded per field, so that a better source may later replace a guess
	// and nothing replaces what a person typed.
	Source string
}

// Where a field's value came from, as recorded with the book.
const (
	// SourceFilename is a title read off the name of a file or its folder,
	// for want of anything better.
	SourceFilename = "filename"
)

// NewFile is a file found on disk or uploaded.
type NewFile struct {
	// RelPath is the path below the library's folder, with forward slashes.
	RelPath    string
	Format     string
	Size       int64
	ModifiedAt time.Time
	// SHA256 may be nil for a file that has not been hashed yet.
	SHA256     []byte
	PartIndex  *int32
	UploadedBy *uuid.UUID
	// Identifiers the file itself carries, which belong to this format only.
	Identifiers []Identifier
}

// File is one file of a book.
type File struct {
	ID           uuid.UUID
	Kind         string
	Format       string
	RelPath      string
	Size         int64
	ModifiedAt   time.Time
	SHA256       []byte
	PartIndex    *int32
	ExtractState string
}

// BookIdentifier is an identifier and, when it belongs to one format only,
// the file it came from.
type BookIdentifier struct {
	Identifier
	FileID *uuid.UUID
}

// Book is a book with everything that describes it.
type Book struct {
	ID          uuid.UUID
	LibraryID   uuid.UUID
	Title       string
	SortTitle   string
	Subtitle    string
	Description string
	Language    string
	// PublishedOn is nil when the date is unknown; PublishedPrecision says
	// how much of it is known.
	PublishedOn        *time.Time
	PublishedPrecision string
	Publisher          string
	Series             string
	SeriesIndex        *float64
	PageCount          *int32
	Contributors       []Contributor
	Tags               []string
	Identifiers        []BookIdentifier
	Files              []File
}

// Service reads and writes the catalogue.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// CreateBook adds a book and, with it, whichever of its authors, series,
// publisher and tags the catalogue does not know yet. Those that it knows
// under another spelling are reused.
func (s *Service) CreateBook(ctx context.Context, in NewBook) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = CreateBookTx(ctx, tx, in)
		return err
	})
	return id, err
}

// CreateBookTx is CreateBook inside a transaction the caller runs, for an
// import that adds a book and its files together or not at all.
func CreateBookTx(ctx context.Context, tx pgx.Tx, in NewBook) (uuid.UUID, error) {
	q := sqlc.New(tx)
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return uuid.Nil, errors.New("a book needs a title")
	}
	params := sqlc.CreateBookParams{
		LibraryID:   in.LibraryID,
		Title:       title,
		SortTitle:   SortTitle(title, in.Language),
		TitleKey:    Key(title),
		Subtitle:    optional(in.Subtitle),
		Description: optional(in.Description),
		Language:    optional(in.Language),
		SeriesIndex: in.SeriesIndex,
		PageCount:   in.PageCount,
	}
	if date, precision, ok := ParsePublished(in.Published); ok {
		params.PublishedOn, params.PublishedPrecision = &date, &precision
	}
	if name := clean(in.Publisher); Key(name) != "" {
		publisher, err := q.UpsertPublisher(ctx, sqlc.UpsertPublisherParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("publisher: %w", err)
		}
		params.PublisherID = &publisher.ID
	}
	if name := clean(in.Series); Key(name) != "" {
		series, err := q.UpsertSeries(ctx, sqlc.UpsertSeriesParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("series: %w", err)
		}
		params.SeriesID = &series.ID
	} else {
		// A position in no series means nothing.
		params.SeriesIndex = nil
	}
	sources, err := json.Marshal(fieldSources(params, in.Source))
	if err != nil {
		return uuid.Nil, err
	}
	params.FieldSources = sources

	book, err := q.CreateBook(ctx, params)
	if err != nil {
		return uuid.Nil, fmt.Errorf("book: %w", err)
	}

	if err := addContributors(ctx, q, book.ID, in.Contributors); err != nil {
		return uuid.Nil, err
	}
	if err := addTags(ctx, q, book.ID, in.Tags); err != nil {
		return uuid.Nil, err
	}
	for _, ident := range in.Identifiers {
		err := q.AddBookIdentifier(ctx, sqlc.AddBookIdentifierParams{BookID: book.ID, Type: ident.Type, Value: ident.Value})
		if err != nil {
			return uuid.Nil, fmt.Errorf("identifier %s: %w", ident.Type, err)
		}
	}
	return book.ID, nil
}

func addContributors(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID, contributors []NewContributor) error {
	for position, c := range contributors {
		name := clean(c.Name)
		if Key(name) == "" {
			continue
		}
		sortName := clean(c.SortName)
		if sortName == "" {
			sortName = SortName(name)
		}
		author, err := q.UpsertAuthor(ctx, sqlc.UpsertAuthorParams{Name: name, SortName: sortName, NameKey: Key(name)})
		if err != nil {
			return fmt.Errorf("author %q: %w", name, err)
		}
		role := c.Role
		if role == "" {
			role = RoleAuthor
		}
		err = q.AddBookContributor(ctx, sqlc.AddBookContributorParams{
			BookID: bookID, AuthorID: author.ID, Role: role, Position: int32(position),
		})
		if err != nil {
			return fmt.Errorf("contributor %q: %w", name, err)
		}
	}
	return nil
}

func addTags(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID, tags []string) error {
	for _, name := range tags {
		name = clean(name)
		if Key(name) == "" {
			continue
		}
		tag, err := q.UpsertTag(ctx, sqlc.UpsertTagParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return fmt.Errorf("tag %q: %w", name, err)
		}
		if err := q.AddBookTag(ctx, sqlc.AddBookTagParams{BookID: bookID, TagID: tag.ID}); err != nil {
			return err
		}
	}
	return nil
}

// AddFile attaches a file to a book. The file lands in the book's library:
// the database refuses any other.
func (s *Service) AddFile(ctx context.Context, bookID, libraryID uuid.UUID, in NewFile) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = AddFileTx(ctx, tx, bookID, libraryID, in)
		return err
	})
	return id, err
}

// AddFileTx is AddFile inside a transaction the caller runs.
func AddFileTx(ctx context.Context, tx pgx.Tx, bookID, libraryID uuid.UUID, in NewFile) (uuid.UUID, error) {
	format := strings.ToLower(strings.TrimPrefix(in.Format, "."))
	kind, ok := KindOf(format)
	if !ok {
		return uuid.Nil, fmt.Errorf("%q is not a format GOtome knows", in.Format)
	}
	q := sqlc.New(tx)
	file, err := q.CreateBookFile(ctx, sqlc.CreateBookFileParams{
		BookID: bookID, LibraryID: libraryID, Kind: kind, Format: format,
		RelPath: in.RelPath, SizeBytes: in.Size, ModifiedAt: in.ModifiedAt,
		Sha256: in.SHA256, PartIndex: in.PartIndex, UploadedBy: in.UploadedBy,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "book_files_library_id_rel_path_key":
			return uuid.Nil, ErrPathTaken
		case pgErr.Code == "23503":
			// No such book in that library.
			return uuid.Nil, ErrNotFound
		}
	}
	if err != nil {
		return uuid.Nil, err
	}
	for _, ident := range in.Identifiers {
		err := q.AddBookIdentifier(ctx, sqlc.AddBookIdentifierParams{
			BookID: bookID, FileID: &file.ID, Type: ident.Type, Value: ident.Value,
		})
		if err != nil {
			return uuid.Nil, err
		}
	}
	return file.ID, nil
}

// Get returns a book the scope may see, with its files and credits, or
// ErrNotFound.
func (s *Service) Get(ctx context.Context, scope library.Scope, id uuid.UUID) (Book, error) {
	q := sqlc.New(s.pool)
	row, err := q.GetVisibleBook(ctx, sqlc.GetVisibleBookParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, err
	}
	book := Book{
		ID: row.ID, LibraryID: row.LibraryID, Title: row.Title, SortTitle: row.SortTitle,
		Subtitle: deref(row.Subtitle), Description: deref(row.Description), Language: deref(row.Language),
		PublishedOn: row.PublishedOn, PublishedPrecision: deref(row.PublishedPrecision),
		SeriesIndex: row.SeriesIndex, PageCount: row.PageCount,
	}
	if row.SeriesID != nil {
		series, err := q.GetSeries(ctx, *row.SeriesID)
		if err != nil {
			return Book{}, err
		}
		book.Series = series.Name
	}
	if row.PublisherID != nil {
		publisher, err := q.GetPublisher(ctx, *row.PublisherID)
		if err != nil {
			return Book{}, err
		}
		book.Publisher = publisher.Name
	}

	contributors, err := q.ListBookContributors(ctx, id)
	if err != nil {
		return Book{}, err
	}
	for _, c := range contributors {
		book.Contributors = append(book.Contributors, Contributor{ID: c.ID, Name: c.Name, SortName: c.SortName, Role: c.Role})
	}
	tags, err := q.ListBookTags(ctx, id)
	if err != nil {
		return Book{}, err
	}
	for _, tag := range tags {
		book.Tags = append(book.Tags, tag.Name)
	}
	identifiers, err := q.ListBookIdentifiers(ctx, id)
	if err != nil {
		return Book{}, err
	}
	for _, ident := range identifiers {
		book.Identifiers = append(book.Identifiers, BookIdentifier{
			Identifier: Identifier{Type: ident.Type, Value: ident.Value}, FileID: ident.FileID,
		})
	}
	files, err := q.ListBookFiles(ctx, id)
	if err != nil {
		return Book{}, err
	}
	for _, f := range files {
		book.Files = append(book.Files, File{
			ID: f.ID, Kind: f.Kind, Format: f.Format, RelPath: f.RelPath, Size: f.SizeBytes,
			ModifiedAt: f.ModifiedAt, SHA256: f.Sha256, PartIndex: f.PartIndex, ExtractState: f.ExtractState,
		})
	}
	return book, nil
}

// Authors are the names credited as authors, in order.
func (b Book) Authors() []string {
	var names []string
	for _, c := range b.Contributors {
		if c.Role == RoleAuthor {
			names = append(names, c.Name)
		}
	}
	return names
}

// fieldSources names the source for every field the new book has a value in.
func fieldSources(p sqlc.CreateBookParams, source string) map[string]string {
	sources := map[string]string{}
	if source == "" {
		return sources
	}
	set := map[string]bool{
		"title":       true,
		"subtitle":    p.Subtitle != nil,
		"description": p.Description != nil,
		"language":    p.Language != nil,
		"published":   p.PublishedOn != nil,
		"publisher":   p.PublisherID != nil,
		"series":      p.SeriesID != nil,
		"pageCount":   p.PageCount != nil,
	}
	for field, has := range set {
		if has {
			sources[field] = source
		}
	}
	return sources
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func optional(s string) *string {
	if s = strings.TrimSpace(s); s != "" {
		return &s
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
