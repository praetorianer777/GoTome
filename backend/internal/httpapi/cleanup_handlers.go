package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type cleanupQuery struct {
	Kind   string `query:"kind" enum:"authors,placeholders,clashes,titles" doc:"Which suggestions: authors (one person under several spellings, the default), placeholders (a value a tool left in place of an author), clashes (an author that is the name of the books' series) or titles (what a shop added to a title)."`
	Cursor string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; suggestions about the most books come first."`
	Limit  int    `query:"limit" doc:"How many suggestions a page holds, at most 200; 50 when left out."`
}

type cleanupName struct {
	Name string `json:"name"`
	// Books is how many books the caller sees carry it.
	Books int `json:"books"`
}

// cleanupSuggestion is one thing found about the books the caller sees, and
// what applying it does.
type cleanupSuggestion struct {
	Kind string `json:"kind"`
	// Subject names the suggestion to apply or dismiss.
	Subject string `json:"subject"`
	// Found are the names found: an author's spellings, most used first,
	// or the one author or title.
	Found []cleanupName `json:"found"`
	// Fix is what applying it does: merge the spellings into Suggested,
	// remove the author from the books, take the books out of the series of
	// the author's name, or retitle the book to Suggested.
	Fix       string `json:"fix"`
	Suggested string `json:"suggested,omitempty"`
	// Books is how many books it is about.
	Books int `json:"books"`
	// Book is the book of a title.
	Book *uuid.UUID `json:"book,omitempty"`
}

type cleanupCounts struct {
	Authors      int `json:"authors"`
	Placeholders int `json:"placeholders"`
	Clashes      int `json:"clashes"`
	Titles       int `json:"titles"`
}

type cleanupList struct {
	Suggestions []cleanupSuggestion `json:"suggestions"`
	// Counts is how many suggestions there are of each kind.
	Counts cleanupCounts `json:"counts"`
	// NextCursor asks for the suggestions that follow; left out when none
	// do.
	NextCursor string `json:"nextCursor,omitempty"`
}

type cleanupPick struct {
	Subject string `json:"subject"`
	// To is, for an author, the spelling to keep when it is not the one
	// suggested: any spelling of the same name, in any order of its words.
	To string `json:"to,omitempty"`
}

type cleanupApplyRequest struct {
	Kind string `json:"kind"`
	// Picks are the suggestions to apply; left out, every one of the kind
	// that nobody dismissed.
	Picks []cleanupPick `json:"picks,omitempty"`
}

type cleanupApplied struct {
	// ID is the bulk change that applies them, as GET /bulk/{id} reports it.
	ID uuid.UUID `json:"id"`
	// Total is how many books it changes.
	Total int `json:"total"`
}

type cleanupDismissRequest struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
}

const (
	defaultCleanupPage = 50
	maxCleanupPage     = 200
)

func (s *Server) listCleanup(w http.ResponseWriter, r *http.Request) error {
	var q cleanupQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	kind := q.Kind
	if kind == "" {
		kind = cleanup.KindAuthors
	}
	limit := defaultCleanupPage
	if q.Limit > 0 {
		limit = min(q.Limit, maxCleanupPage)
	}
	var after *cleanup.Cursor
	if q.Cursor != "" {
		books, subject, ok := strings.Cut(q.Cursor, "_")
		n, err := strconv.Atoi(books)
		if !ok || err != nil {
			return ErrValidation(map[string]string{"cursor": "Give the nextCursor of the page before."})
		}
		after = &cleanup.Cursor{Books: n, Subject: subject}
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	page, err := s.Cleanup.List(r.Context(), scope, kind, after, limit)
	if err != nil {
		return err
	}
	counts, err := s.Cleanup.Counts(r.Context(), scope)
	if err != nil {
		return err
	}
	out := cleanupList{
		Suggestions: make([]cleanupSuggestion, len(page.Suggestions)),
		Counts: cleanupCounts{
			Authors: counts[cleanup.KindAuthors], Placeholders: counts[cleanup.KindPlaceholders],
			Clashes: counts[cleanup.KindClashes], Titles: counts[cleanup.KindTitles],
		},
	}
	if page.Next != nil {
		out.NextCursor = strconv.Itoa(page.Next.Books) + "_" + page.Next.Subject
	}
	for i, sg := range page.Suggestions {
		cs := cleanupSuggestion{
			Kind: sg.Kind, Subject: sg.Subject, Fix: sg.Fix, Suggested: sg.Suggested,
			Books: sg.Books, Book: sg.BookID, Found: make([]cleanupName, len(sg.Found)),
		}
		for j, n := range sg.Found {
			cs.Found[j] = cleanupName{Name: n.Name, Books: n.Books}
		}
		out.Suggestions[i] = cs
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) applyCleanup(w http.ResponseWriter, r *http.Request) error {
	var req cleanupApplyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	var picks []cleanup.Pick
	if req.Picks != nil {
		picks = make([]cleanup.Pick, len(req.Picks))
		for i, p := range req.Picks {
			picks[i] = cleanup.Pick{Subject: p.Subject, To: p.To}
		}
	}
	applied, err := s.Cleanup.Apply(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Kind, picks)
	var invalid catalog.EditError
	switch {
	case errors.Is(err, cleanup.ErrNotFound):
		return ErrNotFound("There is no such suggestion; it may have been dealt with.")
	case errors.Is(err, cleanup.ErrKind):
		return ErrValidation(map[string]string{"kind": "Use authors, placeholders, clashes or titles."})
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusAccepted, cleanupApplied{ID: applied.BulkChangeID, Total: applied.Books})
	return nil
}

func (s *Server) dismissCleanup(w http.ResponseWriter, r *http.Request) error {
	var req cleanupDismissRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err := s.Cleanup.Dismiss(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Kind, req.Subject)
	switch {
	case errors.Is(err, cleanup.ErrNotFound):
		return ErrNotFound("There is no such suggestion; it may have been dealt with.")
	case errors.Is(err, cleanup.ErrKind):
		return ErrValidation(map[string]string{"kind": "Use authors, placeholders, clashes or titles."})
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
