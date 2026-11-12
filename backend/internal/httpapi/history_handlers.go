package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/readimport"
)

type noteRequest struct {
	// Note is the caller's private note or review; empty removes it.
	Note string `json:"note"`
}

func (s *Server) setNote(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req noteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Note) > maxNoteBytes {
		return ErrValidation(map[string]string{"note": "A note may be at most 20,000 characters."})
	}
	err = s.Books.SetNote(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.Note)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

const maxNoteBytes = 20000

type historyQuery struct {
	Status string `query:"status" enum:"reading,completed,abandoned,wishlist" doc:"Only books with this status; reading, completed and abandoned together when left out."`
	Year   int    `query:"year" doc:"Only books finished in this year (started in it, if not finished)."`
	Stars  int    `query:"stars" doc:"Only books the caller rated with this many stars."`
	Q      string `query:"q" doc:"Words of the title or of an author."`
	After  string `query:"after" doc:"The nextCursor of the page before."`
}

type historyEntry struct {
	Book       bookSummary `json:"book"`
	StartedOn  string      `json:"startedOn,omitempty"`
	FinishedOn string      `json:"finishedOn,omitempty"`
}

type historyList struct {
	Entries []historyEntry `json:"entries"`
	// Years are the years the history lies in, the latest first.
	Years      []int  `json:"years"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func dayOf(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

// listReading lists the caller's reading history: books the library holds
// and books it does not, the caller's own.
func (s *Server) listReading(w http.ResponseWriter, r *http.Request) error {
	var q historyQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	f := catalog.HistoryFilter{Year: q.Year, Stars: q.Stars, Search: q.Q, After: q.After}
	if q.Status != "" {
		f.Statuses = []string{q.Status}
	}
	page, err := s.Books.ListReading(r.Context(), library.ScopeOf(*UserFrom(r.Context())), f)
	var invalid catalog.EditError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid)
	}
	if err != nil {
		return err
	}
	out := historyList{Entries: make([]historyEntry, len(page.Entries)), Years: page.Years, NextCursor: page.Next}
	for i, e := range page.Entries {
		out.Entries[i] = historyEntry{Book: summaryOf(e.Book), StartedOn: dayOf(e.StartedOn), FinishedOn: dayOf(e.FinishedOn)}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

type importForm struct {
	// File is the CSV export of Goodreads or StoryGraph.
	File string `json:"file"`
	// Library takes the books that are new, if the caller sees several
	// libraries and has not chosen one before.
	Library string `json:"library,omitempty"`
}

type importAmbiguity struct {
	Row        int           `json:"row"`
	Title      string        `json:"title"`
	Authors    []string      `json:"authors"`
	Candidates []bookSummary `json:"candidates"`
	// Choice is the book picked, if any; Skip says the row is left out.
	Choice *uuid.UUID `json:"choice,omitempty"`
	Skip   bool       `json:"skip"`
}

type importSkipped struct {
	Row     int    `json:"row"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// readingImport is an import of a reading history: the preview of what it
// would do, then what it came to.
type readingImport struct {
	ID uuid.UUID `json:"id"`
	// Source is goodreads or storygraph.
	Source string `json:"source"`
	// State is preview, running or done.
	State     string    `json:"state"`
	Library   uuid.UUID `json:"library"`
	Overwrite bool      `json:"overwrite"`
	Total     int       `json:"total"`
	Matched   int       `json:"matched"`
	New       int       `json:"new"`
	Ambiguous int       `json:"ambiguous"`
	Maybe     int       `json:"maybe"`
	Skipped   int       `json:"skipped"`
	// RoundedRatings counts ratings of a fraction of a star, rounded to
	// whole stars.
	RoundedRatings int               `json:"roundedRatings"`
	Ambiguities    []importAmbiguity `json:"ambiguities"`
	// Maybes are rows no book fits by ISBN or title that look like one or
	// more: Choice is the book taken, Skip says the row stays a book not
	// owned, which it is when neither is set.
	Maybes      []importAmbiguity `json:"maybes"`
	SkippedRows []importSkipped   `json:"skippedRows"`
	// Outcomes counts the rows by what the import came to: created,
	// changed, unchanged, skipped, failed.
	Outcomes map[string]int `json:"outcomes"`
}

func importOf(i readimport.Import) readingImport {
	out := readingImport{
		ID: i.ID, Source: i.Source, State: i.State, Library: i.Library, Overwrite: i.Overwrite, Total: i.Total,
		Matched: i.Matched, New: i.New, Ambiguous: i.Ambiguous, Maybe: i.Maybe, Skipped: i.Skipped, RoundedRatings: i.RoundedRatings,
		Ambiguities: make([]importAmbiguity, len(i.Ambiguities)), Maybes: make([]importAmbiguity, len(i.Maybes)), SkippedRows: make([]importSkipped, len(i.SkippedRows)),
		Outcomes: i.Outcomes,
	}
	for n, a := range i.Ambiguities {
		c := make([]bookSummary, len(a.Candidates))
		for k, b := range a.Candidates {
			c[k] = summaryOf(b)
		}
		out.Ambiguities[n] = importAmbiguity{Row: a.Row, Title: a.Title, Authors: a.Authors, Candidates: c, Choice: a.Choice, Skip: a.Skip}
	}
	for n, a := range i.Maybes {
		c := make([]bookSummary, len(a.Candidates))
		for k, b := range a.Candidates {
			c[k] = summaryOf(b)
		}
		out.Maybes[n] = importAmbiguity{Row: a.Row, Title: a.Title, Authors: a.Authors, Candidates: c, Choice: a.Choice, Skip: a.Skip}
	}
	for n, k := range i.SkippedRows {
		out.SkippedRows[n] = importSkipped{Row: k.Row, Title: k.Title, Message: k.Message}
	}
	return out
}

// createImport reads an export from the form's file and answers what an
// import of it would do. Nothing is written to the caller's reading.
func (s *Server) createImport(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, readimport.MaxBytes+multipartSlack)
	mr, err := r.MultipartReader()
	if err != nil {
		return ErrBadRequest("Send the export as multipart/form-data, in a field named file.")
	}
	var data []byte
	var chosen *uuid.UUID
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrBadRequest("The upload was cut off before the whole file arrived.")
		}
		switch part.FormName() {
		case "file":
			data, err = io.ReadAll(io.LimitReader(part, readimport.MaxBytes+1))
			if err != nil {
				return ErrBadRequest("The upload was cut off before the whole file arrived.")
			}
		case "library":
			v, _ := io.ReadAll(io.LimitReader(part, 64))
			if id, err := uuid.Parse(strings.TrimSpace(string(v))); err == nil {
				chosen = &id
			} else if strings.TrimSpace(string(v)) != "" {
				return ErrValidation(map[string]string{"library": "There is no such library."})
			}
		}
	}
	if len(data) == 0 {
		return ErrBadRequest("Send the export as multipart/form-data, in a field named file.")
	}
	if len(data) > readimport.MaxBytes {
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "The export is larger than 20 MiB."}
	}
	imp, err := s.Imports.Create(r.Context(), library.ScopeOf(*UserFrom(r.Context())), data, chosen)
	switch {
	case errors.Is(err, readimport.ErrUnknownFormat):
		return ErrValidation(map[string]string{"file": "This is not a Goodreads or StoryGraph export (a CSV file with their columns)."})
	case errors.Is(err, readimport.ErrTooMany):
		return ErrValidation(map[string]string{"file": "The export has more than 20,000 rows; split it."})
	case errors.Is(err, catalog.ErrNeedsLibrary):
		return ErrValidation(map[string]string{"library": "Choose the library that takes books you do not have yet."})
	case errors.Is(err, catalog.ErrNotFound):
		return ErrValidation(map[string]string{"library": "There is no such library."})
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusCreated, importOf(imp))
	return nil
}

func (s *Server) getImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "importId", "import")
	if err != nil {
		return err
	}
	imp, err := s.Imports.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, readimport.ErrNotFound) {
		return ErrNotFound("There is no such import.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, importOf(imp))
	return nil
}

type importChoice struct {
	// Book is the candidate the row is; Skip leaves the row out. Exactly
	// one of them is sent.
	Book *uuid.UUID `json:"book,omitempty"`
	Skip bool       `json:"skip,omitempty"`
}

func (s *Server) chooseImportRow(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "importId", "import")
	if err != nil {
		return err
	}
	row, err := strconv.Atoi(chi.URLParam(r, "row"))
	if err != nil {
		return ErrNotFound("There is no such row.")
	}
	var req importChoice
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err = s.Imports.Choose(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, row, req.Book, req.Skip)
	switch {
	case errors.Is(err, readimport.ErrNotFound):
		return ErrNotFound("There is no such import.")
	case errors.Is(err, readimport.ErrStarted):
		return ErrConflict("The import was started already.")
	case errors.Is(err, readimport.ErrBadChoice):
		return ErrValidation(map[string]string{"book": "Pick one of the books offered for the row, or skip it."})
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type importStart struct {
	// Overwrite replaces what the caller already set on a book: its status,
	// rating, finish date and note. Left out, the import only fills gaps.
	Overwrite bool `json:"overwrite,omitempty"`
}

func (s *Server) startImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "importId", "import")
	if err != nil {
		return err
	}
	var req importStart
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	err = s.Imports.Start(r.Context(), scope, id, req.Overwrite)
	switch {
	case errors.Is(err, readimport.ErrNotFound):
		return ErrNotFound("There is no such import.")
	case errors.Is(err, readimport.ErrStarted):
		return ErrConflict("The import was started already.")
	case err != nil:
		return err
	}
	imp, err := s.Imports.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, importOf(imp))
	return nil
}
