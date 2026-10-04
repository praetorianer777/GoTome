package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// FulfilPlaceholderTx gives a placeholder the files it was waiting for. When
// what a file says of itself is the book of a placeholder in its library,
// and the file's book is only what a scan made of it, the book's files move
// to the placeholder, the scan's book is merged into it, and the
// placeholder's ID is returned; otherwise the file's own book's. The
// placeholder keeps what it was made with; the file fills in what it lacks
// once its metadata is applied to it.
func FulfilPlaceholderTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID, m FileMetadata) (uuid.UUID, error) {
	q := sqlc.New(tx)
	untouched, err := q.BookIsUntouched(ctx, bookID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !untouched {
		return bookID, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	book, err := q.GetVisibleBook(ctx, sqlc.GetVisibleBookParams{ID: bookID, SeesAll: true})
	if err != nil {
		return uuid.Nil, err
	}
	isbns := []string{}
	for _, id := range m.Identifiers {
		if id.Type == IDISBN {
			isbns = append(isbns, id.Value)
		}
	}
	authors := []string{}
	for _, c := range m.Contributors {
		if c.Role == "" || c.Role == RoleAuthor {
			if k := Key(c.Name); k != "" {
				authors = append(authors, k)
			}
		}
	}
	if len(isbns) == 0 && (Key(m.Title) == "" || len(authors) == 0) {
		return bookID, nil
	}
	placeholder, err := q.FindPlaceholder(ctx, sqlc.FindPlaceholderParams{
		LibraryID: book.LibraryID, Isbns: isbns, TitleKey: Key(m.Title), AuthorKeys: authors,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return bookID, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if err := q.MoveFileIdentifiers(ctx, sqlc.MoveFileIdentifiersParams{ToBook: placeholder, FromBook: bookID}); err != nil {
		return uuid.Nil, err
	}
	if err := q.MoveBookFiles(ctx, sqlc.MoveBookFilesParams{ToBook: placeholder, FromBook: bookID}); err != nil {
		return uuid.Nil, err
	}
	if err := q.MergeBookInto(ctx, sqlc.MergeBookIntoParams{ID: bookID, IntoBook: &placeholder}); err != nil {
		return uuid.Nil, err
	}
	if err := q.SetPlaceholder(ctx, sqlc.SetPlaceholderParams{ID: placeholder, Placeholder: false}); err != nil {
		return uuid.Nil, err
	}
	return placeholder, nil
}

// CreatePlaceholder makes a book the library does not hold yet, in a
// library the scope may see, described by the edit, which names the
// provider it comes from as its Source; and puts it on the scope's user's
// wishlist. It returns ErrNotFound for a library the scope may not see, and
// an EditError for an edit that is not valid.
func (s *Service) CreatePlaceholder(ctx context.Context, scope library.Scope, libraryID uuid.UUID, e Edit) (uuid.UUID, error) {
	if e.Title == nil || clean(*e.Title) == "" {
		return uuid.Nil, EditError{FieldTitle: "A book needs a title."}
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		visible, err := q.IsLibraryVisible(ctx, sqlc.IsLibraryVisibleParams{ID: libraryID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		if !visible {
			return ErrNotFound
		}
		if id, err = CreateBookTx(ctx, tx, NewBook{LibraryID: libraryID, Title: *e.Title, Source: e.Source}); err != nil {
			return err
		}
		if err := EditTx(ctx, tx, scope, id, e); err != nil {
			return err
		}
		if err := q.SetPlaceholder(ctx, sqlc.SetPlaceholderParams{ID: id, Placeholder: true}); err != nil {
			return err
		}
		wished := StatusWishlist
		_, err = SetReadingTx(ctx, tx, scope, []uuid.UUID{id}, ReadingChange{Status: &wished})
		return err
	})
	return id, err
}
