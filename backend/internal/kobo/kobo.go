// Package kobo keeps what a Kobo reader was told by its library sync
// (#230), which GOtome answers in place of Kobo's store: the books sent to
// the device, as entitlements, each once, again when its details or
// another device's reading place change, and as removed once it leaves
// the device's deliveries.
package kobo

import (
	"context"
	"slices"
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
			if err := q.ForgetKoboTags(ctx, deviceID); err != nil {
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

// ReadingMinutes takes what a reader reports as the minutes it has counted
// reading the book in all, and returns the part beyond what was reported
// before: each minute once, however often the same figure is sent. A figure
// below the last, as after the reader was reset, is the new count to go on
// from.
func (s *Service) ReadingMinutes(ctx context.Context, deviceID, bookID uuid.UUID, total int) (int, error) {
	delta := 0
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.SeedKoboReadingTime(ctx, sqlc.SeedKoboReadingTimeParams{DeviceID: deviceID, BookID: bookID}); err != nil {
			return err
		}
		seen, err := q.LockKoboReadingTime(ctx, sqlc.LockKoboReadingTimeParams{DeviceID: deviceID, BookID: bookID})
		if err != nil {
			return err
		}
		if int(seen) == total {
			return nil
		}
		if int(seen) < total {
			delta = total - int(seen)
		}
		return q.SetKoboReadingTime(ctx, sqlc.SetKoboReadingTimeParams{DeviceID: deviceID, BookID: bookID, Minutes: int32(total)})
	})
	return delta, err
}

// Tag is a collection as the reader holds it: a Kobo "tag", with the books
// on it that are on the device.
type Tag struct {
	ID               uuid.UUID
	Name             string
	Created, Updated time.Time
	Books            []uuid.UUID
}

// TagChange is what a sync tells the reader of one tag: new, changed (its
// name or its books), or, for Deleted, gone, with only the ID set.
type TagChange struct {
	Tag     Tag
	New     bool
	Deleted bool
}

// OnDevice is the books the reader was told it holds.
func (s *Service) OnDevice(ctx context.Context, deviceID uuid.UUID) ([]uuid.UUID, error) {
	told, err := sqlc.New(s.pool).ListKoboBooks(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, len(told))
	for i, t := range told {
		out[i] = t.BookID
	}
	return out, nil
}

// SyncTags compares the tags the reader should hold with those it was told
// of and records the difference as told: each change once. A tag it was
// told of that is not wanted any more is deleted.
func (s *Service) SyncTags(ctx context.Context, deviceID uuid.UUID, wanted []Tag) ([]TagChange, error) {
	var out []TagChange
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		told, err := q.ListKoboTags(ctx, deviceID)
		if err != nil {
			return err
		}
		known := make(map[uuid.UUID]sqlc.ListKoboTagsRow, len(told))
		for _, t := range told {
			known[t.TagID] = t
		}
		for _, w := range wanted {
			t, ok := known[w.ID]
			delete(known, w.ID)
			if ok && t.Name == w.Name && sameBooks(t.BookIds, w.Books) {
				continue
			}
			if err := q.PutKoboTag(ctx, sqlc.PutKoboTagParams{DeviceID: deviceID, TagID: w.ID, Name: w.Name, BookIds: nonNil(w.Books)}); err != nil {
				return err
			}
			out = append(out, TagChange{Tag: w, New: !ok})
		}
		for id := range known {
			if err := q.DropKoboTag(ctx, sqlc.DropKoboTagParams{DeviceID: deviceID, TagID: id}); err != nil {
				return err
			}
			out = append(out, TagChange{Tag: Tag{ID: id}, Deleted: true})
		}
		return nil
	})
	return out, err
}

// RecordTag notes that the reader holds the tag as given, after a change
// it sent itself, so that the sync does not send it back.
func (s *Service) RecordTag(ctx context.Context, deviceID uuid.UUID, t Tag) error {
	return sqlc.New(s.pool).PutKoboTag(ctx, sqlc.PutKoboTagParams{DeviceID: deviceID, TagID: t.ID, Name: t.Name, BookIds: nonNil(t.Books)})
}

// ForgetTag drops what the reader was told of a tag, so the next sync tells
// it anew: how a change the reader made to a tag it may not change is undone.
func (s *Service) ForgetTag(ctx context.Context, deviceID, tagID uuid.UUID) error {
	return sqlc.New(s.pool).DropKoboTag(ctx, sqlc.DropKoboTagParams{DeviceID: deviceID, TagID: tagID})
}

func nonNil(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func sameBooks(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			return false
		}
	}
	return true
}
