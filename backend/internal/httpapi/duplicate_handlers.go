package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type duplicatesQuery struct {
	State   string `query:"state" enum:"open,kept_both,merged,replaced" doc:"Which pairs: open (the default) are those nobody has decided about."`
	Library string `query:"library" doc:"A library's ID: the pairs with a book in it. Left out, every library the caller may see."`
	Before  string `query:"before" doc:"The ID of the last pair of the page before; pairs come newest first."`
	Limit   int    `query:"limit" doc:"How many pairs a page holds, at most 100; 50 when left out."`
}

type duplicateEvidence struct {
	Kind string `json:"kind"`
	// Detail is what the books share: the hash, the ISBN, or the title and
	// author as compared.
	Detail string `json:"detail"`
}

// duplicatePair is two books that look like one, both visible to the
// caller, and why.
type duplicatePair struct {
	ID       uuid.UUID           `json:"id"`
	State    string              `json:"state"`
	FoundAt  time.Time           `json:"foundAt"`
	Books    []bookSummary       `json:"books"`
	Evidence []duplicateEvidence `json:"evidence"`
}

type duplicateList struct {
	Pairs []duplicatePair `json:"pairs"`
	// More says pairs follow; ask with before set to the last one's ID.
	More bool `json:"more"`
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
	if q.Before != "" {
		id, err := uuid.Parse(q.Before)
		if err != nil {
			return ErrValidation(map[string]string{"before": "Give a pair's ID."})
		}
		lq.Before = &id
	}
	pairs, more, err := s.Duplicates.List(r.Context(), library.ScopeOf(*UserFrom(r.Context())), lq)
	if err != nil {
		return err
	}
	out := duplicateList{Pairs: make([]duplicatePair, len(pairs)), More: more}
	for i, p := range pairs {
		dp := duplicatePair{ID: p.ID, State: p.State, FoundAt: p.FoundAt, Evidence: []duplicateEvidence{}}
		for _, b := range p.Books {
			dp.Books = append(dp.Books, summaryOf(b))
		}
		for _, e := range p.Evidence {
			dp.Evidence = append(dp.Evidence, duplicateEvidence{Kind: e.Kind, Detail: e.Detail})
		}
		out.Pairs[i] = dp
	}
	writeJSON(w, r, http.StatusOK, out)
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
