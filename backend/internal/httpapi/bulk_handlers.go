package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// bulkRequest asks for one change to many books. The books are those named,
// or, when none are, every book of the list that library and filter give.
// Either way they are fixed now: a book that matches later is not changed.
type bulkRequest struct {
	Books []uuid.UUID `json:"books,omitempty"`
	// Library and Filter select books as listBooks does.
	Library string `json:"library,omitempty"`
	Filter  string `json:"filter,omitempty"`
	// Action is edit (make the change), fetch (look each book up with the
	// metadata providers, as happens after an import) or writeBack (write
	// each book's metadata into its EPUBs).
	Action string `json:"action"`
	// Change is what an edit does; the other actions take none.
	Change *bulkChange `json:"change,omitempty"`
}

// bulkChange is an edit of many books. Every field it changes is locked, as
// a person's edit of one book is; a field already locked on a book is left
// as it is and reported, unless includeLocked.
type bulkChange struct {
	Language  *string `json:"language,omitempty"`
	Published *string `json:"published,omitempty"`
	Publisher *string `json:"publisher,omitempty"`
	// Series names the series; each book keeps its position in it. ""
	// takes the books out of their series.
	Series *string `json:"series,omitempty"`
	// Authors replace each book's authors; narrators and the other
	// contributors stay.
	Authors       *[]string `json:"authors,omitempty"`
	AddAuthors    []string  `json:"addAuthors,omitempty"`
	RemoveAuthors []string  `json:"removeAuthors,omitempty"`
	Tags          *[]string `json:"tags,omitempty"`
	AddTags       []string  `json:"addTags,omitempty"`
	RemoveTags    []string  `json:"removeTags,omitempty"`
	// Locks puts the lock on a field (true) or takes it off (false).
	Locks map[string]bool `json:"locks,omitempty"`
	// IncludeLocked changes locked fields too.
	IncludeLocked bool `json:"includeLocked,omitempty"`
}

type bulkStarted struct {
	ID uuid.UUID `json:"id"`
	// Total is how many books the change takes.
	Total int `json:"total"`
}

func (s *Server) startBulk(w http.ResponseWriter, r *http.Request) error {
	var req bulkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if !slices.Contains(bulk.Actions, req.Action) {
		return ErrValidation(map[string]string{"action": "The action is edit, fetch or writeBack."})
	}
	var change catalog.Change
	switch {
	case req.Action == bulk.ActionEdit && req.Change == nil:
		return ErrValidation(map[string]string{"change": "Say what to change."})
	case req.Action != bulk.ActionEdit && req.Change != nil:
		return ErrValidation(map[string]string{"change": "Only an edit takes a change."})
	case req.Change != nil:
		c := req.Change
		change = catalog.Change{
			Language: c.Language, Published: c.Published, Publisher: c.Publisher, Series: c.Series,
			Authors: c.Authors, AddAuthors: c.AddAuthors, RemoveAuthors: c.RemoveAuthors,
			Tags: c.Tags, AddTags: c.AddTags, RemoveTags: c.RemoveTags,
			Locks: c.Locks, IncludeLocked: c.IncludeLocked,
		}
		var invalid catalog.EditError
		if err := change.Check(); errors.As(err, &invalid) {
			return ErrValidation(invalid)
		} else if err != nil {
			return err
		}
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
	case len(books) == 0:
		return ErrValidation(map[string]string{"books": "None of these books is one you may change."})
	}
	id, err := s.Bulk.Start(r.Context(), scope, books, req.Action, change)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, bulkStarted{ID: id, Total: len(books)})
	return nil
}

// bulkStatus is a bulk change and what it came to so far.
type bulkStatus struct {
	ID         uuid.UUID  `json:"id"`
	Action     string     `json:"action"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Total      int        `json:"total"`
	// Done is how many books have had their turn.
	Done int `json:"done"`
	// Counts says how many books came to each outcome.
	Counts map[string]int `json:"counts"`
	// Books are in the order they are changed in.
	Books []bulkResult `json:"books"`
}

type bulkResult struct {
	BookID uuid.UUID `json:"bookId"`
	Title  string    `json:"title"`
	// Outcome is left out until the book's turn has come. Changed,
	// unchanged, locked (every field the edit would change is locked),
	// review (the lookup found matches to review), notFound or failed.
	Outcome string `json:"outcome,omitempty"`
	// Message says why it failed.
	Message string `json:"message,omitempty"`
	// Skipped are the fields left as they were because they are locked.
	Skipped []string `json:"skipped"`
}

func (s *Server) getBulk(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bulkId", "bulk change")
	if err != nil {
		return err
	}
	st, err := s.Bulk.Status(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, bulk.ErrNotFound) {
		return ErrNotFound("There is no such bulk change.")
	}
	if err != nil {
		return err
	}
	out := bulkStatus{
		ID: st.ID, Action: st.Action, CreatedAt: st.CreatedAt, FinishedAt: st.FinishedAt,
		Total: len(st.Books), Counts: map[string]int{}, Books: make([]bulkResult, len(st.Books)),
	}
	for i, b := range st.Books {
		out.Books[i] = bulkResult{BookID: b.BookID, Title: b.Title, Outcome: b.Outcome, Message: b.Message, Skipped: b.Skipped}
		if b.Outcome != "" {
			out.Done++
			out.Counts[b.Outcome]++
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
