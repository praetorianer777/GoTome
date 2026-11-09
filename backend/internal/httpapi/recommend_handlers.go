package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type recommendationsQuery struct {
	Limit int `query:"limit" doc:"How many books to return, at most 50; 12 when left out."`
}

type recommendationBecause struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

type recommendation struct {
	Book bookSummary `json:"book"`
	// Because are books the caller liked that this one is near.
	Because []recommendationBecause `json:"because"`
}

// recommendationList is what the caller may like among the books they have
// not read.
type recommendationList struct {
	// HasProfile is false while the caller has finished or rated fewer than
	// three books; there is then nothing to recommend from.
	HasProfile bool             `json:"hasProfile"`
	Books      []recommendation `json:"books"`
}

// listRecommendations recommends unread books of the libraries the caller
// sees from what they liked. It is the caller's own: nobody else's taste is
// in it.
func (s *Server) listRecommendations(w http.ResponseWriter, r *http.Request) error {
	var q recommendationsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 12
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	recs, ok, err := s.Similar.Recommend(r.Context(), scope, limit)
	if err != nil {
		return err
	}
	out := recommendationList{HasProfile: ok, Books: []recommendation{}}
	if len(recs) > 0 {
		ids := make([]uuid.UUID, len(recs))
		for i, rec := range recs {
			ids[i] = rec.BookID
		}
		books, err := s.Books.Summaries(r.Context(), scope, ids)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]catalog.Summary, len(books))
		for _, b := range books {
			byID[b.ID] = b
		}
		for _, rec := range recs {
			b, ok := byID[rec.BookID]
			if !ok {
				continue
			}
			item := recommendation{Book: summaryOf(b), Because: []recommendationBecause{}}
			for _, bc := range rec.Because {
				item.Because = append(item.Because, recommendationBecause{ID: bc.ID, Title: bc.Title})
			}
			out.Books = append(out.Books, item)
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) dismissRecommendation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	err = s.Similar.Dismiss(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
