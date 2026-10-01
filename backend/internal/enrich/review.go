package enrich

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// ErrNoMatch is a match that does not exist, is no longer pending, or is of
// a book the viewer may not see.
var ErrNoMatch = errors.New("no such match")

// Match is a record found for a book.
type Match struct {
	ID     uuid.UUID
	BookID uuid.UUID
	Score  float64
	State  string
	metadata.Record
}

// Waiting is a book and the matches it waits for a person with, best first.
type Waiting struct {
	BookID  uuid.UUID
	Matches []Match
}

// Review lists the books whose matches wait, those that wait longest first,
// and how many such books the scope sees in all.
func (s *Service) Review(ctx context.Context, scope library.Scope, limit int) ([]Waiting, int, error) {
	q := sqlc.New(s.pool)
	ids, err := q.ListReviewBooks(ctx, sqlc.ListReviewBooksParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxBooks: int32(limit)})
	if err != nil {
		return nil, 0, err
	}
	total, err := q.CountReviewBooks(ctx, sqlc.CountReviewBooksParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.ListPendingMatches(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byBook := map[uuid.UUID][]Match{}
	for _, row := range rows {
		m, err := matchOf(row)
		if err != nil {
			return nil, 0, err
		}
		byBook[row.BookID] = append(byBook[row.BookID], m)
	}
	out := make([]Waiting, 0, len(ids))
	for _, id := range ids {
		out = append(out, Waiting{BookID: id, Matches: byBook[id]})
	}
	return out, int(total), nil
}

// Pending returns a match that waits, of a book the scope may see.
func (s *Service) Pending(ctx context.Context, scope library.Scope, id uuid.UUID) (Match, error) {
	row, err := sqlc.New(s.pool).GetVisibleMatch(ctx, sqlc.GetVisibleMatchParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.State != StatePending) {
		return Match{}, ErrNoMatch
	}
	if err != nil {
		return Match{}, err
	}
	return matchOf(row)
}

// Reject dismisses a match. Found again, it stays dismissed.
func (s *Service) Reject(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	if _, err := s.Pending(ctx, scope, id); err != nil {
		return err
	}
	return sqlc.New(s.pool).SetMatchState(ctx, sqlc.SetMatchStateParams{ID: id, State: StateDismissed})
}

// Accepted records that a match was taken, which settles the others its
// book waited with.
func (s *Service) Accepted(ctx context.Context, m Match) error {
	q := sqlc.New(s.pool)
	if err := q.SetMatchState(ctx, sqlc.SetMatchStateParams{ID: m.ID, State: StateApplied}); err != nil {
		return err
	}
	return q.DismissOtherMatches(ctx, sqlc.DismissOtherMatchesParams{BookID: m.BookID, ID: m.ID})
}

func matchOf(row sqlc.MetadataMatch) (Match, error) {
	m := Match{ID: row.ID, BookID: row.BookID, Score: row.Score, State: row.State}
	if err := json.Unmarshal(row.Record, &m.Record); err != nil {
		return Match{}, err
	}
	m.Provider, m.Record.ID = row.Provider, row.RecordID
	return m, nil
}
