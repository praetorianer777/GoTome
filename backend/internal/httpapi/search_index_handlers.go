package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// textRereadRequest names what to read again: a library, a book, or with
// neither every book the caller sees.
type textRereadRequest struct {
	Library *uuid.UUID `json:"library,omitempty"`
	Book    *uuid.UUID `json:"book,omitempty"`
}

type textRereadResult struct {
	// Files is how many files' text was queued to be read again.
	Files int `json:"files"`
}

type rebuildResult struct {
	// Queued is false when a rebuild was already waiting or running.
	Queued bool `json:"queued"`
}

type searchStatusQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
}

func (s *Server) rereadText(w http.ResponseWriter, r *http.Request) error {
	var req textRereadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	if req.Library != nil {
		if _, err := s.Libraries.Get(r.Context(), scope, *req.Library); err != nil {
			return libraryError(err)
		}
	}
	if req.Book != nil {
		if _, err := s.Books.Get(r.Context(), scope, *req.Book); errors.Is(err, catalog.ErrNotFound) {
			return ErrNotFound("There is no such book.")
		} else if err != nil {
			return err
		}
	}
	n, err := s.Scans.Rechunk(r.Context(), scope, req.Library, req.Book)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, textRereadResult{Files: n})
	return nil
}

func (s *Server) rebuildSearchIndex(w http.ResponseWriter, r *http.Request) error {
	queued, err := s.Index.RequestRebuild(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, rebuildResult{Queued: queued})
	return nil
}

func (s *Server) searchStatus(w http.ResponseWriter, r *http.Request) error {
	var q searchStatusQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	var libraryID *uuid.UUID
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrNotFound("There is no such library.")
		}
		if _, err := s.Libraries.Get(r.Context(), scope, id); err != nil {
			return libraryError(err)
		}
		libraryID = &id
	}
	status, err := s.Index.Status(r.Context(), scope, libraryID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, status)
	return nil
}
