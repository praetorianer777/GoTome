// Package reading keeps where each person is in a book, so that another
// device or session picks up there, and what follows from it: the stretches
// of reading, and each time the end was reached.
package reading

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Media a position is kept for, each on its own: the text of a book and its
// audio are read on different devices at different speeds.
const (
	MediumEbook = "ebook"
	MediumAudio = "audio"
)

// Media lists every medium.
var Media = []string{MediumEbook, MediumAudio}

const (
	// End is the fraction from which a book counts as read to its end: the
	// last page of an EPUB seldom reports 1.
	End = 0.99
	// sessionGap is how long a client may be silent with a session still
	// going on.
	sessionGap = 30 * time.Minute
	maxLocator = 2000
	maxClient  = 100
	maxChapter = 500
)

// ErrNotFound is a book the user may not see, or a file not of the book.
var ErrNotFound = errors.New("no such book")

// Problems says, per field, what is wrong with a position.
type Problems map[string]string

func (p Problems) Error() string { return "the position is not valid" }

// Progress is where a person is in a book in one medium.
type Progress struct {
	Medium string
	FileID *uuid.UUID
	// Locator is the exact place, as the reader writes it: an EPUB CFI, a
	// PDF page, a millisecond of the audio.
	Locator    string
	Fraction   float64
	Chapter    string
	Page       *int32
	PositionMS *int64
	ClientID   string
	UpdatedAt  time.Time
}

// Update is a position a client reports.
type Update struct {
	Progress
	// BasedOn is when the position the client last read from the server
	// was written; nil when it read none.
	BasedOn *time.Time
	// Force writes even over a further position another client wrote.
	Force bool
}

func (u Update) check() error {
	p := Problems{}
	if !slices.Contains(Media, u.Medium) {
		p["medium"] = "The medium is ebook or audio."
	}
	if strings.TrimSpace(u.Locator) == "" || len(u.Locator) > maxLocator {
		p["locator"] = fmt.Sprintf("Give where in the book, in at most %d characters.", maxLocator)
	}
	if u.Fraction < 0 || u.Fraction > 1 {
		p["fraction"] = "The fraction read is between 0 and 1."
	}
	if strings.TrimSpace(u.ClientID) == "" || len(u.ClientID) > maxClient {
		p["clientId"] = fmt.Sprintf("Name the client in at most %d characters.", maxClient)
	}
	if len(u.Chapter) > maxChapter {
		p["chapter"] = fmt.Sprintf("A chapter's name is at most %d characters.", maxChapter)
	}
	if u.Page != nil && *u.Page < 0 || u.PositionMS != nil && *u.PositionMS < 0 {
		p["position"] = "A page or a position cannot be below zero."
	}
	if len(p) > 0 {
		return p
	}
	return nil
}

// State is where a person is in a book, per medium, and how often they read
// it to the end.
type State struct {
	Progress []Progress
	Finishes int
}

// Service keeps positions.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Get returns where the scope's user is in a book they may see, or
// ErrNotFound.
func (s *Service) Get(ctx context.Context, scope library.Scope, bookID uuid.UUID) (State, error) {
	var out State
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := visible(ctx, tx, scope, bookID); err != nil {
			return err
		}
		q := sqlc.New(tx)
		rows, err := q.ListProgress(ctx, sqlc.ListProgressParams{UserID: scope.Viewer, BookID: bookID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out.Progress = append(out.Progress, progressOf(r))
		}
		n, err := q.CountFinishes(ctx, sqlc.CountFinishesParams{UserID: scope.Viewer, BookID: bookID})
		out.Finishes = int(n)
		return err
	})
	return out, err
}

// Save records a position. Last write wins, but for one case: a position
// further on that another client wrote since this one last read is not
// silently overwritten. Then nothing is written, saved is false, and the
// further position is returned for the person to choose; Force writes
// anyway.
//
// The first position of a book makes it one the person is reading, and
// reaching the end, from before it, records a finish and makes it
// completed.
func (s *Service) Save(ctx context.Context, scope library.Scope, bookID uuid.UUID, u Update) (Progress, bool, error) {
	if err := u.check(); err != nil {
		return Progress{}, false, err
	}
	var out Progress
	saved := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := visible(ctx, tx, scope, bookID); err != nil {
			return err
		}
		q := sqlc.New(tx)
		if u.FileID != nil {
			ok, err := q.FileOfBook(ctx, sqlc.FileOfBookParams{ID: *u.FileID, BookID: bookID})
			if err != nil {
				return err
			}
			if !ok {
				return Problems{"fileId": "The file is not one of this book's."}
			}
		}
		before, err := q.LockProgress(ctx, sqlc.LockProgressParams{UserID: scope.Viewer, BookID: bookID, Medium: u.Medium})
		found := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if found && !u.Force && before.ClientID != u.ClientID && before.Fraction > u.Fraction &&
			(u.BasedOn == nil || before.UpdatedAt.After(*u.BasedOn)) {
			out = progressOf(before)
			return nil
		}
		row, err := q.PutProgress(ctx, sqlc.PutProgressParams{
			UserID: scope.Viewer, BookID: bookID, Medium: u.Medium, FileID: u.FileID, Locator: u.Locator,
			Fraction: u.Fraction, Chapter: optional(u.Chapter), Page: u.Page, PositionMs: u.PositionMS, ClientID: u.ClientID,
		})
		if err != nil {
			return err
		}
		out, saved = progressOf(row), true

		extended, err := q.ExtendSession(ctx, sqlc.ExtendSessionParams{
			Fraction: u.Fraction, UserID: scope.Viewer, BookID: bookID, Medium: u.Medium,
			ClientID: u.ClientID, GapSeconds: sessionGap.Seconds(),
		})
		if err != nil {
			return err
		}
		if extended == 0 {
			from := u.Fraction
			if found {
				from = before.Fraction
			}
			err := q.StartSession(ctx, sqlc.StartSessionParams{
				UserID: scope.Viewer, BookID: bookID, Medium: u.Medium, ClientID: u.ClientID,
				FromFraction: from, ToFraction: u.Fraction,
			})
			if err != nil {
				return err
			}
		}
		return s.follow(ctx, tx, scope, bookID, u, found && before.Fraction >= End)
	})
	return out, saved, err
}

// follow moves the book's status on from what a position says: the first
// one makes it one the person is reading, the end a finish.
func (s *Service) follow(ctx context.Context, tx pgx.Tx, scope library.Scope, bookID uuid.UUID, u Update, wasAtEnd bool) error {
	state, err := catalog.ReadingTx(ctx, tx, scope.Viewer, bookID)
	if err != nil {
		return err
	}
	var change catalog.ReadingChange
	if u.Fraction >= End && !wasAtEnd {
		if err := sqlc.New(tx).AddFinish(ctx, sqlc.AddFinishParams{UserID: scope.Viewer, BookID: bookID, Medium: u.Medium}); err != nil {
			return err
		}
		completed, today := catalog.StatusCompleted, time.Now().UTC().Format(time.DateOnly)
		change = catalog.ReadingChange{Status: &completed, FinishedOn: &today}
	} else if state.Status == catalog.StatusUnread || state.Status == catalog.StatusWishlist {
		started := catalog.StatusReading
		change = catalog.ReadingChange{Status: &started}
	} else {
		return nil
	}
	_, err = catalog.SetReadingTx(ctx, tx, scope, []uuid.UUID{bookID}, change)
	return err
}

func visible(ctx context.Context, tx pgx.Tx, scope library.Scope, bookID uuid.UUID) error {
	ids, err := sqlc.New(tx).LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{
		Ids: []uuid.UUID{bookID}, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrNotFound
	}
	return nil
}

func progressOf(r sqlc.ReadingProgress) Progress {
	p := Progress{
		Medium: r.Medium, FileID: r.FileID, Locator: r.Locator, Fraction: r.Fraction, Page: r.Page,
		PositionMS: r.PositionMs, ClientID: r.ClientID, UpdatedAt: r.UpdatedAt,
	}
	if r.Chapter != nil {
		p.Chapter = *r.Chapter
	}
	return p
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
