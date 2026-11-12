package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// placeholderJoinRequest names the library's book that the placeholder in
// the path is.
type placeholderJoinRequest struct {
	Book uuid.UUID `json:"book"`
}

// placeholderJoin says what a join came to.
type placeholderJoin struct {
	// Book is the library's book, where the placeholder's reading lives now.
	Book uuid.UUID `json:"book"`
	// ActionID is what undoes the join: POST /placeholder-joins/{actionId}/undo.
	ActionID uuid.UUID `json:"actionId"`
}

func (s *Server) joinPlaceholder(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req placeholderJoinRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	action, err := s.Scans.JoinPlaceholder(r.Context(), scope, id, req.Book)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrNotFound("There is no such book.")
	case errors.Is(err, ingest.ErrMergeSelf), errors.Is(err, ingest.ErrNotPlaceholder):
		return ErrValidation(map[string]string{"book": "Name a book of the library, and a book not owned in the path."})
	case errors.Is(err, ingest.ErrPlaceholderHolders):
		return ErrConflict("Someone else reads this book and cannot see that one, so the two stay apart.")
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, placeholderJoin{Book: req.Book, ActionID: action})
	return nil
}

func (s *Server) undoPlaceholderJoin(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "actionId", "join")
	if err != nil {
		return err
	}
	out, err := s.Scans.UndoPlaceholderJoin(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, curation.ErrNotFound) {
		return ErrNotFound("There is no such join.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, outcomeOf(out))
	return nil
}
