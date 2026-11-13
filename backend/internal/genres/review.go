package genres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// ErrNotFound is a genre the book does not have, or a book the viewer
// does not see.
var ErrNotFound = errors.New("no such genre of a book")

// States a person sets.
const (
	Accepted = "accepted"
	Rejected = "rejected"
)

// Suggestion is a genre suggested for a book that nobody has decided.
type Suggestion struct {
	BookID uuid.UUID
	Genre  string
	Method string
	Score  float64
	// Why is the ensemble's reasons, nil for another method.
	Why *Why
}

// Cursor is where a page of suggestions ended.
type Cursor struct {
	Score  float64
	BookID uuid.UUID
	Genre  string
}

// Suggestions are the waiting suggestions of the books the scope sees, of
// one library and one genre when named, the surest first: up to limit
// after the cursor, and the cursor of the next page if there is one.
func (s *Service) Suggestions(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, genre string, after *Cursor, limit int) ([]Suggestion, *Cursor, error) {
	params := sqlc.ListGenreSuggestionsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID, Max: int32(limit + 1),
	}
	if genre != "" {
		params.Genre = &genre
	}
	if after != nil {
		params.AfterScore, params.AfterBook, params.AfterGenre = &after.Score, after.BookID, after.Genre
	}
	rows, err := sqlc.New(s.pool).ListGenreSuggestions(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	var next *Cursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = &Cursor{Score: last.Score, BookID: last.BookID, Genre: last.Genre}
	}
	out := make([]Suggestion, len(rows))
	for i, r := range rows {
		out[i] = Suggestion{BookID: r.BookID, Genre: r.Genre, Score: r.Score}
		if r.Method != nil {
			out[i].Method = *r.Method
		}
		if r.Reasons != nil {
			var w Why
			if err := json.Unmarshal(r.Reasons, &w); err != nil {
				return nil, nil, err
			}
			out[i].Why = &w
		}
	}
	return out, next, nil
}

// Waiting counts the waiting suggestions per genre, of the books the
// scope sees, of one library when named.
func (s *Service) Waiting(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (map[string]int, error) {
	rows, err := sqlc.New(s.pool).CountGenreSuggestions(ctx, sqlc.CountGenreSuggestionsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Genre] = int(r.Waiting)
	}
	return out, nil
}

// Decide accepts or rejects a genre of a book the scope sees, a suggestion
// or one of its tags'. What a person decided no pass changes, and a
// rejected genre is not suggested again.
func (s *Service) Decide(ctx context.Context, scope library.Scope, bookID uuid.UUID, genre, state string) error {
	n, err := sqlc.New(s.pool).DecideGenre(ctx, sqlc.DecideGenreParams{
		BookID: bookID, Genre: genre, State: state, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
