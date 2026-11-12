package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// PlaceholderReading is where the person stands with a book made for them;
// the zero value is the wishlist.
type PlaceholderReading struct {
	// Status is wishlist, reading or completed.
	Status string
	// Rating is 1 to MaxRating stars, 0 for none.
	Rating int16
	// FinishedOn is the day a completed book was finished; with it the
	// finish counts in its year.
	FinishedOn *time.Time
}

// ErrNeedsLibrary is no way to tell which library should take a book the
// library does not hold: the person has to choose one.
var ErrNeedsLibrary = errors.New("choose a library")

// HistoryLibrary is the library that takes the placeholders of books the
// scope's user read or wishes for, when none was named: the one they see
// that holds most books of the series or by the authors, else the one they
// chose before, else the only one they see. It returns ErrNeedsLibrary when
// there are several and nothing decides.
func (s *Service) HistoryLibrary(ctx context.Context, scope library.Scope, authors []string, series string) (uuid.UUID, error) {
	q := sqlc.New(s.pool)
	keys := []string{}
	for _, a := range authors {
		if k := PersonKey(a); k != "" {
			keys = append(keys, k)
		}
	}
	id, err := q.LibraryHoldingMost(ctx, sqlc.LibraryHoldingMostParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, SeriesKey: Key(series), PersonKeys: keys,
	})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	id, err = q.UserHistoryLibrary(ctx, sqlc.UserHistoryLibraryParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	all, err := q.VisibleLibraryIDs(ctx, sqlc.VisibleLibraryIDsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return uuid.Nil, err
	}
	if len(all) == 1 {
		return all[0], nil
	}
	return uuid.Nil, ErrNeedsLibrary
}

// RememberHistoryLibrary makes a library the one the scope's user's
// placeholders go to from now on, if they have not chosen one yet.
func (s *Service) RememberHistoryLibrary(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	q := sqlc.New(s.pool)
	visible, err := q.IsLibraryVisible(ctx, sqlc.IsLibraryVisibleParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil || !visible {
		return err
	}
	return q.SetUserHistoryLibrary(ctx, sqlc.SetUserHistoryLibraryParams{ID: scope.Viewer, HistoryLibraryID: &id})
}

// CreatePlaceholderTx makes a book the library does not hold yet, with no
// one's state on it.
func CreatePlaceholderTx(ctx context.Context, tx pgx.Tx, in NewBook) (uuid.UUID, error) {
	q := sqlc.New(tx)
	if err := q.HoldLibrary(ctx, in.LibraryID); err != nil {
		return uuid.Nil, err
	}
	id, err := CreateBookTx(ctx, tx, in)
	if err != nil {
		return uuid.Nil, err
	}
	return id, q.SetPlaceholder(ctx, sqlc.SetPlaceholderParams{ID: id, Placeholder: true})
}

// CreatePlaceholder makes a book the library does not hold yet, in a
// library the scope may see, described by the edit, which names the
// provider it comes from as its Source; and puts it on the scope's user's
// wishlist, or, as r says, among the books they read or are reading. It
// returns ErrNotFound for a library the scope may not see, and an
// EditError for an edit that is not valid.
func (s *Service) CreatePlaceholder(ctx context.Context, scope library.Scope, libraryID uuid.UUID, e Edit, r PlaceholderReading) (uuid.UUID, error) {
	if e.Title == nil || clean(*e.Title) == "" {
		return uuid.Nil, EditError{FieldTitle: "A book needs a title."}
	}
	if r.Status == "" {
		r.Status = StatusWishlist
	}
	if r.Status != StatusWishlist && r.Status != StatusReading && r.Status != StatusCompleted {
		return uuid.Nil, EditError{"status": "A new book is wished for, being read or completed."}
	}
	if r.Rating < 0 || r.Rating > MaxRating {
		return uuid.Nil, EditError{"rating": fmt.Sprintf("A rating is 1 to %d stars, or 0 for none.", MaxRating)}
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
		if r.Status == StatusWishlist && r.Rating == 0 {
			wished := StatusWishlist
			_, err = SetReadingTx(ctx, tx, scope, []uuid.UUID{id}, ReadingChange{Status: &wished})
			return err
		}
		_, err = ImportReadingTx(ctx, tx, scope.Viewer, id, ImportedReading{
			Status: r.Status, Rating: r.Rating, FinishedOn: r.FinishedOn, Overwrite: true,
		})
		return err
	})
	return id, err
}
