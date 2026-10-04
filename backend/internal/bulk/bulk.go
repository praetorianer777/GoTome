// Package bulk makes one change to many books: an edit, a lookup with the
// metadata providers, or the writing of their metadata into their files. It
// runs as a job, a book at a time, and keeps what it came to for each.
package bulk

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What a bulk change does to each book.
const (
	ActionEdit      = "edit"
	ActionFetch     = "fetch"
	ActionWriteBack = "writeBack"
)

// Actions lists every action.
var Actions = []string{ActionEdit, ActionFetch, ActionWriteBack}

// What a bulk change came to for one book.
const (
	OutcomeChanged   = "changed"
	OutcomeUnchanged = "unchanged"
	// OutcomeLocked is a book left as it was because every field the edit
	// would change is locked.
	OutcomeLocked = "locked"
	// OutcomeReview is a lookup that found matches for a person to review.
	OutcomeReview   = "review"
	OutcomeNotFound = "notFound"
	OutcomeFailed   = "failed"
)

// Outcomes lists every outcome.
var Outcomes = []string{OutcomeChanged, OutcomeUnchanged, OutcomeLocked, OutcomeReview, OutcomeNotFound, OutcomeFailed}

const (
	// budget is how long one run of the job works before it lets other
	// jobs of its queue have a turn.
	budget = 30 * time.Second
	// busyWait is how long a lookup waits for providers that asked to be
	// left alone.
	busyWait    = 15 * time.Minute
	batch       = 20
	jobAttempts = 10
	jobKind     = "bulk.change"
	gone        = "The book is gone, or no longer one you may change."
)

// ErrNotFound is a bulk change that does not exist or is somebody else's.
var ErrNotFound = errors.New("no such bulk change")

// Ingest changes books and has their files written: ingest.Service.
type Ingest interface {
	ChangeTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, c catalog.Change) (catalog.Changed, error)
	WriteBackTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) (int, error)
}

// Fetcher looks a book up with the providers: enrich.Service.
type Fetcher interface {
	Fetch(ctx context.Context, bookID uuid.UUID) (string, error)
}

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service runs bulk changes.
type Service struct {
	pool    *pgxpool.Pool
	books   *catalog.Service
	ingest  Ingest
	fetcher Fetcher
	log     *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
}

// NewService returns a Service. Its Queue must be set before Start is
// called.
func NewService(pool *pgxpool.Pool, ingest Ingest, fetcher Fetcher, log *slog.Logger) *Service {
	return &Service{pool: pool, books: catalog.NewService(pool), ingest: ingest, fetcher: fetcher, log: log}
}

// Start records a bulk change of the books, which the scope's user chose
// among those they may see, and queues the job that makes it. An edit must
// have passed catalog.Change.Check.
func (s *Service) Start(ctx context.Context, scope library.Scope, books []uuid.UUID, action string, change catalog.Change) (uuid.UUID, error) {
	data := []byte("{}")
	if action == ActionEdit {
		var err error
		if data, err = json.Marshal(change); err != nil {
			return uuid.Nil, err
		}
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		var err error
		id, err = q.CreateBulkChange(ctx, sqlc.CreateBulkChangeParams{
			CreatedBy: scope.Viewer, SeesAll: scope.SeesAll, Action: action, Change: data,
		})
		if err != nil {
			return err
		}
		if err := q.AddBulkChangeBooks(ctx, sqlc.AddBulkChangeBooksParams{BulkChangeID: id, BookIds: books}); err != nil {
			return err
		}
		_, err = s.Queue.InsertTx(ctx, tx, Args{BulkChangeID: id}, jobs.InsertOpts{
			Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts,
		})
		return err
	})
	return id, err
}

// Run works through the books of a bulk change whose turn has not come, for
// a while, and reports whether it got to the end. A book's outcome is
// recorded in the transaction that changes it, so a run cut off and run
// again does each book once.
func (s *Service) Run(ctx context.Context, id uuid.UUID) (bool, error) {
	q := sqlc.New(s.pool)
	row, err := q.GetBulkChange(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && row.FinishedAt != nil {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	scope := library.Scope{Viewer: row.CreatedBy, SeesAll: row.SeesAll}
	var change catalog.Change
	if err := json.Unmarshal(row.Change, &change); err != nil {
		return false, err
	}
	started := time.Now()
	for time.Since(started) < budget {
		next, err := q.NextBulkChangeBooks(ctx, sqlc.NextBulkChangeBooksParams{BulkChangeID: id, Limit: batch})
		if err != nil {
			return false, err
		}
		if len(next) == 0 {
			return true, q.FinishBulkChange(ctx, id)
		}
		for _, book := range next {
			if err := s.one(ctx, row.Action, scope, change, id, book); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// one makes the change to one book and records what it came to. It returns
// an error only for what stops the whole run: the job being cut off, the
// database gone, or providers that ask to be left alone.
func (s *Service) one(ctx context.Context, action string, scope library.Scope, change catalog.Change, id, book uuid.UUID) error {
	record := func(q *sqlc.Queries, outcome, message string, skipped []string) error {
		return q.SetBulkChangeOutcome(ctx, sqlc.SetBulkChangeOutcomeParams{
			BulkChangeID: id, BookID: book, Outcome: &outcome, Message: optional(message), Skipped: append([]string{}, skipped...),
		})
	}
	var err error
	switch action {
	case ActionEdit:
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			changed, err := s.ingest.ChangeTx(ctx, tx, scope, book, change)
			if err != nil {
				return err
			}
			outcome := OutcomeUnchanged
			switch {
			case changed.Values || changed.Locks:
				outcome = OutcomeChanged
			case len(changed.Skipped) > 0:
				outcome = OutcomeLocked
			}
			return record(sqlc.New(tx), outcome, "", changed.Skipped)
		})
	case ActionFetch:
		if _, err = s.books.Get(ctx, scope, book); err != nil {
			break
		}
		var found string
		if found, err = s.fetcher.Fetch(ctx, book); err != nil {
			break
		}
		outcome := map[string]string{
			enrich.OutcomeApplied: OutcomeChanged, enrich.OutcomeComplete: OutcomeUnchanged,
			enrich.OutcomeReview: OutcomeReview, enrich.OutcomeNotFound: OutcomeNotFound,
		}[found]
		err = record(sqlc.New(s.pool), outcome, "", nil)
	case ActionWriteBack:
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			_, err := sqlc.New(tx).LockVisibleBook(ctx, sqlc.LockVisibleBookParams{ID: book, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
			if errors.Is(err, pgx.ErrNoRows) {
				return catalog.ErrNotFound
			}
			if err != nil {
				return err
			}
			queued, err := s.ingest.WriteBackTx(ctx, tx, book)
			if err != nil {
				return err
			}
			outcome := OutcomeChanged
			if queued == 0 {
				outcome = OutcomeUnchanged
			}
			return record(sqlc.New(tx), outcome, "", nil)
		})
	}
	if err == nil || ctx.Err() != nil || errors.Is(err, enrich.ErrBusy) {
		return err
	}
	var invalid catalog.EditError
	message := "It could not be changed: " + err.Error()
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		message = gone
	case errors.As(err, &invalid):
		message = strings.Join(slices.Sorted(maps.Values(invalid)), " ")
	default:
		s.log.Warn("bulk change", "id", id, "book", book, "error", err)
	}
	return record(sqlc.New(s.pool), OutcomeFailed, message, nil)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Status is a bulk change and what it came to so far.
type Status struct {
	ID         uuid.UUID
	Action     string
	CreatedAt  time.Time
	FinishedAt *time.Time
	Books      []Result
}

// Result is what a bulk change came to for one book.
type Result struct {
	BookID uuid.UUID
	Title  string
	// Outcome is empty until the book's turn has come.
	Outcome string
	Message string
	Skipped []string
}

// Status returns a bulk change the scope's user asked for, or ErrNotFound,
// with the books of it they may still see.
func (s *Service) Status(ctx context.Context, scope library.Scope, id uuid.UUID) (Status, error) {
	q := sqlc.New(s.pool)
	row, err := q.GetBulkChange(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && row.CreatedBy != scope.Viewer {
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, err
	}
	books, err := q.ListBulkChangeBooks(ctx, sqlc.ListBulkChangeBooksParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Status{}, err
	}
	out := Status{ID: row.ID, Action: row.Action, CreatedAt: row.CreatedAt, FinishedAt: row.FinishedAt, Books: make([]Result, len(books))}
	for i, b := range books {
		out.Books[i] = Result{BookID: b.BookID, Title: b.Title, Outcome: deref(b.Outcome), Message: deref(b.Message), Skipped: b.Skipped}
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Args is the job that makes a bulk change.
type Args struct {
	BulkChangeID uuid.UUID `json:"bulkChangeId"`
}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (Args) Kind() string { return jobKind }

// Worker runs Args.
type Worker struct {
	river.WorkerDefaults[Args]
	Service *Service
}

func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	done, err := w.Service.Run(ctx, job.Args.BulkChangeID)
	switch {
	case errors.Is(err, enrich.ErrBusy):
		// Not a failure: the providers said when to come back, roughly.
		return river.JobSnooze(busyWait)
	case err != nil:
		return err
	case !done:
		return river.JobSnooze(time.Second)
	}
	return nil
}
