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
	Kind   string `query:"kind" enum:"authors,placeholders,clashes,titles,series" doc:"Which suggestions: authors (one person under several spellings, the default), placeholders (a value a tool left in place of an author), clashes (an author that is the name of the books' series), titles (what a shop added to a title) or series (a series written into the titles of books without one)."`
	Cursor string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; suggestions about the most books come first."`
	Limit  int    `query:"limit" doc:"How many suggestions a page holds, at most 200; 50 when left out."`
}

type cleanupName struct {
	Name string `json:"name"`
	// Books is how many books the caller sees carry it.
	Books int `json:"books"`
	// Book is, for a series in titles, the book of the title.
	Book *uuid.UUID `json:"book,omitempty"`
	// Position and Title are, for a series in titles, the book's place in
	// the series and its title without the series.
	Position *float64 `json:"position,omitempty"`
	Title    string   `json:"title,omitempty"`
}

// cleanupSuggestion is one thing found about the books the caller sees, and
// what applying it does.
type cleanupSuggestion struct {
	Kind string `json:"kind"`
	// Subject names the suggestion to apply or dismiss.
	Subject string `json:"subject"`
	// Found are the names found: an author's spellings, most used first,
	// the one author or title, or the titles that hold a series, in the
	// order of their places in it.
	Found []cleanupName `json:"found"`
	// Fix is what applying it does: merge the spellings into Suggested,
	// remove the author from the books, retitle the book to Suggested, or
	// put the books into the series Suggested at the places their titles
	// say, under the titles without the series. A clash of an author with
	// a series of the same name, where only some of the author's books are
	// in it, is "choose": the pick says which the name is, "nameIsSeries"
	// (the author goes from every book, and the books without the series
	// get it, at the places their titles say) or "clearSeries" (the books
	// leave the series). A preview of a choice carries the one chosen.
	Fix string `json:"fix" enum:"merge,removeAuthor,retitle,setSeries,choose,nameIsSeries,clearSeries"`
	// Suggested is the spelling to keep, the title or the series; for a
	// fix to choose, the fix the titles and the providers' answers point
	// to, if any.
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
	Series       int `json:"series"`
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
	// Fix is, for a suggestion whose fix is to choose, the one chosen:
	// nameIsSeries or clearSeries. Without it such a suggestion is refused,
	// and one that is not to choose accepts only its own fix.
	Fix string `json:"fix,omitempty"`
}

type cleanupApplyRequest struct {
	Kind string `json:"kind"`
	// Picks are the suggestions to apply; left out, every one of the kind
	// that nobody dismissed, except those whose fix is to choose.
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

type cleanupPreviewQuery struct {
	Kind    string `query:"kind" enum:"authors,placeholders,clashes,titles,series" doc:"The kind of the suggestion."`
	Subject string `query:"subject" doc:"The suggestion's subject, as the list says."`
	To      string `query:"to" doc:"For an author, the spelling to keep when it is not the one suggested."`
	Fix     string `query:"fix" enum:"nameIsSeries,clearSeries" doc:"For a suggestion whose fix is to choose, the fix to show; required then."`
	Offset  int    `query:"offset" doc:"How many books to skip."`
	Limit   int    `query:"limit" doc:"How many books to show, at most 200; 5 when left out."`
}

// cleanupField is a field of a book as the fix leaves it.
type cleanupField struct {
	// Name is title, series or contributors.
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
	// Locked says the field stays as it is, as a person locked it; After is
	// what it would be otherwise.
	Locked bool `json:"locked,omitempty"`
}

// cleanupEvidence is what a provider said about a book, which backs the
// suggestion up or not. It is only shown.
type cleanupEvidence struct {
	Provider string `json:"provider"`
	// Source is match (a match found for the book) or cached (an answer the
	// providers gave for one of its identifiers and still keep).
	Source string `json:"source" enum:"match,cached"`
	// Field is what Value is: series, title or authors.
	Field   string `json:"field" enum:"series,title,authors"`
	Value   string `json:"value"`
	Verdict string `json:"verdict,omitempty" enum:"agrees,differs"`
}

type cleanupBook struct {
	ID       uuid.UUID         `json:"id"`
	Title    string            `json:"title"`
	CoverKey string            `json:"coverKey,omitempty"`
	Fields   []cleanupField    `json:"fields"`
	Evidence []cleanupEvidence `json:"evidence"`
}

type cleanupPreview struct {
	Suggestion cleanupSuggestion `json:"suggestion"`
	// Total is how many books the fix changes, Books those from offset on.
	Total int           `json:"total"`
	Books []cleanupBook `json:"books"`
}

const (
	defaultCleanupPreview = 5
	defaultCleanupPage    = 50
	maxCleanupPage        = 200
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
			Series: counts[cleanup.KindSeries],
		},
	}
	if page.Next != nil {
		out.NextCursor = strconv.Itoa(page.Next.Books) + "_" + page.Next.Subject
	}
	for i, sg := range page.Suggestions {
		out.Suggestions[i] = cleanupSuggestionOf(sg)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func cleanupSuggestionOf(sg cleanup.Suggestion) cleanupSuggestion {
	cs := cleanupSuggestion{
		Kind: sg.Kind, Subject: sg.Subject, Fix: sg.Fix, Suggested: sg.Suggested,
		Books: sg.Books, Book: sg.BookID, Found: make([]cleanupName, len(sg.Found)),
	}
	for j, n := range sg.Found {
		cs.Found[j] = cleanupName{Name: n.Name, Books: n.Books, Book: n.Book}
		if n.Place != nil {
			cs.Found[j].Position, cs.Found[j].Title = &n.Place.Position, n.Place.Title
		}
	}
	return cs
}

func (s *Server) previewCleanup(w http.ResponseWriter, r *http.Request) error {
	var q cleanupPreviewQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	if q.Subject == "" {
		return ErrValidation(map[string]string{"subject": "Name the suggestion."})
	}
	limit := defaultCleanupPreview
	if q.Limit > 0 {
		limit = min(q.Limit, maxCleanupPage)
	}
	preview, err := s.Cleanup.Preview(r.Context(), library.ScopeOf(*UserFrom(r.Context())), q.Kind, q.Subject,
		cleanup.Options{To: q.To, Fix: q.Fix, Offset: q.Offset, Limit: limit})
	var invalid catalog.EditError
	switch {
	case errors.Is(err, cleanup.ErrNotFound):
		return ErrNotFound("There is no such suggestion; it may have been dealt with.")
	case errors.Is(err, cleanup.ErrKind):
		return ErrValidation(map[string]string{"kind": "Use authors, placeholders, clashes, titles or series."})
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	}
	out := cleanupPreview{
		Suggestion: cleanupSuggestionOf(preview.Suggestion), Total: preview.Total,
		Books: make([]cleanupBook, len(preview.Books)),
	}
	for i, b := range preview.Books {
		cb := cleanupBook{
			ID: b.ID, Title: b.Title, CoverKey: b.CoverKey,
			Fields: make([]cleanupField, len(b.Fields)), Evidence: make([]cleanupEvidence, len(b.Evidence)),
		}
		for j, f := range b.Fields {
			cb.Fields[j] = cleanupField{Name: f.Name, Before: f.Before, After: f.After, Locked: f.Locked}
		}
		for j, e := range b.Evidence {
			cb.Evidence[j] = cleanupEvidence{Provider: e.Provider, Source: e.Source, Field: e.Field, Value: e.Value, Verdict: e.Verdict}
		}
		out.Books[i] = cb
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
			picks[i] = cleanup.Pick{Subject: p.Subject, To: p.To, Fix: p.Fix}
		}
	}
	applied, err := s.Cleanup.Apply(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Kind, picks)
	var invalid catalog.EditError
	switch {
	case errors.Is(err, cleanup.ErrNotFound):
		return ErrNotFound("There is no such suggestion; it may have been dealt with.")
	case errors.Is(err, cleanup.ErrKind):
		return ErrValidation(map[string]string{"kind": "Use authors, placeholders, clashes, titles or series."})
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
		return ErrValidation(map[string]string{"kind": "Use authors, placeholders, clashes, titles or series."})
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
