// Package dedupe finds books that look like one another: files of the same
// bytes or content, shared ISBNs, the same title by the same author. It
// keeps them as pairs with their evidence for a person to decide about.
package dedupe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
	"github.com/praetorianer777/gotome/backend/internal/notify"
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
	// EvidenceOverlap is text the two share, by the MinHash signatures of
	// their primary text files: the Jaccard estimate and how much of each
	// the other holds.
	EvidenceOverlap = "overlap"
)

// Kinds are the kinds of evidence.
var Kinds = []string{EvidenceSHA256, EvidenceContent, EvidenceISBN, EvidenceTitleAuthor, EvidenceOverlap}

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
	// Events, when set, hears of new pairs.
	Events *notify.Events
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
		shared, err := overlaps(ctx, q, bookID)
		if err != nil {
			return err
		}
		found = append(found, shared...)
		pairs := map[uuid.UUID]uuid.UUID{}
		var others []uuid.UUID
		var ev sqlc.AddDuplicateEvidenceParams
		for _, f := range found {
			pair, ok := pairs[f.Other]
			if !ok {
				a, b := bookID, f.Other
				if b.String() < a.String() {
					a, b = b, a
				}
				row, err := q.UpsertDuplicatePair(ctx, sqlc.UpsertDuplicatePairParams{BookA: a, BookB: b})
				if err != nil {
					return err
				}
				pair = row.ID
				pairs[f.Other] = pair
				if row.Inserted {
					others = append(others, f.Other)
				}
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
		if err := q.DropEmptyDuplicatePairs(ctx, bookID); err != nil {
			return err
		}
		if err := q.RefreshPairScores(ctx, bookID); err != nil {
			return err
		}
		return s.newPairsTx(ctx, tx, bookID, others)
	})
}

// newPairsTx tells of the pairs the book's check made, to those who see
// both books of each: the book's library first, then the others'.
func (s *Service) newPairsTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID, others []uuid.UUID) error {
	if len(others) == 0 || s.Events == nil {
		return nil
	}
	q := sqlc.New(tx)
	book, err := q.GetBookTitleAndLibrary(ctx, bookID)
	if err != nil {
		return err
	}
	libraries, err := q.ListBookLibraries(ctx, others)
	if err != nil {
		return err
	}
	libraries = slices.DeleteFunc(libraries, func(l uuid.UUID) bool { return l == book.LibraryID })
	return s.Events.QueueTx(ctx, tx, notify.Event{
		Kind: notify.KindDuplicatesFound, Libraries: append([]uuid.UUID{book.LibraryID}, libraries...),
		BookID: &bookID, Data: map[string]any{"title": book.Title, "count": len(others)}, Link: "/duplicates",
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
	Score  float32
}

// Pair is two books that look like one, both of which the viewer sees.
type Pair struct {
	ID    uuid.UUID
	State string
	// Score is how sure its strongest evidence makes it, from 0 to 1
	// (duplicate_score in the database).
	Score    float32
	FoundAt  time.Time
	Books    []catalog.Summary
	Evidence []Evidence
}

// MaxPage is the most pairs a page holds.
const MaxPage = 100

// Cursor is where a page ends: the score and ID of its last pair.
type Cursor struct {
	Score float32
	ID    uuid.UUID
}

// ListQuery picks the pairs a list shows.
type ListQuery struct {
	State     string
	LibraryID *uuid.UUID
	// Kind keeps the pairs with evidence of the kind; Least those with a
	// score of at least it.
	Kind  *string
	Least float32
	// After is the cursor of the page before.
	After *Cursor
	Limit int
}

// List returns the pairs of the state of which the scope sees both books,
// the strongest first, and whether more follow.
func (s *Service) List(ctx context.Context, scope library.Scope, lq ListQuery) ([]Pair, bool, error) {
	limit := min(max(lq.Limit, 1), MaxPage)
	q := sqlc.New(s.pool)
	params := sqlc.ListDuplicatePairsParams{
		State: lq.State, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		LibraryID: lq.LibraryID, Kind: lq.Kind, Least: lq.Least, PageSize: int32(limit + 1),
	}
	if lq.After != nil {
		params.AfterID, params.AfterScore = &lq.After.ID, lq.After.Score
	}
	rows, err := q.ListDuplicatePairs(ctx, params)
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
		byPair[e.PairID] = append(byPair[e.PairID], Evidence{Kind: e.Kind, Detail: e.Detail, Score: e.Score})
	}
	out := make([]Pair, 0, len(rows))
	for _, r := range rows {
		a, okA := byID[r.BookA]
		b, okB := byID[r.BookB]
		if !okA || !okB {
			continue
		}
		out = append(out, Pair{
			ID: r.ID, State: r.State, Score: r.Score, FoundAt: r.FoundAt,
			Books: []catalog.Summary{a, b}, Evidence: byPair[r.ID],
		})
	}
	return out, more, nil
}

// ErrNotFound is a pair that does not exist, or one of whose books the
// viewer does not see.
var ErrNotFound = errors.New("no such pair")

// ErrState is a state a person may not set here: merged and replaced are
// what merging and replacing set.
var ErrState = errors.New("a pair is kept both or open")

// Relations a person may record between two books kept both.
var Relations = []string{"edition", "translation", "related"}

// ErrRelation is a relation that is not one of Relations.
var ErrRelation = errors.New("no such relation")

// SetState records what a person decided about a pair of which they see
// both books: that both books stay (kept_both), as another edition, a
// translation or a related book when relation names one, or that it is
// open again. A pair kept both stays so when it is found again.
func (s *Service) SetState(ctx context.Context, scope library.Scope, id uuid.UUID, state, relation string) error {
	if state != StateKeptBoth && state != StateOpen {
		return ErrState
	}
	if relation != "" && (state != StateKeptBoth || !slices.Contains(Relations, relation)) {
		return ErrRelation
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		pair, err := q.GetVisiblePair(ctx, sqlc.GetVisiblePairParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if relation != "" {
			if err := q.RelateBooks(ctx, sqlc.RelateBooksParams{One: pair.BookA, Other: pair.BookB, Kind: relation}); err != nil {
				return err
			}
		}
		return q.SetPairState(ctx, sqlc.SetPairStateParams{ID: id, State: state})
	})
}

// Books are a pair's two books, if the scope sees both, and its state.
func (s *Service) Books(ctx context.Context, scope library.Scope, id uuid.UUID) (a, b uuid.UUID, state string, err error) {
	pair, err := sqlc.New(s.pool).GetVisiblePair(ctx, sqlc.GetVisiblePairParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, b, "", ErrNotFound
	}
	return pair.BookA, pair.BookB, pair.State, err
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
