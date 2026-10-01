// Package enrich looks newly arrived books up with the metadata providers by
// itself. A match sure enough fills in what the book is missing; anything
// less is kept as pending for a person to review.
package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// Match states, as stored.
const (
	StatePending   = "pending"
	StateApplied   = "applied"
	StateDismissed = "dismissed"
)

const (
	// matchDelay lets the other files of a book be read before it is looked
	// up: one lookup then sees what all of them said.
	matchDelay = 30 * time.Second
	// busyWait is how long a match waits for providers that asked to be
	// left alone.
	busyWait = 15 * time.Minute
	// keepAtLeast is the score below which a match is not even worth a
	// person's look; pendingPerBook how many of the rest are kept.
	keepAtLeast    = 0.5
	pendingPerBook = 3
	matchAttempts  = 5
)

// Settings is where the switches are: settings.Store.
type Settings interface {
	Text(ctx context.Context, key string) (string, error)
}

// Editor changes a book as a person's edit does, with the write-back into
// its files that follows: ingest.Service.
type Editor interface {
	Edit(ctx context.Context, scope library.Scope, id uuid.UUID, e catalog.Edit) error
}

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service matches books.
type Service struct {
	pool     *pgxpool.Pool
	meta     *metadata.Service
	editor   Editor
	covers   *covers.Store
	settings Settings
	log      *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
}

// NewService returns a Service. Its Queue must be set before EnqueueTx is
// called.
func NewService(pool *pgxpool.Pool, meta *metadata.Service, editor Editor, store *covers.Store, s Settings, log *slog.Logger) *Service {
	return &Service{pool: pool, meta: meta, editor: editor, covers: store, settings: s, log: log}
}

// EnqueueTx asks for a book to be looked up, inside the transaction that
// gave a reason to: a file of it was read. Asking again while the lookup
// waits is the same lookup.
func (s *Service) EnqueueTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
	on, err := s.settings.Text(ctx, settings.AutoMatch)
	if err != nil || on != "on" {
		return err
	}
	_, err = s.Queue.InsertTx(ctx, tx, MatchArgs{BookID: bookID}, jobs.InsertOpts{
		Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: matchAttempts,
		ScheduledAt: time.Now().Add(matchDelay),
	})
	return err
}

// errBusy is every provider asked having asked to be left alone.
var errBusy = errors.New("the providers ask to be asked later")

// Match looks a book up. The best candidate, when its score reaches the
// threshold, fills in the fields the book lacks; otherwise the best few are
// kept as pending. Run again, it finds the same and changes nothing more.
func (s *Service) Match(ctx context.Context, bookID uuid.UUID) error {
	book, err := catalog.NewService(s.pool).Get(ctx, library.Scope{SeesAll: true}, bookID)
	if errors.Is(err, catalog.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	threshold, err := s.threshold(ctx)
	if err != nil {
		return err
	}
	found, err := s.meta.Candidates(ctx, book)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if len(found) == 0 && errors.Is(err, metadata.ErrBusy) {
			return errBusy
		}
		s.log.Warn("metadata lookup", "book", bookID, "error", err)
		if len(found) == 0 {
			return err
		}
	}
	if len(found) == 0 {
		return nil
	}

	best := found[0]
	if best.Score >= threshold {
		edit, err := s.gaps(ctx, book, best.Record)
		if err != nil {
			return err
		}
		if edit.ChangesValues() {
			if err := s.editor.Edit(ctx, library.Scope{SeesAll: true}, bookID, edit); err != nil {
				return fmt.Errorf("apply %s %s: %w", best.Provider, best.ID, err)
			}
		}
		return s.record(ctx, bookID, best, StateApplied)
	}
	for i, c := range found {
		if i == pendingPerBook || c.Score < keepAtLeast {
			break
		}
		if err := s.record(ctx, bookID, c, StatePending); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) threshold(ctx context.Context) (float64, error) {
	v, err := s.settings.Text(ctx, settings.MatchThreshold)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(v, 64)
}

func (s *Service) record(ctx context.Context, bookID uuid.UUID, c metadata.Candidate, state string) error {
	data, err := json.Marshal(c.Record)
	if err != nil {
		return err
	}
	return sqlc.New(s.pool).RecordMatch(ctx, sqlc.RecordMatchParams{
		BookID: bookID, Provider: c.Provider, RecordID: c.ID, Score: c.Score, Record: data, State: state,
	})
}

// gaps is the edit that takes from a record what the book lacks: empty
// fields, a title guessed from a file name, identifiers it does not have,
// and a cover where it has none. What the book has stays: a file's own
// metadata is the edition at hand, a provider's may be another. Locked
// fields are left to the edit, which skips them.
func (s *Service) gaps(ctx context.Context, book catalog.Book, r metadata.Record) (catalog.Edit, error) {
	e := catalog.Edit{Source: catalog.ProviderSource(r.Provider)}
	text := func(current, theirs string) *string {
		if current == "" && theirs != "" {
			return &theirs
		}
		return nil
	}
	if book.Sources[catalog.FieldTitle] == catalog.SourceFilename && r.Title != "" && r.Title != book.Title {
		e.Title = &r.Title
	}
	e.Subtitle = text(book.Subtitle, r.Subtitle)
	e.Description = text(book.Description, r.Description)
	e.Language = text(book.Language, r.Language)
	e.Published = text(book.Published(), r.Published)
	e.Publisher = text(book.Publisher, r.Publisher)
	if book.Series == "" && r.Series != "" {
		e.Series = &catalog.SeriesPlace{Name: r.Series, Index: r.SeriesIndex}
	}
	if book.PageCount == nil && r.PageCount != nil {
		e.PageCount = r.PageCount
	}
	if len(book.Contributors) == 0 && len(r.Contributors) > 0 {
		e.Contributors = &r.Contributors
	}
	if len(book.Tags) == 0 && len(r.Tags) > 0 {
		e.Tags = &r.Tags
	}
	var own, all []catalog.Identifier
	for _, id := range book.Identifiers {
		all = append(all, id.Identifier)
		if id.FileID == nil {
			own = append(own, id.Identifier)
		}
	}
	added := false
	for _, id := range r.Identifiers {
		if !slices.Contains(all, id) {
			own, all, added = append(own, id), append(all, id), true
		}
	}
	if added {
		e.Identifiers = &own
	}
	if book.CoverKey == "" && r.CoverURL != "" {
		image, err := s.meta.Cover(ctx, r.Provider, r.CoverURL)
		switch {
		case errors.Is(err, metadata.ErrNotFound):
		case err != nil:
			return catalog.Edit{}, fmt.Errorf("cover: %w", err)
		default:
			key, err := s.covers.Put(image)
			if errors.Is(err, covers.ErrNotAnImage) {
				break
			}
			if err != nil {
				return catalog.Edit{}, err
			}
			e.Cover = &key
		}
	}
	return e, nil
}

// MatchArgs is the job that looks a book up.
type MatchArgs struct {
	BookID uuid.UUID `json:"bookId"`
}

// matchKind names the job in the queue. It is stored with every job, so it
// stays.
const matchKind = "enrich.match_book"

func (MatchArgs) Kind() string { return matchKind }

// MatchWorker runs MatchArgs.
type MatchWorker struct {
	river.WorkerDefaults[MatchArgs]
	Service *Service
}

func (w *MatchWorker) Work(ctx context.Context, job *river.Job[MatchArgs]) error {
	err := w.Service.Match(ctx, job.Args.BookID)
	if errors.Is(err, errBusy) {
		// Not a failure: the providers said when to come back, roughly.
		return river.JobSnooze(busyWait)
	}
	return err
}
