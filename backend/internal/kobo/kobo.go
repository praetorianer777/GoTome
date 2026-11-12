// Package kobo keeps what a Kobo reader was told by its library sync
// (#230), which GOtome answers in place of Kobo's store: the books sent to
// the device, as entitlements, each once, again when its details or
// another device's reading place change, and as removed once it leaves
// the device's deliveries.
package kobo

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Change is what a sync tells the reader about one book.
type Change struct {
	BookID uuid.UUID
	// New is a book the reader is told of the first time; Metadata, one
	// whose details changed since; State, one whose reading place another
	// device moved since. Removed is one no longer sent to the device.
	New, Metadata, State, Removed bool
}

// Service keeps what each Kobo reader was told.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Sync works out at most max changes for the device's reader and records
// them as told, and says whether more follow. fresh starts over, for a
// reader that syncs from the start: after a reset it holds nothing. Places
// the client wrote itself are not sent back to it.
func (s *Service) Sync(ctx context.Context, scope library.Scope, deviceID uuid.UUID, client string, fresh bool, max int) ([]Change, bool, error) {
	var out []Change
	more := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if fresh {
			if err := q.ForgetKoboBooks(ctx, deviceID); err != nil {
				return err
			}
		}
		books, err := q.ListKoboLibrary(ctx, sqlc.ListKoboLibraryParams{DeviceID: deviceID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		told, err := q.ListKoboBooks(ctx, deviceID)
		if err != nil {
			return err
		}
		known := make(map[uuid.UUID]sqlc.ListKoboBooksRow, len(told))
		for _, t := range told {
			known[t.BookID] = t
		}
		now := time.Now()
		for _, b := range books {
			t, ok := known[b.ID]
			delete(known, b.ID)
			c := Change{BookID: b.ID, New: !ok}
			theirs := b.ProgressAt != nil && (b.ProgressClient == nil || *b.ProgressClient != client)
			if ok {
				c.Metadata = b.UpdatedAt.After(t.SyncedAt)
				c.State = theirs && (t.ProgressAt == nil || b.ProgressAt.After(*t.ProgressAt))
			} else {
				c.State = b.ProgressAt != nil
			}
			if !c.New && !c.Metadata && !c.State {
				continue
			}
			if len(out) == max {
				more = true
				return nil
			}
			seen := b.ProgressAt
			if ok && !c.State {
				seen = t.ProgressAt
			}
			if err := q.PutKoboBook(ctx, sqlc.PutKoboBookParams{DeviceID: deviceID, BookID: b.ID, SyncedAt: now, ProgressAt: seen}); err != nil {
				return err
			}
			out = append(out, c)
		}
		for id := range known {
			if len(out) == max {
				more = true
				return nil
			}
			if err := q.DropKoboBook(ctx, sqlc.DropKoboBookParams{DeviceID: deviceID, BookID: id}); err != nil {
				return err
			}
			out = append(out, Change{BookID: id, Removed: true})
		}
		return nil
	})
	return out, more, err
}

// ProgressSeen is when the place the reader was last told of, or wrote
// itself, was written: what a place it sends is based on.
func (s *Service) ProgressSeen(ctx context.Context, deviceID, bookID uuid.UUID) (*time.Time, error) {
	told, err := sqlc.New(s.pool).ListKoboBooks(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	for _, t := range told {
		if t.BookID == bookID {
			return t.ProgressAt, nil
		}
	}
	return nil, nil
}

// SawProgress records a place the reader wrote, which it need not be told
// of again.
func (s *Service) SawProgress(ctx context.Context, deviceID, bookID uuid.UUID, at time.Time) error {
	return sqlc.New(s.pool).SetKoboProgressSeen(ctx, sqlc.SetKoboProgressSeenParams{DeviceID: deviceID, BookID: bookID, ProgressAt: &at})
}

// Forget drops the book from what the reader was told, as it archived it.
func (s *Service) Forget(ctx context.Context, deviceID, bookID uuid.UUID) error {
	return sqlc.New(s.pool).DropKoboBook(ctx, sqlc.DropKoboBookParams{DeviceID: deviceID, BookID: bookID})
}
