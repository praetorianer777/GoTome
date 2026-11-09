package shelves

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MaxQueue is the most books a play queue holds.
const MaxQueue = 200

// ErrQueueFull is a queue longer than MaxQueue.
var ErrQueueFull = fmt.Errorf("a queue holds at most %d books", MaxQueue)

// Queue is the scope's play queue: the audiobooks lined up to play, in
// order, of those the scope may still see.
func (s *Service) Queue(ctx context.Context, scope library.Scope) ([]uuid.UUID, error) {
	return sqlc.New(s.pool).ListPlayQueue(ctx, sqlc.ListPlayQueueParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
}

// SetQueue replaces the scope's queue with the books named, in that order.
// A book the scope may not see is left out, and a book named twice is kept
// at its first place. It returns the queue as stored.
func (s *Service) SetQueue(ctx context.Context, scope library.Scope, books []uuid.UUID) ([]uuid.UUID, error) {
	if len(books) > MaxQueue {
		return nil, ErrQueueFull
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.ClearPlayQueue(ctx, scope.Viewer); err != nil {
			return err
		}
		for i, b := range books {
			if err := q.AddToPlayQueue(ctx, sqlc.AddToPlayQueueParams{
				Viewer: scope.Viewer, SeesAll: scope.SeesAll, Book: b, Position: int32(i),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Queue(ctx, scope)
}
