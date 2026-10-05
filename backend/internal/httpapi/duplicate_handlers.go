package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type duplicatesQuery struct {
	State   string `query:"state" enum:"open,kept_both,merged,replaced" doc:"Which pairs: open (the default) are those nobody has decided about."`
	Library string `query:"library" doc:"A library's ID: the pairs with a book in it. Left out, every library the caller may see."`
	Kind    string `query:"kind" enum:"sha256,content,isbn,title_author,overlap" doc:"Only pairs with evidence of this kind."`
	Least   int    `query:"least" doc:"Only pairs whose score is at least this many percent."`
	Cursor  string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; pairs come strongest first."`
	Limit   int    `query:"limit" doc:"How many pairs a page holds, at most 100; 50 when left out."`
}

type duplicateEvidence struct {
	Kind string `json:"kind"`
	// Detail is what the books share: the hash, the ISBN, the title and
	// author as compared, or for overlap "jaccard=… a_in_b=… b_in_a=…",
	// from the pair's first book.
	Detail string `json:"detail"`
	// Score is how sure this evidence alone makes it, from 0 to 1.
	Score float32 `json:"score"`
}

// duplicatePair is two books that look like one, both visible to the
// caller, and why.
type duplicatePair struct {
	ID    uuid.UUID `json:"id"`
	State string    `json:"state"`
	// Score is how sure the strongest evidence makes it that the books are
	// one, from 0 to 1.
	Score    float32             `json:"score"`
	FoundAt  time.Time           `json:"foundAt"`
	Books    []bookSummary       `json:"books"`
	Evidence []duplicateEvidence `json:"evidence"`
}

type duplicateList struct {
	Pairs []duplicatePair `json:"pairs"`
	// NextCursor asks for the pairs that follow; left out when none do.
	NextCursor string `json:"nextCursor,omitempty"`
}

type pairStateRequest struct {
	// State is kept_both, that both books stay as they are, or open.
	State string `json:"state"`
}

type duplicateCheckRequest struct {
	// Library names the library whose books are checked; left out, every
	// library the caller sees.
	Library *uuid.UUID `json:"library,omitempty"`
}

type duplicateCheckResult struct {
	// Books is how many books were queued to be checked.
	Books int `json:"books"`
}

const defaultDuplicatePage = 50

func (s *Server) listDuplicates(w http.ResponseWriter, r *http.Request) error {
	var q duplicatesQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	lq := dedupe.ListQuery{State: dedupe.StateOpen, Limit: defaultDuplicatePage}
	if q.State != "" {
		lq.State = q.State
	}
	if q.Limit > 0 {
		lq.Limit = q.Limit
	}
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		}
		lq.LibraryID = &id
	}
	if q.Kind != "" {
		lq.Kind = &q.Kind
	}
	lq.Least = float32(q.Least) / 100
	if q.Cursor != "" {
		score, id, _ := strings.Cut(q.Cursor, "_")
		f, scoreErr := strconv.ParseFloat(score, 32)
		pairID, idErr := uuid.Parse(id)
		if scoreErr != nil || idErr != nil {
			return ErrValidation(map[string]string{"cursor": "Give the nextCursor of the page before."})
		}
		lq.After = &dedupe.Cursor{Score: float32(f), ID: pairID}
	}
	pairs, more, err := s.Duplicates.List(r.Context(), library.ScopeOf(*UserFrom(r.Context())), lq)
	if err != nil {
		return err
	}
	out := duplicateList{Pairs: make([]duplicatePair, len(pairs))}
	if more && len(pairs) > 0 {
		last := pairs[len(pairs)-1]
		out.NextCursor = strconv.FormatFloat(float64(last.Score), 'g', -1, 32) + "_" + last.ID.String()
	}
	for i, p := range pairs {
		dp := duplicatePair{ID: p.ID, State: p.State, Score: p.Score, FoundAt: p.FoundAt, Evidence: []duplicateEvidence{}}
		for _, b := range p.Books {
			dp.Books = append(dp.Books, summaryOf(b))
		}
		for _, e := range p.Evidence {
			dp.Evidence = append(dp.Evidence, duplicateEvidence{Kind: e.Kind, Detail: e.Detail, Score: e.Score})
		}
		out.Pairs[i] = dp
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) setPairState(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "pairId", "pair")
	if err != nil {
		return err
	}
	var req pairStateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err = s.Duplicates.SetState(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.State)
	switch {
	case errors.Is(err, dedupe.ErrNotFound):
		return ErrNotFound("There is no such pair.")
	case errors.Is(err, dedupe.ErrState):
		return ErrValidation(map[string]string{"state": "Use kept_both or open."})
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) checkDuplicates(w http.ResponseWriter, r *http.Request) error {
	var req duplicateCheckRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	if req.Library != nil {
		if _, err := s.Libraries.Get(r.Context(), scope, *req.Library); err != nil {
			return libraryError(err)
		}
	}
	n, err := s.Duplicates.Request(r.Context(), scope, req.Library)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, duplicateCheckResult{Books: n})
	return nil
}
