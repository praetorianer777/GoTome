package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

// playQueue is the audiobooks a person has lined up, in the order they
// play.
type playQueue struct {
	Books []uuid.UUID `json:"books"`
}

func (s *Server) getPlayQueue(w http.ResponseWriter, r *http.Request) error {
	books, err := s.Shelves.Queue(r.Context(), library.ScopeOf(*UserFrom(r.Context())))
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, playQueue{Books: books})
	return nil
}

func (s *Server) putPlayQueue(w http.ResponseWriter, r *http.Request) error {
	var req playQueue
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	books, err := s.Shelves.SetQueue(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Books)
	if errors.Is(err, shelves.ErrQueueFull) {
		return ErrValidation(map[string]string{"books": fmt.Sprintf("A queue holds at most %d books.", shelves.MaxQueue)})
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, playQueue{Books: books})
	return nil
}
