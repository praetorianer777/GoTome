package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/similar"
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

type describedQuery struct {
	Q       string `query:"q" doc:"What the books are to be about, in a few words or sentences, in any language the model reads."`
	Library string `query:"library" doc:"A library's ID: only its books. Left out, every library the caller may see."`
	Offset  int    `query:"offset" doc:"How many of the best books to pass over, as the page before's nextOffset says."`
	Limit   int    `query:"limit" doc:"How many books a page holds, at most 100; 24 when left out."`
}

// describedList is the books nearest a description, the best first.
type describedList struct {
	Books []bookSummary `json:"books"`
	// NextOffset asks for the books that follow; left out when none do.
	NextOffset *int `json:"nextOffset,omitempty"`
}

const defaultDescribed = 24

func (s *Server) searchDescribed(w http.ResponseWriter, r *http.Request) error {
	var q describedQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	if strings.TrimSpace(q.Q) == "" {
		return ErrValidation(map[string]string{"q": "Say what the books are to be about."})
	}
	var libraryID *uuid.UUID
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		}
		libraryID = &id
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	limit := cmp.Or(q.Limit, defaultDescribed)
	ids, more, err := s.Similar.Describe(r.Context(), scope, libraryID, q.Q, q.Offset, limit)
	if errors.Is(err, similar.ErrUnavailable) {
		return ErrUnavailable("Searching by description needs the embedding model, which cannot run here yet.")
	}
	if err != nil {
		return err
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	out := describedList{Books: make([]bookSummary, len(books))}
	for i, b := range books {
		out.Books[i] = summaryOf(b)
	}
	if more {
		next := max(q.Offset, 0) + min(max(limit, 1), similar.MaxDescribed)
		out.NextOffset = &next
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) embeddingStatus(w http.ResponseWriter, r *http.Request) error {
	status, err := s.Similar.Status(r.Context(), library.ScopeOf(*UserFrom(r.Context())))
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, status)
	return nil
}
