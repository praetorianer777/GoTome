package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/calibre"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type calibreRequest struct {
	// Library is the external library whose folder is a Calibre library.
	Library uuid.UUID `json:"library"`
}

type calibreColumn struct {
	// Label is Calibre's name for the column, the key of the choices.
	Label string `json:"label"`
	Name  string `json:"name"`
	// Datatype is Calibre's: text, comments, series, enumeration, datetime,
	// float, int, rating or bool.
	Datatype string `json:"datatype"`
	Books    int    `json:"books"`
	// Choices are what the column may become: ignore, status, tags.
	Choices []string `json:"choices"`
}

type calibreProblem struct {
	CalibreID int64  `json:"calibreId"`
	Title     string `json:"title"`
	Message   string `json:"message"`
}

// calibreImport is an import of a Calibre library: the preview of what it
// would do, then what it came to.
type calibreImport struct {
	ID uuid.UUID `json:"id"`
	// State is preview, running or done.
	State     string    `json:"state"`
	Library   uuid.UUID `json:"library"`
	CreatedAt time.Time `json:"createdAt"`
	// Books are the books Calibre lists; Ready have a file in the library,
	// Skipped have none.
	Books   int `json:"books"`
	Ready   int `json:"ready"`
	Skipped int `json:"skipped"`
	// Formats counts Calibre's files of the books that are ready by format.
	Formats map[string]int `json:"formats"`
	// MissingFiles counts files Calibre lists that the library lacks.
	MissingFiles int `json:"missingFiles"`
	Covers       int `json:"covers"`
	Series       int `json:"series"`
	Tags         int `json:"tags"`
	// Rated counts books with a rating in Calibre.
	Rated   int             `json:"rated"`
	Columns []calibreColumn `json:"columns"`
	// Ratings and Choices are what the import was started with.
	Ratings bool              `json:"ratings"`
	Choices map[string]string `json:"choices"`
	// Problems are books left out or failed, at most 100.
	Problems []calibreProblem `json:"problems"`
	// Outcomes counts the books by what the import came to: changed,
	// unchanged, skipped, failed.
	Outcomes map[string]int `json:"outcomes"`
}

func calibreImportOf(i calibre.Import) calibreImport {
	out := calibreImport{
		ID: i.ID, State: i.State, Library: i.Library, CreatedAt: i.CreatedAt,
		Books: i.Summary.Books, Ready: i.Summary.Ready, Skipped: i.Summary.Skipped, Formats: i.Summary.Formats,
		MissingFiles: i.Summary.MissingFiles, Covers: i.Summary.Covers, Series: i.Summary.Series, Tags: i.Summary.Tags,
		Rated: i.Summary.Rated, Columns: make([]calibreColumn, len(i.Summary.Columns)), Ratings: i.Ratings, Choices: i.Columns,
		Problems: make([]calibreProblem, len(i.Problems)), Outcomes: i.Outcomes,
	}
	for n, c := range i.Summary.Columns {
		out.Columns[n] = calibreColumn{Label: c.Label, Name: c.Name, Datatype: c.Datatype, Books: c.Books, Choices: c.Choices}
	}
	for n, p := range i.Problems {
		out.Problems[n] = calibreProblem{CalibreID: p.CalibreID, Title: p.Title, Message: p.Message}
	}
	return out
}

// createCalibreImport reads the library's metadata.db and answers what an
// import would do. Nothing is written but the preview.
func (s *Server) createCalibreImport(w http.ResponseWriter, r *http.Request) error {
	var req calibreRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	imp, err := s.Calibre.Preview(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Library)
	switch {
	case errors.Is(err, library.ErrNotFound):
		return ErrValidation(map[string]string{"library": "There is no such library."})
	case errors.Is(err, calibre.ErrNotExternal):
		return ErrValidation(map[string]string{"library": "Only an external library, a folder mounted into GOtome, can be a Calibre library."})
	case errors.Is(err, calibre.ErrNotCalibre):
		return ErrValidation(map[string]string{"library": "The library's folder holds no Calibre library: there is no readable metadata.db in it."})
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusCreated, calibreImportOf(imp))
	return nil
}

func (s *Server) getCalibreImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "importId", "import")
	if err != nil {
		return err
	}
	imp, err := s.Calibre.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, calibre.ErrNotFound) {
		return ErrNotFound("There is no such import.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, calibreImportOf(imp))
	return nil
}

type calibreStart struct {
	// Ratings makes Calibre's ratings the caller's own, in whole stars,
	// where they have none.
	Ratings bool `json:"ratings,omitempty"`
	// Columns says per custom column, by its label, what it becomes:
	// ignore, status or tags, as the column's choices allow. A column left
	// out is ignored.
	Columns map[string]string `json:"columns,omitempty"`
}

func (s *Server) startCalibreImport(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "importId", "import")
	if err != nil {
		return err
	}
	var req calibreStart
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	err = s.Calibre.Start(r.Context(), scope, id, req.Ratings, req.Columns)
	switch {
	case errors.Is(err, calibre.ErrNotFound):
		return ErrNotFound("There is no such import.")
	case errors.Is(err, calibre.ErrStarted):
		return ErrConflict("The import was started already.")
	case errors.Is(err, calibre.ErrBadColumns):
		return ErrValidation(map[string]string{"columns": "Choose ignore, status or tags for a column, as its type allows."})
	case err != nil:
		return err
	}
	imp, err := s.Calibre.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, calibreImportOf(imp))
	return nil
}
