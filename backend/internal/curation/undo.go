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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
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

// ErrRunning is a run that still makes actions.
var ErrRunning = errors.New("the run is still going")

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

// UndoRun undoes each action of the run that the scope sees, the last made
// first, each in a transaction of its own.
func (s *Service) UndoRun(ctx context.Context, scope library.Scope, id uuid.UUID) (RunOutcome, error) {
	q := sqlc.New(s.pool)
	run, err := q.GetCurationRun(ctx, sqlc.GetCurationRunParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return RunOutcome{}, ErrNotFound
	}
	if err != nil {
		return RunOutcome{}, err
	}
	if run.State == StateRunning {
		return RunOutcome{}, ErrRunning
	}
	ids, err := q.ListCurationActionIDsToUndo(ctx, sqlc.ListCurationActionIDsToUndoParams{RunID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return RunOutcome{}, err
	}
	out := RunOutcome{RunID: id, Actions: []Outcome{}}
	for _, aid := range ids {
		o, err := s.UndoAction(ctx, scope, aid)
		if err != nil {
			return out, err
		}
		out.Actions = append(out.Actions, o)
	}
	return out, nil
}
