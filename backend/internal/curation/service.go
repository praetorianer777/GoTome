package curation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What a run is about.
const (
	KindDuplicates = "duplicates"
	KindCleanup    = "cleanup"
	KindMatches    = "matches"
)

// What an action did.
const (
	ActionMerge   = "merge"
	ActionReplace = "replace"
	ActionEdit    = "edit"
)

// The states of a run.
const (
	StatePlanned   = "planned"
	StateRunning   = "running"
	StateDone      = "done"
	StateConfirmed = "confirmed"
	StateUndone    = "undone"
)

var (
	// ErrNotFound is a run or an action that does not exist, or whose
	// libraries the caller does not see.
	ErrNotFound = errors.New("no such run")
	// ErrNotConfirmable is a run that is not done, or is confirmed already.
	ErrNotConfirmable = errors.New("the run is not waiting for a confirmation")
)

// Hooks are what curation asks of the packages that own files and text.
type Hooks struct {
	// TrashPath is where a trashed file lies.
	TrashPath func(root string, id uuid.UUID, relPath string) string
	// AfterUndoTx follows rows moved back to the books: their text is read
	// again where it is missing, and their duplicates looked for.
	AfterUndoTx func(ctx context.Context, tx pgx.Tx, books []uuid.UUID) error
	// WriteBackTx queues the writing of a book's metadata into its files.
	WriteBackTx func(ctx context.Context, tx pgx.Tx, book uuid.UUID) (int, error)
}

// Service keeps runs and actions, and undoes them.
type Service struct {
	pool  *pgxpool.Pool
	Hooks Hooks
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type runKey struct{}

type runInfo struct {
	ID        uuid.UUID
	Automatic bool
}

// WithRun says that what is changed with the context is an action of the
// run: it records into it instead of making a run of its own.
func WithRun(ctx context.Context, id uuid.UUID, automatic bool) context.Context {
	return context.WithValue(ctx, runKey{}, runInfo{ID: id, Automatic: automatic})
}

// InRun says whether the context belongs to a run.
func InRun(ctx context.Context) bool {
	_, ok := ctx.Value(runKey{}).(runInfo)
	return ok
}

// Deferred says whether the context belongs to an automatic run, whose
// changes are not written into the files until a person confirms it.
func Deferred(ctx context.Context) bool {
	r, ok := ctx.Value(runKey{}).(runInfo)
	return ok && r.Automatic
}

// Pending is the state of some books before an action changes them.
type Pending struct {
	set    Set
	books  []uuid.UUID
	before Snapshot
}

// Begin takes the state of the books, in the transaction that is to change
// them. A nil Service takes nothing, and its Pending is done by Done
// without a record.
func (s *Service) Begin(ctx context.Context, tx pgx.Tx, set Set, books ...uuid.UUID) (*Pending, error) {
	if s == nil {
		return nil, nil
	}
	before, err := Capture(ctx, tx, books, set.tables())
	if err != nil {
		return nil, err
	}
	return &Pending{set: set, books: books, before: before}, nil
}

// Record is what was done, for Done to record.
type Record struct {
	// RunKind and By name the run to make when the context has none: a
	// person's own.
	RunKind string
	By      uuid.UUID
	Kind    string
	// TrashedFiles are the files the action moved to the trash.
	TrashedFiles []uuid.UUID
	Rule         string
	Reason       string
}

// Done records the action in the transaction that did it: the state of the
// books now, the state before, and the run it belongs to.
func (p *Pending) Done(ctx context.Context, tx pgx.Tx, a Record) error {
	if p == nil {
		return nil
	}
	after, err := Capture(ctx, tx, p.books, p.set.tables())
	if err != nil {
		return err
	}
	q := sqlc.New(tx)
	var library uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT library_id FROM books WHERE id = $1`, p.books[0]).Scan(&library); err != nil {
		return err
	}
	run, ok := ctx.Value(runKey{}).(runInfo)
	if !ok {
		id, err := q.CreateCurationRun(ctx, sqlc.CreateCurationRunParams{
			Kind: a.RunKind, StartedBy: &a.By, State: StateDone,
		})
		if err != nil {
			return err
		}
		run = runInfo{ID: id}
	}
	before, err := marshal(p.before)
	if err != nil {
		return err
	}
	afterJSON, err := marshal(after)
	if err != nil {
		return err
	}
	trashed := a.TrashedFiles
	if trashed == nil {
		trashed = []uuid.UUID{}
	}
	var rule *string
	if a.Rule != "" {
		rule = &a.Rule
	}
	_, err = q.InsertCurationAction(ctx, sqlc.InsertCurationActionParams{
		RunID: run.ID, LibraryID: library, Kind: a.Kind, Rule: rule, Reason: a.Reason,
		Books: p.books, TrashedFiles: trashed, Before: before, After: afterJSON,
	})
	return err
}

// StartRun makes a run of a person's or a rule's, running, for actions to
// be recorded into with WithRun, as a bulk change's are.
func (s *Service) StartRun(ctx context.Context, tx pgx.Tx, kind string, by uuid.UUID, bulkChange uuid.UUID) (uuid.UUID, error) {
	return sqlc.New(tx).CreateCurationRun(ctx, sqlc.CreateCurationRunParams{
		Kind: kind, StartedBy: &by, State: StateRunning, BulkChangeID: &bulkChange,
	})
}

// RunOfBulkChange is the run a bulk change's actions are recorded into, if
// it has one.
func (s *Service) RunOfBulkChange(ctx context.Context, bulkChange uuid.UUID) (uuid.UUID, bool, bool, error) {
	if s == nil {
		return uuid.Nil, false, false, nil
	}
	r, err := sqlc.New(s.pool).GetCurationRunOfBulkChange(ctx, &bulkChange)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, false, nil
	}
	return r.ID, r.Automatic, true, err
}

// FinishBulkChange marks the run of a bulk change done.
func (s *Service) FinishBulkChange(ctx context.Context, tx pgx.Tx, bulkChange uuid.UUID) error {
	if s == nil {
		return nil
	}
	return sqlc.New(tx).FinishCurationRun(ctx, &bulkChange)
}

// Confirm accepts a run that is done and that the scope sees: its trashed
// files start their time in the trash, and the metadata it changed is
// written into the files, which an automatic run waited with.
func (s *Service) Confirm(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.GetCurationRun(ctx, sqlc.GetCurationRunParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		run, err := q.GetCurationRunForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if run.State != StateDone {
			return ErrNotConfirmable
		}
		if _, err := q.ConfirmCurationRun(ctx, id); err != nil {
			return err
		}
		if !run.Automatic || s.Hooks.WriteBackTx == nil {
			return nil
		}
		books, err := q.ListCurationRunBooks(ctx, id)
		if err != nil {
			return err
		}
		for _, b := range books {
			if _, err := s.Hooks.WriteBackTx(ctx, tx, b); err != nil {
				return fmt.Errorf("write back %s: %w", b, err)
			}
		}
		return nil
	})
}

func marshal(s Snapshot) ([]byte, error) { return json.Marshal(s) }

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
