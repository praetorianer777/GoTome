package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
)

type figuresQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
}

// statsLibrary is the library a statistics route was asked about, nil for
// every one the caller sees; one they may not see is not found.
func (s *Server) statsLibrary(r *http.Request) (*uuid.UUID, error) {
	var q figuresQuery
	if err := decodeQuery(r, &q); err != nil {
		return nil, err
	}
	if q.Library == "" {
		return nil, nil
	}
	id, err := uuid.Parse(q.Library)
	if err != nil {
		return nil, ErrNotFound("There is no such library.")
	}
	if _, err := s.Libraries.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return nil, libraryError(err)
	}
	return &id, nil
}

func (s *Server) getLibraryFigures(w http.ResponseWriter, r *http.Request) error {
	libraryID, err := s.statsLibrary(r)
	if err != nil {
		return err
	}
	figures, err := s.Stats.Figures(r.Context(), library.ScopeOf(*UserFrom(r.Context())), libraryID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, figures)
	return nil
}

func (s *Server) getWorkProgress(w http.ResponseWriter, r *http.Request) error {
	libraryID, err := s.statsLibrary(r)
	if err != nil {
		return err
	}
	work, err := s.Stats.Work(r.Context(), library.ScopeOf(*UserFrom(r.Context())), libraryID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, work)
	return nil
}
