package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

type metadataSearchQuery struct {
	Title  string `query:"title" doc:"The book's title, or part of it."`
	Author string `query:"author" doc:"One of its authors."`
	ISBN   string `query:"isbn" doc:"An ISBN, with or without hyphens; found by it before anything else."`
}

// searchMetadata asks the providers for books by title, author or ISBN, for
// a person to wish for one the library does not hold.
func (s *Server) searchMetadata(w http.ResponseWriter, r *http.Request) error {
	var q metadataSearchQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	book := catalog.Book{Title: strings.TrimSpace(q.Title)}
	if a := strings.TrimSpace(q.Author); a != "" {
		book.Contributors = []catalog.Contributor{{Name: a, Role: catalog.RoleAuthor}}
	}
	if strings.TrimSpace(q.ISBN) != "" {
		id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, q.ISBN)
		if !ok {
			return ErrValidation(map[string]string{"isbn": "This is not a valid ISBN."})
		}
		book.Identifiers = []catalog.BookIdentifier{{Identifier: id}}
	}
	if book.Title == "" && len(book.Identifiers) == 0 {
		return ErrValidation(map[string]string{"title": "Give a title or an ISBN to look for."})
	}
	return s.writeCandidates(w, r, book, false)
}

// wishRequest makes a book the library does not hold yet from a provider's
// record, and puts it on the caller's wishlist, or among the books they
// read or are reading. The fields are those a candidate is applied with; a
// title is needed.
type wishRequest struct {
	editBookRequest
	// Library is where the book is to go once it arrives. Left out, it is
	// the library that holds most of the series or of the authors, else the
	// one the caller chose before, else the only one they see; otherwise
	// the request is refused for the library.
	Library  *uuid.UUID `json:"library,omitempty"`
	Provider string     `json:"provider"`
	// Status is wishlist (the default), reading or completed.
	Status string `json:"status,omitempty"`
	// Rating is 1 to 5 stars for a book read; 0 or left out is none.
	Rating int16 `json:"rating,omitempty"`
	// FinishedOn is the day a completed book was finished, as 2006-01-02.
	FinishedOn string `json:"finishedOn,omitempty"`
	// CoverToken is the candidate's, to take its cover.
	CoverToken string `json:"coverToken,omitempty"`
}

func (s *Server) createWish(w http.ResponseWriter, r *http.Request) error {
	var req wishRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if !s.Metadata.Has(req.Provider) {
		return ErrValidation(map[string]string{"provider": "There is no such provider."})
	}
	if len(req.Locks) > 0 {
		return ErrValidation(map[string]string{"locks": "A wished book locks nothing; set locks by editing it."})
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	var finished *time.Time
	if req.FinishedOn != "" {
		day, err := time.Parse(time.DateOnly, req.FinishedOn)
		if err != nil {
			return ErrValidation(map[string]string{"finishedOn": "Give the date as 2006-01-02."})
		}
		finished = &day
	}
	chosen := req.Library != nil
	var target uuid.UUID
	if chosen {
		target = *req.Library
		if _, err := s.Libraries.Get(r.Context(), scope, target); errors.Is(err, library.ErrNotFound) {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		} else if err != nil {
			return err
		}
	} else {
		var authors []string
		if req.Contributors != nil {
			for _, c := range *req.Contributors {
				authors = append(authors, c.Name)
			}
		}
		series := ""
		if req.Series != nil {
			series = req.Series.Name
		}
		var err error
		target, err = s.Books.HistoryLibrary(r.Context(), scope, authors, series)
		if errors.Is(err, catalog.ErrNeedsLibrary) {
			return ErrValidation(map[string]string{"library": "Choose the library that takes books you do not have yet."})
		} else if err != nil {
			return err
		}
	}
	e := req.edit()
	e.Source = catalog.ProviderSource(req.Provider)
	if req.CoverToken != "" {
		image, err := s.Metadata.CoverOf(r.Context(), req.CoverToken)
		if errors.Is(err, metadata.ErrBadToken) || errors.Is(err, metadata.ErrNotFound) {
			return ErrValidation(map[string]string{"coverToken": "This cover is no longer there; look again."})
		}
		if err != nil {
			return err
		}
		key, err := s.Covers.Put(image)
		if errors.Is(err, covers.ErrNotAnImage) {
			return ErrValidation(map[string]string{"coverToken": "The provider's cover is not an image GOtome can read."})
		}
		if err != nil {
			return err
		}
		e.Cover = &key
	}
	id, err := s.Books.CreatePlaceholder(r.Context(), scope, target, e, catalog.PlaceholderReading{
		Status: req.Status, Rating: req.Rating, FinishedOn: finished,
	})
	var invalid catalog.EditError
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrValidation(map[string]string{"library": "There is no such library."})
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	}
	if chosen {
		if err := s.Books.RememberHistoryLibrary(r.Context(), scope, target); err != nil {
			return err
		}
	}
	b, err := s.Books.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusCreated, detailOf(b, user))
	return nil
}
