package httpapi

import (
	"cmp"
	"errors"
	"net/http"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type similarQuery struct {
	Limit int `query:"limit" doc:"How many books to return, at most 50; 12 when left out."`
}

// similarList is the books most like one, the nearest first. It is empty
// until the books are embedded.
type similarList struct {
	Books []bookSummary `json:"books"`
}

const defaultSimilar = 12

func (s *Server) listSimilar(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var q similarQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	// Through Get, so that a book the caller may not see is not found, and
	// a merged one answers for the book it went into.
	b, err := s.Books.Get(r.Context(), scope, id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	ids, err := s.Similar.Similar(r.Context(), scope, b.ID, cmp.Or(q.Limit, defaultSimilar))
	if err != nil {
		return err
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	out := similarList{Books: make([]bookSummary, len(books))}
	for i, b := range books {
		out.Books[i] = summaryOf(b)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
