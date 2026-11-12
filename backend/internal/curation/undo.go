package curation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What an undo came to for one action.
const (
	OutcomeUndone = "undone"
	// OutcomeAlready is an action undone before.
	OutcomeAlready = "alreadyUndone"
	// OutcomeConflict is an action left alone because its books changed
	// since.
	OutcomeConflict = "conflict"
	// OutcomeNotUndoable is an action whose trashed files are gone.
	OutcomeNotUndoable = "notUndoable"
	OutcomeFailed      = "failed"
)

var (
	// ErrRunning is a run that still makes actions.
	ErrRunning = errors.New("the run is still going")
	// ErrDryRun is a run that only said what it would do.
	ErrDryRun = errors.New("the run changed nothing")
)

// Outcome is what undoing one action came to.
type Outcome struct {
	ActionID  uuid.UUID  `json:"actionId"`
	State     string     `json:"state"`
	Message   string     `json:"message,omitempty"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
}

type move struct{ from, to string }

// UndoAction puts the books of an action the scope sees back as they were
// before it, in one transaction, unless something about them changed since
// (a conflict, which is reported and left as it is) or the files it
// trashed are gone from the trash.
func (s *Service) UndoAction(ctx context.Context, scope library.Scope, id uuid.UUID) (Outcome, error) {
	out := Outcome{ActionID: id}
	var made []move
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		a, err := q.GetCurationActionForUpdate(ctx, sqlc.GetCurationActionForUpdateParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if a.State != "done" {
			out.State = OutcomeAlready
			return nil
		}
		if a.DryRun {
			out.State, out.Message = OutcomeNotUndoable, "A dry run changed nothing."
			return nil
		}
		if err := q.HoldLibrariesOfBooks(ctx, a.Books); err != nil {
			return err
		}
		stop := func(state, message string, conflicts []Conflict) error {
			out.State, out.Message, out.Conflicts = state, message, conflicts
			return q.SetCurationActionNote(ctx, sqlc.SetCurationActionNoteParams{ID: id, Note: message})
		}

		var moves []move
		if len(a.TrashedFiles) > 0 {
			if err := q.LockLibraryFiles(ctx, a.LibraryID); err != nil {
				return err
			}
			var problem string
			moves, problem, err = s.filesToRestore(ctx, q, a.TrashedFiles)
			if err != nil {
				return err
			}
			if problem != "" {
				return stop(OutcomeNotUndoable, problem, nil)
			}
		}
		var after Snapshot
		if err := jsonUnmarshal(a.After, &after); err != nil {
			return err
		}
		conflicts, err := Conflicts(ctx, tx, a.Books, after)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			names := make([]string, len(conflicts))
			for i, c := range conflicts {
				names[i] = fmt.Sprintf("%s (%d)", c.Table, c.Rows)
			}
			return stop(OutcomeConflict, "Changed since: "+strings.Join(names, ", "), conflicts)
		}
		var before Snapshot
		if err := jsonUnmarshal(a.Before, &before); err != nil {
			return err
		}
		if err := Restore(ctx, tx, a.Books, before); err != nil {
			return err
		}
		if s.Hooks.AfterUndoTx != nil {
			if err := s.Hooks.AfterUndoTx(ctx, tx, a.Books); err != nil {
				return err
			}
		}
		// Files of an automatic run that is not confirmed were never
		// written with the new details.
		if s.Hooks.WriteBackTx != nil && !(a.Automatic && a.RunState != StateConfirmed) {
			for _, b := range a.Books {
				if _, err := s.Hooks.WriteBackTx(ctx, tx, b); err != nil {
					return err
				}
			}
		}
		if err := q.SetCurationActionUndone(ctx, sqlc.SetCurationActionUndoneParams{ID: id, By: &scope.Viewer}); err != nil {
			return err
		}
		if err := q.SettleCurationRun(ctx, a.RunID); err != nil {
			return err
		}
		out.State = OutcomeUndone
		// Last, so that nothing after them can fail but the commit.
		for _, m := range moves {
			made = append(made, m)
			if err := os.MkdirAll(filepath.Dir(m.to), 0o755); err != nil {
				return err
			}
			if err := os.Rename(m.from, m.to); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		for _, m := range made {
			if _, statErr := os.Lstat(m.to); statErr == nil {
				if mvErr := os.Rename(m.to, m.from); mvErr != nil {
					slog.Error("curation: cannot move a file back after its undo failed", "from", m.to, "to", m.from, "error", mvErr)
				}
			}
		}
		if errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return out, err
		}
		slog.Warn("curation: undo failed", "action", id, "error", err)
		out.State, out.Message = OutcomeFailed, "It could not be undone: "+err.Error()
		return out, sqlc.New(s.pool).SetCurationActionNote(ctx, sqlc.SetCurationActionNoteParams{ID: id, Note: out.Message})
	}
	if out.State == OutcomeUndone {
		for _, m := range made {
			_ = os.Remove(filepath.Dir(m.from))
		}
	}
	return out, nil
}

// filesToRestore checks that the files an action trashed are still in the
// trash, and where they go back to.
func (s *Service) filesToRestore(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) ([]move, string, error) {
	files, err := q.ListFilesToRestore(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	if len(files) != len(ids) {
		return nil, "A file the action moved to the trash has been deleted for good.", nil
	}
	var moves []move
	for _, f := range files {
		if f.TrashedAt == nil {
			return nil, "A file the action moved to the trash was restored since.", nil
		}
		if !f.Writable {
			return nil, "The library is read-only.", nil
		}
		from := s.Hooks.TrashPath(f.RootPath, f.ID, f.RelPath)
		to := filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath))
		if _, err := os.Lstat(from); err != nil {
			return nil, "A file the action moved to the trash is no longer there.", nil
		}
		if _, err := os.Lstat(to); err == nil {
			return nil, "Another file is where a trashed file was.", nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", err
		}
		moves = append(moves, move{from: from, to: to})
	}
	return moves, "", nil
}

// RunOutcome is what undoing a run came to.
type RunOutcome struct {
	RunID   uuid.UUID `json:"runId"`
	Actions []Outcome `json:"actions"`
}

// undoBudget is how long one pass of an undo works before it hands the rest
// to the next, so that a run of hundreds of actions does not hold a worker.
const undoBudget = 20 * time.Second

// undoBatch is how many actions one look into the run's list takes.
const undoBatch = 50

// RequestUndo asks for the run the scope sees to be undone by its job, and
// answers at once. Asking again while it is being undone asks for nothing.
func (s *Service) RequestUndo(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.GetCurationRun(ctx, sqlc.GetCurationRunParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		n, err := q.RequestRunUndo(ctx, sqlc.RequestRunUndoParams{ID: id, Viewer: &scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		if n == 0 {
			flags, err := q.GetCurationRunFlags(ctx, id)
			switch {
			case err != nil:
				return err
			case flags.Undoing:
				return nil
			case flags.DryRun:
				return ErrDryRun
			default:
				return ErrRunning
			}
		}
		_, err = s.Queue.InsertTx(ctx, tx, UndoArgs{RunID: id}, jobs.InsertOpts{
			Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: undoAttempts,
		})
		return err
	})
}

// WorkUndo undoes the actions of a run that was asked to be undone, the last
// made first and each in a transaction of its own, for a while. It says what
// the pass undid and whether it got to the end. An action left because
// its books changed since is not tried again by the same request.
func (s *Service) WorkUndo(ctx context.Context, id uuid.UUID) (RunOutcome, bool, error) {
	out := RunOutcome{RunID: id, Actions: []Outcome{}}
	q := sqlc.New(s.pool)
	run, err := q.GetRunUndo(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && run.UndoRequestedAt == nil {
		return out, true, nil
	}
	if err != nil {
		return out, false, err
	}
	scope := library.Scope{SeesAll: run.UndoSeesAll}
	if run.UndoViewer != nil {
		scope.Viewer = *run.UndoViewer
	} else if !scope.SeesAll {
		return out, true, q.ClearRunUndo(ctx, id)
	}
	cursorAt, cursorID := run.UndoCursorAt, run.UndoCursorID
	started := time.Now()
	for time.Since(started) < undoBudget {
		ids, err := q.ListCurationActionIDsToUndo(ctx, sqlc.ListCurationActionIDsToUndoParams{
			RunID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll, BeforeAt: cursorAt, BeforeID: cursorID, MaxRows: undoBatch,
		})
		if err != nil {
			return out, false, err
		}
		if len(ids) == 0 {
			return out, true, q.ClearRunUndo(ctx, id)
		}
		for _, a := range ids {
			o, err := s.UndoAction(ctx, scope, a.ID)
			if err != nil {
				return out, false, err
			}
			out.Actions = append(out.Actions, o)
			at, aid := a.CreatedAt, a.ID
			cursorAt, cursorID = &at, &aid
			if err := q.AdvanceRunUndo(ctx, sqlc.AdvanceRunUndoParams{ID: id, UndoCursorAt: cursorAt, UndoCursorID: cursorID}); err != nil {
				return out, false, err
			}
		}
	}
	return out, false, nil
}

const (
	undoKind     = "curation.undo_run"
	undoAttempts = 5
)

// UndoArgs is the job that undoes a run.
type UndoArgs struct {
	RunID uuid.UUID `json:"runId"`
}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (UndoArgs) Kind() string { return undoKind }

// UndoWorker runs UndoArgs.
type UndoWorker struct {
	river.WorkerDefaults[UndoArgs]
	Service *Service
}

func (w *UndoWorker) Work(ctx context.Context, job *river.Job[UndoArgs]) error {
	_, done, err := w.Service.WorkUndo(ctx, job.Args.RunID)
	if err != nil {
		return err
	}
	if !done {
		return river.JobSnooze(time.Second)
	}
	return nil
}
