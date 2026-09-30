package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// FileMetadata is what a file says about the book it is. An empty field says
// nothing.
type FileMetadata struct {
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
	// Identifiers are recorded for this file, whatever the book already has:
	// the EPUB and the audiobook of one book carry different ISBNs.
	Identifiers []Identifier
	// CoverKey names the cover in the cover store.
	CoverKey string
}

// FileSource is the source recorded for fields that a file filled in.
func FileSource(fileID uuid.UUID) string { return "file:" + fileID.String() }

// ApplyFileMetadataTx describes a file's book with what the file says. A
// field is only written when nothing better is there: it is empty, was
// guessed from a file name, or was last set by this same file. What a person
// locked, and what another file or a provider said, stays.
func ApplyFileMetadataTx(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, m FileMetadata) error {
	q := sqlc.New(tx)
	book, err := q.LockBookOfFile(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	sources := map[string]string{}
	if err := json.Unmarshal(book.FieldSources, &sources); err != nil {
		return fmt.Errorf("field sources of book %s: %w", book.ID, err)
	}
	own := FileSource(fileID)
	// take reports whether the file's value for the field is to be used, and
	// notes the file as the field's source when it is.
	take := func(field string, says, empty bool) bool {
		if !says || slices.Contains(book.LockedFields, field) {
			return false
		}
		switch sources[field] {
		case SourceFilename, own:
		case "":
			if !empty {
				return false
			}
		default:
			return false
		}
		sources[field] = own
		return true
	}

	update := sqlc.UpdateBookDescribedParams{
		ID: book.ID, Title: book.Title, SortTitle: book.SortTitle, TitleKey: book.TitleKey,
		Subtitle: book.Subtitle, Description: book.Description, Language: book.Language,
		PublishedOn: book.PublishedOn, PublishedPrecision: book.PublishedPrecision,
		PublisherID: book.PublisherID, SeriesID: book.SeriesID, SeriesIndex: book.SeriesIndex,
		PageCount: book.PageCount, CoverKey: book.CoverKey,
	}
	text := func(field, value string, current **string) {
		if value = clean(value); take(field, value != "", *current == nil) {
			*current = &value
		}
	}
	text("subtitle", m.Subtitle, &update.Subtitle)
	text("language", m.Language, &update.Language)
	text("cover", m.CoverKey, &update.CoverKey)
	// A description keeps its paragraphs.
	if take("description", m.Description != "", update.Description == nil) {
		update.Description = optional(m.Description)
	}
	// After the language, which decides whether "Die" is an article.
	if title := clean(m.Title); take("title", title != "", false) {
		update.Title = title
		update.TitleKey = Key(title)
	}
	update.SortTitle = SortTitle(update.Title, deref(update.Language))
	if date, precision, ok := ParsePublished(m.Published); take("published", ok, update.PublishedOn == nil) {
		update.PublishedOn, update.PublishedPrecision = &date, &precision
	}
	if name := clean(m.Publisher); take("publisher", Key(name) != "", update.PublisherID == nil) {
		publisher, err := q.UpsertPublisher(ctx, sqlc.UpsertPublisherParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return fmt.Errorf("publisher: %w", err)
		}
		update.PublisherID = &publisher.ID
	}
	if name := clean(m.Series); take("series", Key(name) != "", update.SeriesID == nil) {
		series, err := q.UpsertSeries(ctx, sqlc.UpsertSeriesParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return fmt.Errorf("series: %w", err)
		}
		update.SeriesID, update.SeriesIndex = &series.ID, m.SeriesIndex
	}
	if take("pageCount", m.PageCount != nil, update.PageCount == nil) {
		update.PageCount = m.PageCount
	}

	var credited []NewContributor
	for _, c := range m.Contributors {
		if Key(c.Name) != "" {
			credited = append(credited, c)
		}
	}
	had, err := q.CountBookContributors(ctx, book.ID)
	if err != nil {
		return err
	}
	if take("contributors", len(credited) > 0, had == 0) {
		if err := q.DeleteBookContributors(ctx, book.ID); err != nil {
			return err
		}
		if err := addContributors(ctx, q, book.ID, credited); err != nil {
			return err
		}
	}
	had, err = q.CountBookTags(ctx, book.ID)
	if err != nil {
		return err
	}
	if take("tags", len(m.Tags) > 0, had == 0) {
		if err := q.DeleteBookTags(ctx, book.ID); err != nil {
			return err
		}
		if err := addTags(ctx, q, book.ID, m.Tags); err != nil {
			return err
		}
	}

	if err := q.DeleteFileIdentifiers(ctx, &fileID); err != nil {
		return err
	}
	for _, ident := range m.Identifiers {
		err := q.AddBookIdentifier(ctx, sqlc.AddBookIdentifierParams{
			BookID: book.ID, FileID: &fileID, Type: ident.Type, Value: ident.Value,
		})
		if err != nil {
			return fmt.Errorf("identifier %s: %w", ident.Type, err)
		}
	}

	if update.FieldSources, err = json.Marshal(sources); err != nil {
		return err
	}
	return q.UpdateBookDescribed(ctx, update)
}

// CoverKey returns the key of the cover of a book the scope may see, or
// ErrNotFound when the book is hidden, gone or has no cover.
func (s *Service) CoverKey(ctx context.Context, scope library.Scope, bookID uuid.UUID) (string, error) {
	key, err := sqlc.New(s.pool).GetVisibleBookCover(ctx, sqlc.GetVisibleBookCoverParams{
		ID: bookID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && key == nil) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return *key, nil
}
