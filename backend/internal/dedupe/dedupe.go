// Package dedupe finds books that look like one another: files of the same
// bytes or content, shared ISBNs, the same title by the same author. It
// keeps them as pairs with their evidence for a person to decide about.
package dedupe

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What a pair's evidence says the books share.
const (
	// EvidenceSHA256 is a file of each with the same bytes.
	EvidenceSHA256 = "sha256"
	// EvidenceContent is a file of each with the same content and other
	// metadata: an EPUB whose details were edited.
	EvidenceContent = "content"
	// EvidenceISBN is an ISBN both carry.
	EvidenceISBN = "isbn"
	// EvidenceTitleAuthor is the same title, as compared, and an author in
	// common.
	EvidenceTitleAuthor = "title_author"
)

// Kinds are the kinds of evidence.
var Kinds = []string{EvidenceSHA256, EvidenceContent, EvidenceISBN, EvidenceTitleAuthor}

// What a person decided about a pair: nothing yet, that both stay, or that
// one was merged into or replaced by the other.
const (
	StateOpen     = "open"
	StateKeptBoth = "kept_both"
	StateMerged   = "merged"
	StateReplaced = "replaced"
)

// States are the states of a pair.
var States = []string{StateOpen, StateKeptBoth, StateMerged, StateReplaced}

// Service finds and lists duplicates.
type Service struct {
	pool  *pgxpool.Pool
	books *catalog.Service
	log   *slog.Logger
	Queue *jobs.Runner
}

// NewService returns the Service on the pool; Queue is set once the runner
// exists.
func NewService(pool *pgxpool.Pool, books *catalog.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, books: books, log: log}
}

// Check looks for the books that look like the book and records each pair
// with its evidence. Found again, a pair and its evidence stay as they
// are; evidence of the book's open pairs that is not found again is taken
// back, and an open pair left with none goes. A book that is deleted,
// merged or only wished for has no duplicates.
func (s *Service) Check(ctx context.Context, bookID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		found, err := q.FindDuplicateEvidence(ctx, bookID)
		if err != nil {
			return err
		}
		pairs := map[uuid.UUID]uuid.UUID{}
		var ev sqlc.AddDuplicateEvidenceParams
		for _, f := range found {
			pair, ok := pairs[f.Other]
			if !ok {
				a, b := bookID, f.Other
				if b.String() < a.String() {
					a, b = b, a
				}
				if pair, err = q.UpsertDuplicatePair(ctx, sqlc.UpsertDuplicatePairParams{BookA: a, BookB: b}); err != nil {
					return err
				}
				pairs[f.Other] = pair
			}
			ev.PairIds = append(ev.PairIds, pair)
			ev.Kinds = append(ev.Kinds, f.Kind)
			ev.Details = append(ev.Details, f.Detail)
		}
		if len(found) > 0 {
			if err := q.AddDuplicateEvidence(ctx, ev); err != nil {
				return err
			}
		}
		if err := q.PruneDuplicateEvidence(ctx, sqlc.PruneDuplicateEvidenceParams{
			BookID: bookID, PairIds: ev.PairIds, Kinds: ev.Kinds, Details: ev.Details,
		}); err != nil {
			return err
		}
		return q.DropEmptyDuplicatePairs(ctx, bookID)
	})
}

// EnqueueTx asks for the book to be checked once the transaction that gave
// a reason to, a file of it read, has committed: the check then sees it.
func (s *Service) EnqueueTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
	_, err := s.Queue.InsertTx(ctx, tx, CheckArgs{BookID: bookID}, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: checkAttempts,
	})
	return err
}

// Request queues a check of every visible book, of a library when named,
// and returns how many books that is.
func (s *Service) Request(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (int, error) {
	ids, err := sqlc.New(s.pool).ListLibraryBookIDs(ctx, sqlc.ListLibraryBookIDsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	args := make([]river.JobArgs, len(ids))
	for i, id := range ids {
		args[i] = CheckArgs{BookID: id}
	}
	if _, err := s.Queue.InsertMany(ctx, args, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: checkAttempts,
	}); err != nil {
		return 0, fmt.Errorf("queue duplicate checks: %w", err)
	}
	return len(ids), nil
}

// Evidence is one reason two books look like one: its kind, and what they
// share, a hash, an ISBN or the title and author as compared.
type Evidence struct {
	Kind   string
	Detail string
}

// Pair is two books that look like one, both of which the viewer sees.
type Pair struct {
	ID       uuid.UUID
	State    string
	FoundAt  time.Time
	Books    []catalog.Summary
	Evidence []Evidence
}

// MaxPage is the most pairs a page holds.
const MaxPage = 100

// ListQuery picks the pairs a list shows.
type ListQuery struct {
	State     string
	LibraryID *uuid.UUID
	// Before is the last pair of the page before.
	Before *uuid.UUID
	Limit  int
}

// List returns the pairs of the state of which the scope sees both books,
// newest first, and whether more follow.
func (s *Service) List(ctx context.Context, scope library.Scope, lq ListQuery) ([]Pair, bool, error) {
	limit := min(max(lq.Limit, 1), MaxPage)
	q := sqlc.New(s.pool)
	rows, err := q.ListDuplicatePairs(ctx, sqlc.ListDuplicatePairsParams{
		State: lq.State, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		LibraryID: lq.LibraryID, Before: lq.Before, PageSize: int32(limit + 1),
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	rows = rows[:min(len(rows), limit)]
	if len(rows) == 0 {
		return []Pair{}, false, nil
	}
	ids := make([]uuid.UUID, len(rows))
	var bookIDs []uuid.UUID
	for i, r := range rows {
		ids[i] = r.ID
		bookIDs = append(bookIDs, r.BookA, r.BookB)
	}
	summaries, err := s.books.Summaries(ctx, scope, bookIDs)
	if err != nil {
		return nil, false, err
	}
	byID := map[uuid.UUID]catalog.Summary{}
	for _, b := range summaries {
		byID[b.ID] = b
	}
	evidence, err := q.ListDuplicateEvidence(ctx, ids)
	if err != nil {
		return nil, false, err
	}
	byPair := map[uuid.UUID][]Evidence{}
	for _, e := range evidence {
		byPair[e.PairID] = append(byPair[e.PairID], Evidence{Kind: e.Kind, Detail: e.Detail})
	}
	out := make([]Pair, 0, len(rows))
	for _, r := range rows {
		a, okA := byID[r.BookA]
		b, okB := byID[r.BookB]
		if !okA || !okB {
			continue
		}
		out = append(out, Pair{
			ID: r.ID, State: r.State, FoundAt: r.FoundAt,
			Books: []catalog.Summary{a, b}, Evidence: byPair[r.ID],
		})
	}
	return out, more, nil
}

// checkAttempts is how often a failed check is tried.
const checkAttempts = 5

// CheckArgs is the job that looks for one book's duplicates.
type CheckArgs struct {
	BookID uuid.UUID `json:"bookId"`
}

// checkKind names the job in the queue. It is stored with every job, so it
// stays.
const checkKind = "dedupe.check_book"

func (CheckArgs) Kind() string { return checkKind }

// CheckWorker runs CheckArgs.
type CheckWorker struct {
	river.WorkerDefaults[CheckArgs]
	Service *Service
}

func (w *CheckWorker) Work(ctx context.Context, job *river.Job[CheckArgs]) error {
	return w.Service.Check(ctx, job.Args.BookID)
}
