package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/organise"
)

type organiseRequest struct {
	// Pattern names the files; empty is the library's own pattern.
	Pattern string `json:"pattern,omitempty"`
}

type organiseMoveItem struct {
	BookID uuid.UUID `json:"bookId"`
	Title  string    `json:"title"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	// Collision: another file has the plain name, so this one is numbered.
	Collision bool `json:"collision"`
}

type organiseSkipItem struct {
	BookID uuid.UUID `json:"bookId"`
	Title  string    `json:"title"`
	Path   string    `json:"path"`
	Reason string    `json:"reason" enum:"missing,too-long"`
}

type organisePreview struct {
	// Pattern is the pattern the preview was made with.
	Pattern        string `json:"pattern"`
	DefaultPattern string `json:"defaultPattern"`
	// Allowed says whether a run may be started; Blocked says why not.
	Allowed bool   `json:"allowed"`
	Blocked string `json:"blocked,omitempty" enum:"read-only,off"`
	// Files and Books are what the pattern was applied to.
	Files      int `json:"files"`
	Books      int `json:"books"`
	Unchanged  int `json:"unchanged"`
	Moves      int `json:"moves"`
	Collisions int `json:"collisions"`
	Skipped    int `json:"skipped"`
	// Examples are the first moves, Left the skipped files, each a part of
	// the whole.
	Examples []organiseMoveItem `json:"examples"`
	Left     []organiseSkipItem `json:"left"`
}

type organiseStarted struct {
	// RunID is the run on the Changes page, GET /curation/runs/{id}.
	RunID uuid.UUID `json:"runId"`
}

const (
	organiseExamples = 100
	organiseLeft     = 50
)

func organiseError(err error) error {
	var bad *ingest.PatternError
	switch {
	case errors.As(err, &bad):
		return ErrValidation(map[string]string{"pattern": bad.Reason + "."})
	case errors.Is(err, library.ErrNotFound):
		return ErrNotFound("There is no such library.")
	case errors.Is(err, ingest.ErrOrganiseOff):
		return ErrConflict("This library's files may not be renamed: it is read-only, or an external library that has not been opted in.")
	case errors.Is(err, ingest.ErrOrganiseBusy):
		return ErrConflict("The library's files are being organised already.")
	}
	return err
}

func (s *Server) previewOrganise(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	var req organiseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	plan, l, err := s.Scans.PlanOrganise(r.Context(), id, req.Pattern)
	if err != nil {
		return organiseError(err)
	}
	blocked := ingest.OrganiseAllowed(l)
	out := organisePreview{
		Pattern: plan.Pattern, DefaultPattern: organise.DefaultPattern, Allowed: blocked == "", Blocked: blocked,
		Files: plan.Files, Books: plan.Books, Unchanged: plan.Unchanged, Moves: len(plan.Moves),
		Collisions: plan.Collisions, Skipped: len(plan.Skipped),
		Examples: []organiseMoveItem{}, Left: []organiseSkipItem{},
	}
	for _, m := range plan.Moves[:min(len(plan.Moves), organiseExamples)] {
		out.Examples = append(out.Examples, organiseMoveItem{BookID: m.BookID, Title: m.Title, From: m.From, To: m.To, Collision: m.Collision})
	}
	for _, k := range plan.Skipped[:min(len(plan.Skipped), organiseLeft)] {
		out.Left = append(out.Left, organiseSkipItem{BookID: k.BookID, Title: k.Title, Path: k.Path, Reason: k.Reason})
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) startOrganise(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	var req organiseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	run, err := s.Scans.RequestOrganise(r.Context(), UserFrom(r.Context()).ID, id, req.Pattern)
	if err != nil {
		return organiseError(err)
	}
	writeJSON(w, r, http.StatusAccepted, organiseStarted{RunID: run})
	return nil
}
