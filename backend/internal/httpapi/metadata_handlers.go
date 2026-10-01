package httpapi

import (
	"cmp"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// editBookRequest changes how a book is described. A field left out stays as
// it is; an empty one removes the value. Every field sent is locked against
// automatic updates, unless locks says otherwise for it.
type editBookRequest struct {
	Title       *string `json:"title,omitempty"`
	Subtitle    *string `json:"subtitle,omitempty"`
	Description *string `json:"description,omitempty"`
	// Language is a BCP 47 tag such as en, de or pt-BR.
	Language *string `json:"language,omitempty"`
	// Published is 2010, 2010-08 or 2010-08-31.
	Published *string      `json:"published,omitempty"`
	Publisher *string      `json:"publisher,omitempty"`
	Series    *seriesPlace `json:"series,omitempty"`
	// PageCount 0 removes the count.
	PageCount *int32 `json:"pageCount,omitempty"`
	// Contributors replace the book's, in the order they are credited. A
	// name the catalogue does not know yet is added.
	Contributors *[]contributor `json:"contributors,omitempty"`
	Tags         *[]string      `json:"tags,omitempty"`
	// Identifiers replace the book's own; those its files carry stay. An
	// ISBN is checked.
	Identifiers *[]identifier `json:"identifiers,omitempty"`
	// Locks puts the lock on a field (true) or takes it off (false), whether
	// or not the request changes it.
	Locks map[string]bool `json:"locks,omitempty"`
}

type seriesPlace struct {
	// Name "" takes the book out of its series.
	Name  string   `json:"name"`
	Index *float64 `json:"index,omitempty"`
}

func (s *Server) editBook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req editBookRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	e := catalog.Edit{
		Title: req.Title, Subtitle: req.Subtitle, Description: req.Description, Language: req.Language,
		Published: req.Published, Publisher: req.Publisher, PageCount: req.PageCount, Tags: req.Tags,
		Locks: req.Locks,
	}
	if req.Series != nil {
		e.Series = &catalog.SeriesPlace{Name: req.Series.Name, Index: req.Series.Index}
	}
	if req.Contributors != nil {
		credits := make([]catalog.NewContributor, len(*req.Contributors))
		for i, c := range *req.Contributors {
			credits[i] = catalog.NewContributor{Name: c.Name, Role: c.Role}
		}
		e.Contributors = &credits
	}
	if req.Identifiers != nil {
		idents := make([]catalog.Identifier, len(*req.Identifiers))
		for i, ident := range *req.Identifiers {
			idents[i] = catalog.Identifier{Type: ident.Type, Value: ident.Value}
		}
		e.Identifiers = &idents
	}
	return s.applyEdit(w, r, id, e)
}

// applyEdit makes the edit and answers with the book as it is now.
func (s *Server) applyEdit(w http.ResponseWriter, r *http.Request, id uuid.UUID, e catalog.Edit) error {
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	err := s.Books.Edit(r.Context(), scope, id, e)
	var invalid catalog.EditError
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrNotFound("There is no such book.")
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	}
	b, err := s.Books.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, detailOf(b, user))
	return nil
}

// coverForm is the multipart form a cover is sent as.
type coverForm struct {
	// File is a JPEG, PNG, GIF or WebP image.
	File string `json:"file"`
}

// maxCoverBytes is the most a cover image sent by hand may take up.
const maxCoverBytes = 20 << 20

func (s *Server) putBookCover(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	const how = "Send the image as multipart/form-data, in a field named file."
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverBytes+multipartSlack)
	mr, err := r.MultipartReader()
	if err != nil {
		return ErrBadRequest(how)
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "file" {
		return ErrBadRequest(how)
	}
	defer part.Close()
	data, err := io.ReadAll(part)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return ErrValidation(map[string]string{"file": "A cover may be at most 20 MiB."})
	case err != nil:
		return err
	}
	// The book is looked for first: who may not see it must not fill the
	// cover store.
	if _, err := s.Books.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	} else if err != nil {
		return err
	}
	key, err := s.Covers.Put(data)
	if errors.Is(err, covers.ErrNotAnImage) {
		return ErrValidation(map[string]string{"file": "This is not an image GOtome can read. Send a JPEG, PNG, GIF or WebP."})
	}
	if err != nil {
		return err
	}
	return s.applyEdit(w, r, id, catalog.Edit{Cover: &key})
}

func (s *Server) deleteBookCover(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	none := ""
	return s.applyEdit(w, r, id, catalog.Edit{Cover: &none})
}

type namesQuery struct {
	Kind  string `query:"kind" enum:"author,series,publisher,tag" doc:"Which names to look in."`
	Q     string `query:"q" doc:"The beginning of a word of the name."`
	Limit int    `query:"limit" doc:"How many names to return, at most 50; 10 when left out."`
}

// nameList is names already in use, the best match first.
type nameList struct {
	Names []string `json:"names"`
}

func (s *Server) listNames(w http.ResponseWriter, r *http.Request) error {
	var q namesQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	if q.Kind == "" {
		return ErrValidation(map[string]string{"kind": "Say which names to look in: author, series, publisher or tag."})
	}
	names, err := s.Books.Names(r.Context(), library.ScopeOf(*UserFrom(r.Context())), q.Kind, q.Q, min(cmp.Or(q.Limit, defaultHits), 50))
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, nameList{Names: names})
	return nil
}
