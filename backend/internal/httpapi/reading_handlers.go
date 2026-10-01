package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// readingState is where one person stands with a book. Nobody else sees it,
// and it is not what a metadata provider's readers think of the book.
type readingState struct {
	// Status is unread, reading, completed, abandoned or wishlist.
	Status string `json:"status"`
	// Rating is 1 to 5 stars; left out when none was given.
	Rating *int16 `json:"rating,omitempty"`
	// StartedOn and FinishedOn are dates as 2006-01-02.
	StartedOn  string `json:"startedOn,omitempty"`
	FinishedOn string `json:"finishedOn,omitempty"`
}

func readingOf(r catalog.Reading) readingState {
	out := readingState{Status: r.Status, Rating: r.Rating}
	if r.StartedOn != nil {
		out.StartedOn = r.StartedOn.Format(time.DateOnly)
	}
	if r.FinishedOn != nil {
		out.FinishedOn = r.FinishedOn.Format(time.DateOnly)
	}
	return out
}

// readingChange changes where the caller stands with a book. A field left
// out stays as it is.
type readingChange struct {
	// Status set to reading also sets startedOn, and set to completed
	// finishedOn, to today where they are not known.
	Status *string `json:"status,omitempty"`
	// Rating 0 takes the rating away.
	Rating *int16 `json:"rating,omitempty"`
	// StartedOn and FinishedOn are dates as 2006-01-02; "" removes one.
	StartedOn  *string `json:"startedOn,omitempty"`
	FinishedOn *string `json:"finishedOn,omitempty"`
}

func (c readingChange) change() catalog.ReadingChange {
	return catalog.ReadingChange{Status: c.Status, Rating: c.Rating, StartedOn: c.StartedOn, FinishedOn: c.FinishedOn}
}

func (s *Server) setReading(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req readingChange
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	n, err := s.Books.SetReading(r.Context(), scope, []uuid.UUID{id}, req.change())
	var invalid catalog.EditError
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	case n == 0:
		return ErrNotFound("There is no such book.")
	}
	b, err := s.Books.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, readingOf(b.Reading))
	return nil
}

// readingBulkRequest changes where the caller stands with many books: those
// named, or, when none are, every book of the list library and filter give.
type readingBulkRequest struct {
	readingChange
	Books   []uuid.UUID `json:"books,omitempty"`
	Library string      `json:"library,omitempty"`
	Filter  string      `json:"filter,omitempty"`
}

type readingBulkResult struct {
	// Changed is how many books the change was made to.
	Changed int `json:"changed"`
}

func (s *Server) setReadingBulk(w http.ResponseWriter, r *http.Request) error {
	var req readingBulkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	libraryID, tree, err := bookSelection(req.Library, req.Filter)
	if err != nil {
		return err
	}
	tooMany := ErrValidation(map[string]string{"books": fmt.Sprintf("A change takes at most %d books; choose fewer.", catalog.MaxSelection)})
	if len(req.Books) > catalog.MaxSelection {
		return tooMany
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	books, err := s.Books.Select(r.Context(), scope, libraryID, tree, req.Books)
	switch {
	case errors.Is(err, catalog.ErrTooMany):
		return tooMany
	case catalog.IsFilterError(err):
		return ErrValidation(map[string]string{"filter": err.Error()})
	case err != nil:
		return err
	}
	n, err := s.Books.SetReading(r.Context(), scope, books, req.change())
	var invalid catalog.EditError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid)
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, readingBulkResult{Changed: n})
	return nil
}
