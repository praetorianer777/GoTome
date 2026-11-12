package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/annotations"
	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// annotationRect is a rectangle on a PDF page, in fractions of the page
// from its top left.
type annotationRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// annotationView is one of the caller's highlights, with its note.
type annotationView struct {
	ID     uuid.UUID  `json:"id"`
	BookID uuid.UUID  `json:"bookId"`
	FileID *uuid.UUID `json:"fileId,omitempty"`
	// Kind says what locator is: cfi (an EPUB CFI), pdf (a page, with
	// rects) or xpointer (KOReader's).
	Kind       string           `json:"kind" enum:"cfi,pdf,xpointer"`
	Locator    string           `json:"locator"`
	LocatorEnd string           `json:"locatorEnd,omitempty"`
	Page       *int32           `json:"page,omitempty"`
	Rects      []annotationRect `json:"rects,omitempty"`
	// Passage is the highlighted text.
	Passage string `json:"passage"`
	Note    string `json:"note,omitempty"`
	Color   string `json:"color" enum:"yellow,orange,red,green,blue,purple,gray"`
	Chapter string `json:"chapter,omitempty"`
	// Fraction is how far into the book it lies, when known.
	Fraction *float64 `json:"fraction,omitempty"`
	// ClientID is the browser or device it came from: "web", or a KOReader.
	ClientID    string    `json:"clientId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	BookTitle   string    `json:"bookTitle,omitempty"`
	BookAuthors []string  `json:"bookAuthors,omitempty"`
}

func annotationViewOf(a annotations.Annotation) annotationView {
	v := annotationView{
		ID: a.ID, BookID: a.BookID, FileID: a.FileID, Kind: a.Kind, Locator: a.Locator, LocatorEnd: a.LocatorEnd,
		Page: a.Page, Passage: a.Passage, Note: a.Note, Color: a.Color, Chapter: a.Chapter, Fraction: a.Fraction,
		ClientID: a.ClientID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, BookTitle: a.BookTitle, BookAuthors: a.BookAuthors,
	}
	for _, r := range a.Rects {
		v.Rects = append(v.Rects, annotationRect(r))
	}
	return v
}

type annotationList struct {
	Annotations []annotationView `json:"annotations"`
	// Next is the cursor for the following page; left out at the end.
	Next string `json:"next,omitempty"`
}

type annotationsQuery struct {
	Q     string `query:"q" doc:"Words that must each occur in the passage, the note, the title or an author."`
	Book  string `query:"book" doc:"Only the annotations of this book."`
	After string `query:"after" doc:"The next cursor of the previous page."`
	Limit int    `query:"limit" doc:"How many to return, at most 200; 50 when left out."`
}

type annotationsExportQuery struct {
	Q    string `query:"q" doc:"Words that must each occur in the passage, the note, the title or an author."`
	Book string `query:"book" doc:"Only the annotations of this book."`
}

// annotationCreate is a highlight made in a reader.
type annotationCreate struct {
	FileID     *uuid.UUID       `json:"fileId,omitempty"`
	Kind       string           `json:"kind" enum:"cfi,pdf,xpointer"`
	Locator    string           `json:"locator"`
	LocatorEnd string           `json:"locatorEnd,omitempty"`
	Page       *int32           `json:"page,omitempty"`
	Rects      []annotationRect `json:"rects,omitempty"`
	Passage    string           `json:"passage"`
	Note       string           `json:"note,omitempty"`
	Color      string           `json:"color,omitempty"`
	Chapter    string           `json:"chapter,omitempty"`
	Fraction   *float64         `json:"fraction,omitempty"`
}

// annotationChange sets what is sent: a note (empty takes it away) and a
// colour.
type annotationChange struct {
	Note  *string `json:"note,omitempty"`
	Color *string `json:"color,omitempty"`
}

// deviceAnnotation is a highlight as a device names it.
type deviceAnnotation struct {
	// Key is the device's own name for it, the same every time it is sent.
	Key        string           `json:"key"`
	Kind       string           `json:"kind" enum:"cfi,pdf,xpointer"`
	Locator    string           `json:"locator"`
	LocatorEnd string           `json:"locatorEnd,omitempty"`
	Page       *int32           `json:"page,omitempty"`
	Rects      []annotationRect `json:"rects,omitempty"`
	Passage    string           `json:"passage"`
	Note       string           `json:"note,omitempty"`
	Color      string           `json:"color,omitempty"`
	Chapter    string           `json:"chapter,omitempty"`
	Fraction   *float64         `json:"fraction,omitempty"`
	// CreatedAt and UpdatedAt are when the device made and last changed it:
	// RFC 3339, or "2006-01-02 15:04:05" taken as UTC.
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// deviceAnnotations is all a device holds for one book.
type deviceAnnotations struct {
	// ClientID names the device, the same on every send.
	ClientID    string             `json:"clientId"`
	Annotations []deviceAnnotation `json:"annotations"`
}

type deviceAnnotationsSaved struct {
	Kept    int `json:"kept"`
	Removed int `json:"removed"`
}

func (s *Server) annotationRoutes() []Route {
	return []Route{
		{
			Method: http.MethodGet, Path: "/annotations", ID: "listAnnotations",
			Summary: "The caller's highlights and notes in books they see, newest first, searchable", Tag: "annotations",
			Query: annotationsQuery{}, Response: annotationList{}, Reads: ReadsLibraries, Permission: auth.PersonalManage, Handler: s.listAnnotations,
		},
		{
			Method: http.MethodGet, Path: "/annotations/export.md", ID: "exportAnnotationsMarkdown",
			Summary: "The caller's highlights and notes as a Markdown document, grouped by book", Tag: "annotations",
			Query: annotationsExportQuery{}, Produces: "text/markdown", Status: http.StatusOK, Reads: ReadsLibraries,
			Permission: auth.PersonalManage, Handler: s.exportAnnotationsMarkdown,
		},
		{
			Method: http.MethodGet, Path: "/annotations/export.csv", ID: "exportAnnotationsCsv",
			Summary: "The caller's highlights and notes as a CSV file", Tag: "annotations",
			Query: annotationsExportQuery{}, Produces: "text/csv", Status: http.StatusOK, Reads: ReadsLibraries,
			Permission: auth.PersonalManage, Handler: s.exportAnnotationsCsv,
		},
		{
			Method: http.MethodPost, Path: "/books/{bookId}/annotations", ID: "createAnnotation",
			Summary: "Keep a highlight made in a reader, with an optional note", Tag: "annotations",
			Request: annotationCreate{}, Response: annotationView{}, Status: http.StatusCreated,
			Permission: auth.PersonalManage, Handler: s.createAnnotation,
		},
		{
			Method: http.MethodPatch, Path: "/annotations/{annotationId}", ID: "changeAnnotation",
			Summary: "Change the note or the colour of one of the caller's highlights", Tag: "annotations",
			Request: annotationChange{}, Response: annotationView{}, Permission: auth.PersonalManage, Handler: s.changeAnnotation,
		},
		{
			Method: http.MethodDelete, Path: "/annotations/{annotationId}", ID: "deleteAnnotation",
			Summary: "Delete one of the caller's highlights", Tag: "annotations",
			Status: http.StatusNoContent, Permission: auth.PersonalManage, Handler: s.deleteAnnotation,
		},
		{
			Method: http.MethodPut, Path: "/device/annotations/{document}", ID: "putDeviceAnnotations",
			Summary: "A device sends all its highlights and notes of a book, named by its KOReader MD5; those it no longer sends are removed", Tag: "annotations",
			Request: deviceAnnotations{}, Response: deviceAnnotationsSaved{}, Permission: auth.PersonalManage, Devices: true,
			Handler: s.putDeviceAnnotations,
		},
	}
}

func (s *Server) annotationFilter(q annotationsQuery) (annotations.Filter, error) {
	f := annotations.Filter{Words: q.Q, After: q.After, Limit: q.Limit}
	if q.Book != "" {
		id, err := uuid.Parse(q.Book)
		if err != nil {
			return f, ErrValidation(map[string]string{"book": "That is not a book's ID."})
		}
		f.BookID = &id
	}
	return f, nil
}

func annotationProblems(err error) error {
	var problems annotations.Problems
	switch {
	case errors.Is(err, annotations.ErrNotFound):
		return ErrNotFound("There is no such annotation.")
	case errors.Is(err, annotations.ErrTooMany):
		return ErrConflict("This book holds as many highlights as it may.")
	case errors.As(err, &problems):
		return ErrValidation(problems)
	}
	return err
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) error {
	var q annotationsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	f, err := s.annotationFilter(q)
	if err != nil {
		return err
	}
	page, err := s.Annotations.List(r.Context(), library.ScopeOf(*UserFrom(r.Context())), f)
	if err != nil {
		return annotationProblems(err)
	}
	out := annotationList{Annotations: []annotationView{}, Next: page.Next}
	for _, a := range page.Items {
		out.Annotations = append(out.Annotations, annotationViewOf(a))
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) exportAnnotations(w http.ResponseWriter, r *http.Request, contentType, name string,
	write func(w http.ResponseWriter, list []annotations.Annotation) error,
) error {
	var q annotationsExportQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	f, err := s.annotationFilter(annotationsQuery{Q: q.Q, Book: q.Book})
	if err != nil {
		return err
	}
	list, err := s.Annotations.All(r.Context(), library.ScopeOf(*UserFrom(r.Context())), f)
	if err != nil {
		return annotationProblems(err)
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	return write(w, list)
}

func (s *Server) exportAnnotationsMarkdown(w http.ResponseWriter, r *http.Request) error {
	return s.exportAnnotations(w, r, "text/markdown; charset=utf-8", "gotome-highlights.md",
		func(w http.ResponseWriter, list []annotations.Annotation) error { return annotations.Markdown(w, list) })
}

func (s *Server) exportAnnotationsCsv(w http.ResponseWriter, r *http.Request) error {
	return s.exportAnnotations(w, r, "text/csv; charset=utf-8", "gotome-highlights.csv",
		func(w http.ResponseWriter, list []annotations.Annotation) error { return annotations.CSV(w, list) })
}

func rectsOf(in []annotationRect) []annotations.Rect {
	var out []annotations.Rect
	for _, r := range in {
		out = append(out, annotations.Rect(r))
	}
	return out
}

func (s *Server) createAnnotation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req annotationCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	a, err := s.Annotations.Create(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, annotations.Input{
		FileID: req.FileID, Kind: req.Kind, Locator: req.Locator, LocatorEnd: req.LocatorEnd, Page: req.Page,
		Rects: rectsOf(req.Rects), Passage: req.Passage, Note: req.Note, Color: req.Color, Chapter: req.Chapter,
		Fraction: req.Fraction,
	})
	if errors.Is(err, annotations.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return annotationProblems(err)
	}
	writeJSON(w, r, http.StatusCreated, annotationViewOf(a))
	return nil
}

func (s *Server) changeAnnotation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "annotationId", "annotation")
	if err != nil {
		return err
	}
	var req annotationChange
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	a, err := s.Annotations.Change(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.Note, req.Color)
	if err != nil {
		return annotationProblems(err)
	}
	writeJSON(w, r, http.StatusOK, annotationViewOf(a))
	return nil
}

func (s *Server) deleteAnnotation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "annotationId", "annotation")
	if err != nil {
		return err
	}
	if err := s.Annotations.Delete(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); err != nil {
		return annotationProblems(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func parseDeviceTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s *Server) putDeviceAnnotations(w http.ResponseWriter, r *http.Request) error {
	var req deviceAnnotations
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	doc, err := s.Sync.Resolve(r.Context(), scope, chi.URLParam(r, "document"))
	if errors.Is(err, kosync.ErrUnknown) {
		return ErrNotFound("GOtome has no book with this file, or not one you may see.")
	}
	if err != nil {
		return err
	}
	items := make([]annotations.Input, 0, len(req.Annotations))
	for _, a := range req.Annotations {
		in := annotations.Input{
			Kind: a.Kind, Locator: a.Locator, LocatorEnd: a.LocatorEnd, Page: a.Page, Rects: rectsOf(a.Rects),
			Passage: a.Passage, Note: a.Note, Color: a.Color, Chapter: a.Chapter, Fraction: a.Fraction, Key: a.Key,
		}
		in.CreatedAt, _ = parseDeviceTime(a.CreatedAt)
		in.UpdatedAt, _ = parseDeviceTime(a.UpdatedAt)
		items = append(items, in)
	}
	res, err := s.Annotations.Sync(r.Context(), scope, doc.Book, doc.File, req.ClientID, items)
	if err != nil {
		return annotationProblems(err)
	}
	writeJSON(w, r, http.StatusOK, deviceAnnotationsSaved{Kept: res.Kept, Removed: res.Removed})
	return nil
}
