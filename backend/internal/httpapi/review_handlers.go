package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

type reviewQuery struct {
	Limit int `query:"limit" doc:"How many books to return, at most 50; 20 when left out."`
}

// reviewList is the books whose matches wait for a person, those that wait
// longest first.
type reviewList struct {
	Books []reviewBook `json:"books"`
	// Total is how many books wait in all.
	Total int `json:"total"`
}

type reviewBook struct {
	Book bookDetail `json:"book"`
	// Matches are the candidates found for it, the best first.
	Matches []matchView `json:"matches"`
}

type matchView struct {
	// MatchID names the match to accept or reject it.
	MatchID uuid.UUID `json:"matchId"`
	candidateView
}

const defaultReview = 20

func (s *Server) listReview(w http.ResponseWriter, r *http.Request) error {
	var q reviewQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	waiting, total, err := s.Matches.Review(r.Context(), scope, min(cmp.Or(q.Limit, defaultReview), 50))
	if err != nil {
		return err
	}
	out := reviewList{Books: []reviewBook{}, Total: total}
	for _, wb := range waiting {
		b, err := s.Books.Get(r.Context(), scope, wb.BookID)
		if errors.Is(err, catalog.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		item := reviewBook{Book: detailOf(b, user), Matches: []matchView{}}
		for _, m := range wb.Matches {
			item.Matches = append(item.Matches, matchView{
				MatchID:       m.ID,
				candidateView: s.candidateViewOf(metadata.Candidate{Record: m.Record, Score: m.Score}),
			})
		}
		out.Books = append(out.Books, item)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) pendingMatch(r *http.Request) (enrich.Match, error) {
	id, err := pathID(r, "matchId", "match")
	if err != nil {
		return enrich.Match{}, err
	}
	m, err := s.Matches.Pending(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, enrich.ErrNoMatch) {
		return enrich.Match{}, ErrNotFound("There is no such match waiting.")
	}
	return m, err
}

// acceptMatchRequest takes chosen values from a match, as applying a
// candidate does.
type acceptMatchRequest struct {
	editBookRequest
	// CoverToken is the match's, to take its cover.
	CoverToken string `json:"coverToken,omitempty"`
}

func (s *Server) acceptMatch(w http.ResponseWriter, r *http.Request) error {
	m, err := s.pendingMatch(r)
	if err != nil {
		return err
	}
	var req acceptMatchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	return s.takeFromProvider(w, r, m.BookID, m.Provider, req.editBookRequest, req.CoverToken,
		func(ctx context.Context) error { return s.Matches.Accepted(ctx, m) })
}

func (s *Server) rejectMatch(w http.ResponseWriter, r *http.Request) error {
	m, err := s.pendingMatch(r)
	if err != nil {
		return err
	}
	if err := s.Matches.Reject(r.Context(), library.ScopeOf(*UserFrom(r.Context())), m.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
