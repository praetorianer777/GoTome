package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type curationRunsQuery struct {
	Kind   string `query:"kind" enum:"duplicates,cleanup,matches" doc:"Only runs of this kind."`
	State  string `query:"state" enum:"planned,running,done,confirmed,undone" doc:"Only runs in this state."`
	Cursor string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; runs come newest first."`
	Limit  int    `query:"limit" doc:"How many runs a page holds, at most 200; 50 when left out."`
}

type curationActionsQuery struct {
	Cursor string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; actions come in the order they were made."`
	Limit  int    `query:"limit" doc:"How many actions a page holds, at most 200; 50 when left out."`
}

// curationRun is a run of changes to books, with the counts of the actions
// in libraries the caller sees.
type curationRun struct {
	ID   uuid.UUID `json:"id"`
	Kind string    `json:"kind"`
	// Rule is the rule of an automatic run; left out for a person's own.
	Rule string `json:"rule,omitempty"`
	// StartedBy is the user who made it, if that account is still there.
	StartedBy *uuid.UUID `json:"startedBy,omitempty"`
	// StartedByName is that user's name.
	StartedByName string `json:"startedByName,omitempty"`
	DryRun        bool   `json:"dryRun"`
	// Automatic runs keep the files they trash until they are confirmed
	// and write nothing into files until then.
	Automatic   bool       `json:"automatic"`
	State       string     `json:"state" enum:"planned,running,done,confirmed,undone"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	ConfirmedAt *time.Time `json:"confirmedAt,omitempty"`
	UndoneAt    *time.Time `json:"undoneAt,omitempty"`
	// Actions is how many actions the caller sees, and Undone how many of
	// them are undone.
	Actions int `json:"actions"`
	Undone  int `json:"undone"`
	// Skipped is how many pairs a duplicates run left for a person.
	Skipped int `json:"skipped"`
	// Undoing is set while the run's job undoes it; Undone then counts how
	// far it has got.
	Undoing bool `json:"undoing"`
}

type curationRunList struct {
	Runs       []curationRun `json:"runs"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type curationFieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type curationBook struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Changes are the details that differ before and after.
	Changes []curationFieldChange `json:"changes"`
}

type curationAction struct {
	ID    uuid.UUID `json:"id"`
	RunID uuid.UUID `json:"runId"`
	Kind  string    `json:"kind" enum:"merge,replace,edit,link,skip"`
	// Rule and Reason say why an automatic rule did it.
	Rule   string         `json:"rule,omitempty"`
	Reason string         `json:"reason,omitempty"`
	Books  []curationBook `json:"books"`
	// TrashedFiles is how many files it moved to the trash.
	TrashedFiles int    `json:"trashedFiles"`
	State        string `json:"state" enum:"done,undone"`
	// Note says why the last try to undo it did not.
	Note      string     `json:"note,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UndoneAt  *time.Time `json:"undoneAt,omitempty"`
}

type curationRunDetail struct {
	Run        curationRun      `json:"run"`
	Actions    []curationAction `json:"actions"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type curationConflict struct {
	Table string `json:"table"`
	Rows  int    `json:"rows"`
}

type curationOutcome struct {
	ActionID uuid.UUID `json:"actionId"`
	// State is undone, alreadyUndone, conflict (its books changed since),
	// notUndoable (its trashed files are gone) or failed.
	State     string             `json:"state" enum:"undone,alreadyUndone,conflict,notUndoable,failed"`
	Message   string             `json:"message,omitempty"`
	Conflicts []curationConflict `json:"conflicts,omitempty"`
}

type curationUndoQueued struct {
	RunID uuid.UUID `json:"runId"`
}

func runOf(r curation.Run) curationRun {
	return curationRun{
		ID: r.ID, Kind: r.Kind, Rule: r.Rule, StartedBy: r.StartedBy, StartedByName: r.StartedByName,
		DryRun: r.DryRun, Automatic: r.Automatic, State: r.State, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		ConfirmedAt: r.ConfirmedAt, UndoneAt: r.UndoneAt, Actions: r.Actions, Undone: r.Undone, Skipped: r.Skipped, Undoing: r.Undoing,
	}
}

func outcomeOf(o curation.Outcome) curationOutcome {
	out := curationOutcome{ActionID: o.ActionID, State: o.State, Message: o.Message}
	for _, c := range o.Conflicts {
		out.Conflicts = append(out.Conflicts, curationConflict{Table: c.Table, Rows: c.Rows})
	}
	return out
}

func curationLimit(n int) int {
	if n <= 0 {
		return curation.DefaultPage
	}
	return min(n, curation.MaxPage)
}

func (s *Server) listCurationRuns(w http.ResponseWriter, r *http.Request) error {
	var q curationRunsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	runs, next, err := s.Curation.Runs(r.Context(), scope, curation.RunFilter{Kind: q.Kind, State: q.State}, curationLimit(q.Limit), q.Cursor)
	if errors.Is(err, curation.ErrCursor) {
		return ErrValidation(map[string]string{"cursor": "Use a cursor from an earlier answer."})
	}
	if err != nil {
		return err
	}
	out := curationRunList{Runs: make([]curationRun, len(runs)), NextCursor: next}
	for i, run := range runs {
		out.Runs[i] = runOf(run)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) getCurationRun(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "runId", "run")
	if err != nil {
		return err
	}
	var q curationActionsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	run, actions, next, err := s.Curation.GetRun(r.Context(), scope, id, curationLimit(q.Limit), q.Cursor)
	switch {
	case errors.Is(err, curation.ErrNotFound):
		return ErrNotFound("There is no such run.")
	case errors.Is(err, curation.ErrCursor):
		return ErrValidation(map[string]string{"cursor": "Use a cursor from an earlier answer."})
	case err != nil:
		return err
	}
	out := curationRunDetail{Run: runOf(run), Actions: make([]curationAction, len(actions)), NextCursor: next}
	for i, a := range actions {
		books := make([]curationBook, len(a.Books))
		for j, b := range a.Books {
			changes := make([]curationFieldChange, len(b.Fields))
			for k, f := range b.Fields {
				changes[k] = curationFieldChange{Field: f.Field, Before: f.Before, After: f.After}
			}
			books[j] = curationBook{ID: b.ID, Title: b.Title, Changes: changes}
		}
		out.Actions[i] = curationAction{
			ID: a.ID, RunID: a.RunID, Kind: a.Kind, Rule: a.Rule, Reason: a.Reason, Books: books,
			TrashedFiles: a.TrashedFiles, State: a.State, Note: a.Note, CreatedAt: a.CreatedAt, UndoneAt: a.UndoneAt,
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) undoCurationRun(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "runId", "run")
	if err != nil {
		return err
	}
	err = s.Curation.RequestUndo(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	switch {
	case errors.Is(err, curation.ErrNotFound):
		return ErrNotFound("There is no such run.")
	case errors.Is(err, curation.ErrRunning):
		return ErrConflict("The run is still going.")
	case errors.Is(err, curation.ErrDryRun):
		return ErrConflict("A dry run changed nothing, so there is nothing to undo.")
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusAccepted, curationUndoQueued{RunID: id})
	return nil
}

func (s *Server) undoCurationAction(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "actionId", "action")
	if err != nil {
		return err
	}
	out, err := s.Curation.UndoAction(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, curation.ErrNotFound) {
		return ErrNotFound("There is no such action.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, outcomeOf(out))
	return nil
}

func (s *Server) confirmCurationRun(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "runId", "run")
	if err != nil {
		return err
	}
	err = s.Curation.Confirm(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	switch {
	case errors.Is(err, curation.ErrNotFound):
		return ErrNotFound("There is no such run.")
	case errors.Is(err, curation.ErrNotConfirmable):
		return ErrConflict("The run is not waiting for a confirmation.")
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
