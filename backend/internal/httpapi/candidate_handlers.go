package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// candidateList is what the providers know about a book, the best fit first.
type candidateList struct {
	Candidates []candidateView `json:"candidates"`
	// Failures are the providers that did not answer this time.
	Failures []providerFailure `json:"failures"`
}

type candidateView struct {
	Provider string `json:"provider"`
	// ID is what the provider calls the book.
	ID string `json:"id"`
	// Score is how well the record fits the book, from 0 to 1; 1 only for
	// one that shares an identifier with it.
	Score        float64       `json:"score"`
	Title        string        `json:"title"`
	Subtitle     string        `json:"subtitle,omitempty"`
	Contributors []contributor `json:"contributors"`
	Description  string        `json:"description,omitempty"`
	Language     string        `json:"language,omitempty"`
	Published    string        `json:"published,omitempty"`
	Publisher    string        `json:"publisher,omitempty"`
	Series       string        `json:"series,omitempty"`
	SeriesIndex  *float64      `json:"seriesIndex,omitempty"`
	PageCount    *int32        `json:"pageCount,omitempty"`
	Tags         []string      `json:"tags"`
	Identifiers  []identifier  `json:"identifiers"`
	// CoverToken names the provider's cover for getCandidateCover and for
	// applying it; left out when there is none.
	CoverToken string `json:"coverToken,omitempty"`
}

type providerFailure struct {
	Provider string `json:"provider"`
	Message  string `json:"message"`
}

func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	book, err := s.Books.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	found, err := s.Metadata.Candidates(r.Context(), book)
	out := candidateList{Candidates: []candidateView{}, Failures: []providerFailure{}}
	if err != nil {
		var failures interface{ Unwrap() []error }
		errs := []error{err}
		if errors.As(err, &failures) {
			errs = failures.Unwrap()
		}
		for _, e := range errs {
			var perr *metadata.ProviderError
			if !errors.As(e, &perr) {
				return err
			}
			f := providerFailure{Provider: perr.Provider, Message: "It did not answer: " + perr.Err.Error()}
			if errors.Is(perr, metadata.ErrBusy) {
				f.Message = "It asks to be asked again later."
			}
			out.Failures = append(out.Failures, f)
		}
	}
	for _, c := range found {
		v := candidateView{
			Provider: c.Provider, ID: c.ID, Score: c.Score, Title: c.Title, Subtitle: c.Subtitle,
			Description: c.Description, Language: c.Language, Published: c.Published, Publisher: c.Publisher,
			Series: c.Series, SeriesIndex: c.SeriesIndex, PageCount: c.PageCount,
			Contributors: []contributor{}, Tags: append([]string{}, c.Tags...), Identifiers: []identifier{},
		}
		for _, p := range c.Contributors {
			v.Contributors = append(v.Contributors, contributor{Name: p.Name, Role: cmp.Or(p.Role, catalog.RoleAuthor)})
		}
		for _, ident := range c.Identifiers {
			v.Identifiers = append(v.Identifiers, identifier{Type: ident.Type, Value: ident.Value})
		}
		if c.CoverURL != "" {
			v.CoverToken = s.Metadata.CoverToken(c.Provider, c.CoverURL)
		}
		out.Candidates = append(out.Candidates, v)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// getCandidateCover sends a provider's cover through the server: the
// browser asks nobody but GOtome, and the server fetches only what it
// handed out a token for.
func (s *Server) getCandidateCover(w http.ResponseWriter, r *http.Request) error {
	image, err := s.Metadata.CoverOf(r.Context(), chi.URLParam(r, "token"))
	switch {
	case errors.Is(err, metadata.ErrBadToken), errors.Is(err, metadata.ErrNotFound):
		return ErrNotFound("There is no such cover.")
	case err != nil:
		return err
	}
	contentType := http.DetectContentType(image)
	if !strings.HasPrefix(contentType, "image/") {
		return ErrNotFound("There is no such cover.")
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	_, err = w.Write(image)
	return err
}

// applyCandidateRequest takes chosen values from a provider's record. The
// fields are those of editBookRequest without locks: what a provider says is
// recorded as the provider's and locks nothing, and locked fields are left
// as they are.
type applyCandidateRequest struct {
	editBookRequest
	Provider string `json:"provider"`
	// CoverToken is a candidate's, to take its cover.
	CoverToken string `json:"coverToken,omitempty"`
}

func (s *Server) applyCandidate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req applyCandidateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if !s.Metadata.Has(req.Provider) {
		return ErrValidation(map[string]string{"provider": "There is no such provider."})
	}
	if len(req.Locks) > 0 {
		return ErrValidation(map[string]string{"locks": "Values from a provider lock nothing; set locks by editing the book."})
	}
	// Seen first: who may not see the book must not fill the cover store.
	if _, err := s.Books.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	} else if err != nil {
		return err
	}
	e := req.edit()
	e.Source = catalog.ProviderSource(req.Provider)
	if req.CoverToken != "" {
		image, err := s.Metadata.CoverOf(r.Context(), req.CoverToken)
		if errors.Is(err, metadata.ErrBadToken) || errors.Is(err, metadata.ErrNotFound) {
			return ErrValidation(map[string]string{"coverToken": "This cover is no longer there; look for candidates again."})
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
	return s.applyEdit(w, r, id, e)
}
