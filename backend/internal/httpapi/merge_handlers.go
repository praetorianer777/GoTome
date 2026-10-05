package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// mergeRequest names the book merged into the one in the path, and the
// fields to take from it over the surviving book's own values.
type mergeRequest struct {
	From uuid.UUID `json:"from"`
	// Take names fields of title, subtitle, description, language,
	// published, publisher, series, pageCount, contributors, tags and cover.
	// Fields the surviving book has no value for are taken without asking.
	Take []string `json:"take"`
}

func (s *Server) mergeBooks(w http.ResponseWriter, r *http.Request) error {
	into, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req mergeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	err = s.Scans.Merge(r.Context(), scope, into, req.From, req.Take)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrNotFound("There is no such book.")
	case errors.Is(err, ingest.ErrMergeSelf):
		return ErrValidation(map[string]string{"from": "Name another book than this one."})
	case errors.Is(err, ingest.ErrMergeLibraries):
		return ErrValidation(map[string]string{"from": "Only books of one library are merged: a file stays in its library's folder."})
	case errors.Is(err, ingest.ErrMergeField):
		return ErrValidation(map[string]string{"take": "Use some of: " + strings.Join(ingest.MergeFields, ", ") + "."})
	case err != nil:
		return err
	}
	b, err := s.Books.Get(r.Context(), scope, into)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, detailOf(b, user))
	return nil
}
