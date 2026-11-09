package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Where a person stands with a book.
const (
	StatusUnread    = "unread"
	StatusReading   = "reading"
	StatusCompleted = "completed"
	StatusAbandoned = "abandoned"
	StatusWishlist  = "wishlist"
)

// Statuses lists every status, in the order a person meets them.
var Statuses = []string{StatusUnread, StatusReading, StatusCompleted, StatusAbandoned, StatusWishlist}

// MaxRating is the most stars a book may be given.
const MaxRating = 5

// Reading is where one person stands with a book. It is that person's
// alone: another sees their own.
type Reading struct {
	Status string
	// Rating is 1 to MaxRating stars, nil when the person gave none.
	Rating     *int16
	StartedOn  *time.Time
	FinishedOn *time.Time
	// Note is the person's private note or review of the book.
	Note string
}

// ReadingChange changes where a person stands with books. A nil field is
// left as it is.
type ReadingChange struct {
	// Status set to reading also sets the start, and set to completed the
	// finish, to today, where none is known.
	Status *string
	// Rating 0 takes the rating away.
	Rating *int16
	// StartedOn and FinishedOn are dates as 2006-01-02; "" removes one.
	StartedOn  *string
	FinishedOn *string
}

// check finds what is wrong with the change.
func (c ReadingChange) check() (started, finished *time.Time, err error) {
	problems := EditError{}
	if c.Status != nil && !slices.Contains(Statuses, *c.Status) {
		problems["status"] = "The status is " + strings.Join(Statuses, ", ") + "."
	}
	if c.Rating != nil && (*c.Rating < 0 || *c.Rating > MaxRating) {
		problems["rating"] = fmt.Sprintf("A rating is 1 to %d stars, or 0 for none.", MaxRating)
	}
	date := func(field string, v *string) *time.Time {
		if v == nil || *v == "" {
			return nil
		}
		t, err := time.Parse(time.DateOnly, *v)
		if err != nil {
			problems[field] = "Give the date as 2006-01-02."
		}
		return &t
	}
	started, finished = date("startedOn", c.StartedOn), date("finishedOn", c.FinishedOn)
	if c.Status == nil && c.Rating == nil && c.StartedOn == nil && c.FinishedOn == nil {
		problems["reading"] = "Say what to change."
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return started, finished, nil
}

// SetReading changes where the scope's user stands with the books among ids
// they may see, and returns how many those are. Books they may not see are
// passed over, as if they did not exist. It returns an EditError for a
// change that is not valid.
func (s *Service) SetReading(ctx context.Context, scope library.Scope, ids []uuid.UUID, c ReadingChange) (int, error) {
	var n int
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		n, err = SetReadingTx(ctx, tx, scope, ids, c)
		return err
	})
	return n, err
}

// SetReadingTx is SetReading inside a transaction the caller runs.
func SetReadingTx(ctx context.Context, tx pgx.Tx, scope library.Scope, ids []uuid.UUID, c ReadingChange) (int, error) {
	started, finished, err := c.check()
	if err != nil {
		return 0, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	q := sqlc.New(tx)
	visible, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return 0, err
	}
	for _, id := range visible {
		r, err := readingTx(ctx, q, scope.Viewer, id)
		if err != nil {
			return 0, err
		}
		if c.Status != nil {
			r.Status = *c.Status
			if r.Status == StatusReading && r.StartedOn == nil {
				r.StartedOn = &today
			}
			if r.Status == StatusCompleted && r.FinishedOn == nil {
				r.FinishedOn = &today
			}
		}
		if c.Rating != nil {
			r.Rating = c.Rating
			if *c.Rating == 0 {
				r.Rating = nil
			}
		}
		if c.StartedOn != nil {
			r.StartedOn = started
		}
		if c.FinishedOn != nil {
			r.FinishedOn = finished
		}
		err = q.PutUserBook(ctx, sqlc.PutUserBookParams{
			UserID: scope.Viewer, BookID: id, Status: r.Status, Rating: r.Rating,
			StartedOn: r.StartedOn, FinishedOn: r.FinishedOn,
		})
		if err != nil {
			return 0, err
		}
	}
	return len(visible), nil
}

// ReadingTx is where the user stands with the book, inside a transaction.
func ReadingTx(ctx context.Context, tx pgx.Tx, user, book uuid.UUID) (Reading, error) {
	return readingTx(ctx, sqlc.New(tx), user, book)
}

// readingTx is where the user stands with the book; unread without a row.
func readingTx(ctx context.Context, q *sqlc.Queries, user, book uuid.UUID) (Reading, error) {
	row, err := q.GetUserBook(ctx, sqlc.GetUserBookParams{UserID: user, BookID: book})
	if errors.Is(err, pgx.ErrNoRows) {
		return Reading{Status: StatusUnread}, nil
	}
	if err != nil {
		return Reading{}, err
	}
	return Reading{Status: row.Status, Rating: row.Rating, StartedOn: row.StartedOn, FinishedOn: row.FinishedOn}, nil
}

// ImportedReading is what an import says of where a person stands with a
// book.
type ImportedReading struct {
	// Status is reading, completed, abandoned or wishlist; empty leaves it.
	Status string
	// Rating is 1 to MaxRating stars, 0 for none.
	Rating int16
	// FinishedOn is the day the book was finished, if the import knows it.
	FinishedOn *time.Time
	Note       string
	// Overwrite replaces what the person already set; without it an import
	// only fills what they left open.
	Overwrite bool
}

// ImportReadingTx puts what an import says into where the user stands with
// a book they may see, and reports whether anything changed. What the
// person set already stays unless Overwrite: a status other than unread
// and wishlist, a rating, a finish date, a note. A finish is recorded only
// when the import itself made the book completed, so that running it again
// changes nothing.
func ImportReadingTx(ctx context.Context, tx pgx.Tx, user, book uuid.UUID, in ImportedReading) (bool, error) {
	q := sqlc.New(tx)
	cur, err := readingTx(ctx, q, user, book)
	if err != nil {
		return false, err
	}
	next := cur
	open := cur.Status == StatusUnread || cur.Status == StatusWishlist
	madeCompleted := false
	switch {
	case in.Status == "" || in.Status == cur.Status:
	case in.Status == StatusWishlist:
		if cur.Status == StatusUnread {
			next.Status = StatusWishlist
		}
	case open || in.Overwrite:
		next.Status = in.Status
		madeCompleted = in.Status == StatusCompleted
	}
	if in.Rating > 0 && (cur.Rating == nil || in.Overwrite) {
		rating := in.Rating
		next.Rating = &rating
	}
	if in.FinishedOn != nil && next.Status == StatusCompleted && (cur.FinishedOn == nil || in.Overwrite) {
		next.FinishedOn = in.FinishedOn
	}
	changed := false
	if next.Status != cur.Status || !sameRating(next.Rating, cur.Rating) || !sameDay(next.FinishedOn, cur.FinishedOn) {
		err := q.PutUserBook(ctx, sqlc.PutUserBookParams{
			UserID: user, BookID: book, Status: next.Status, Rating: next.Rating,
			StartedOn: next.StartedOn, FinishedOn: next.FinishedOn,
		})
		if err != nil {
			return false, err
		}
		changed = true
	}
	if in.FinishedOn != nil && next.Status == StatusCompleted && (madeCompleted || in.Overwrite) {
		n, err := q.AddFinishOn(ctx, sqlc.AddFinishOnParams{UserID: user, BookID: book, Day: *in.FinishedOn})
		if err != nil {
			return false, err
		}
		changed = changed || n > 0
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		old, err := q.GetBookNote(ctx, sqlc.GetBookNoteParams{UserID: user, BookID: book})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		if errors.Is(err, pgx.ErrNoRows) || in.Overwrite && old != note {
			if err := q.PutBookNote(ctx, sqlc.PutBookNoteParams{UserID: user, BookID: book, Body: note}); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

func sameRating(a, b *int16) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func sameDay(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Format(time.DateOnly) == b.Format(time.DateOnly)
}

// SetNote sets the scope's user's private note on a book they may see; an
// empty note removes it. It returns ErrNotFound for a book they may not see.
func (s *Service) SetNote(ctx context.Context, scope library.Scope, id uuid.UUID, note string) error {
	note = strings.TrimSpace(note)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		visible, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: []uuid.UUID{id}, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		if len(visible) == 0 {
			return ErrNotFound
		}
		if note == "" {
			return q.DeleteBookNote(ctx, sqlc.DeleteBookNoteParams{UserID: scope.Viewer, BookID: id})
		}
		return q.PutBookNote(ctx, sqlc.PutBookNoteParams{UserID: scope.Viewer, BookID: id, Body: note})
	})
}
