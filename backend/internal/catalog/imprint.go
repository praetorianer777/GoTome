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
)

// Imprint is what a book's text prints about it: the ISBN as stored, the
// publisher's name and the year, each empty where the text does not say.
type Imprint struct {
	ISBN string
	// PrintISBN is one the text names only beside a print edition. It is
	// never stored as the book's identifier, only asked about at a lookup.
	PrintISBN string
	Publisher string
	Year      string
}

// ApplyImprintTx gives a file's book what the imprint says where the book has
// nothing better, and records it as SourceText. It never takes the place of
// an ISBN, a publisher or a date the book already has from a file's
// metadata, a provider or a person, nor of a locked field; what an earlier
// reading of the text set is replaced by this one.
func ApplyImprintTx(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, format string, im Imprint) error {
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
	var printISBN *string
	if id, ok := NormalizeIdentifier(IDISBN, im.PrintISBN); ok {
		printISBN = &id.Value
	}
	if err := q.SetFileTextPrintISBN(ctx, sqlc.SetFileTextPrintISBNParams{ID: fileID, TextPrintIsbn: printISBN}); err != nil {
		return err
	}
	free := func(field string, empty bool) bool {
		if slices.Contains(book.LockedFields, field) {
			return false
		}
		switch sources[field] {
		case SourceText:
			return true
		case "":
			return empty
		}
		return false
	}

	update := describedAs(book)
	changed := false
	if sources[FieldIdentifiers] == SourceText {
		if err := q.DeleteFileIdentifiersOfType(ctx, sqlc.DeleteFileIdentifiersOfTypeParams{FileID: &fileID, Type: IDISBN}); err != nil {
			return err
		}
		delete(sources, FieldIdentifiers)
		changed = true
	}
	if id, ok := NormalizeIdentifier(IDISBN, im.ISBN); ok && !slices.Contains(book.LockedFields, FieldIdentifiers) && sources[FieldIdentifiers] == "" {
		idents, err := q.ListBookIdentifiers(ctx, book.ID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(idents, func(r sqlc.ListBookIdentifiersRow) bool { return r.Type == IDISBN }) {
			err := q.AddBookIdentifier(ctx, sqlc.AddBookIdentifierParams{BookID: book.ID, FileID: &fileID, Type: IDISBN, Value: id.Value})
			if err != nil {
				return fmt.Errorf("identifier from text: %w", err)
			}
			sources[FieldIdentifiers] = SourceText
			changed = true
		}
	}
	if date, precision, ok := ParsePublished(im.Year); ok && free(FieldPublished, update.PublishedOn == nil) {
		update.PublishedOn, update.PublishedPrecision = &date, &precision
		sources[FieldPublished] = SourceText
		changed = true
	}
	if name := clean(im.Publisher); Key(name) != "" && free(FieldPublisher, update.PublisherID == nil) {
		publisher, err := q.UpsertPublisher(ctx, sqlc.UpsertPublisherParams{Name: name, NameKey: Key(name)})
		if err != nil {
			return fmt.Errorf("publisher: %w", err)
		}
		update.PublisherID = &publisher.ID
		sources[FieldPublisher] = SourceText
		changed = true
	}
	if !changed {
		return nil
	}
	if update.FieldSources, err = json.Marshal(sources); err != nil {
		return err
	}
	return q.UpdateBookDescribed(ctx, update)
}
