package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/releases"
)

type namedQuery struct {
	After string `query:"after" doc:"The nextCursor of the page before."`
	Limit int    `query:"limit" doc:"How many names a page holds, at most 200; 50 when left out."`
}

// namedEntry is an author or a series in a list of them.
type namedEntry struct {
	Name string `json:"name"`
	// Key is what GET /authors/{authorKey} or GET /series/{seriesKey} takes.
	Key string `json:"key"`
	// Sort is what the list is in the order of: for an author "Austen, Jane".
	Sort string `json:"sort"`
	// Books counts the books of it the caller sees.
	Books int `json:"books"`
}

type namedList struct {
	Names []namedEntry `json:"names"`
	// NextCursor asks for the page after this one; left out on the last.
	NextCursor string `json:"nextCursor,omitempty"`
}

// listNamed answers a page of a list of names.
func (s *Server) listNamed(list string) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var q namedQuery
		if err := decodeQuery(r, &q); err != nil {
			return err
		}
		page, err := s.Books.NameList(r.Context(), library.ScopeOf(*UserFrom(r.Context())), list, filter.Node{}, q.After, cmp.Or(q.Limit, defaultPage))
		if errors.Is(err, catalog.ErrBadCursor) {
			return ErrValidation(map[string]string{"after": "This cursor belongs to another list; start again from the first page."})
		}
		if err != nil {
			return err
		}
		out := namedList{Names: make([]namedEntry, len(page.Names)), NextCursor: page.Next}
		for i, n := range page.Names {
			out.Names[i] = namedEntry{Name: n.Name, Key: n.Key, Sort: n.Sort, Books: n.Books}
		}
		writeJSON(w, r, http.StatusOK, out)
		return nil
	}
}

// authorDetail is an author as the books the caller sees name them. The
// books themselves come from GET /books with a rule on person.
type authorDetail struct {
	// Name is the spelling most of those books use.
	Name string `json:"name"`
	Key  string `json:"key"`
	// Spellings are every spelling among those books.
	Spellings []string `json:"spellings"`
	Books     int      `json:"books"`
}

// pathName is a name in the path as it was meant: the router leaves a path
// that holds an escaped comma or slash as it came.
func pathName(r *http.Request, param string) string {
	raw := chi.URLParam(r, param)
	if name, err := url.PathUnescape(raw); err == nil {
		return name
	}
	return raw
}

func (s *Server) getAuthor(w http.ResponseWriter, r *http.Request) error {
	a, err := s.Books.Author(r.Context(), library.ScopeOf(*UserFrom(r.Context())), pathName(r, "authorKey"))
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such author.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, authorDetail{Name: a.Name, Key: a.Key, Spellings: a.Spellings, Books: a.Books})
	return nil
}

// seriesDetail is a series as far as the caller sees it. Its books come from
// GET /books with a rule on series, sorted by series.
type seriesDetail struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	// Books counts its books the caller sees, Read those they completed.
	Books int `json:"books"`
	Read  int `json:"read"`
	// Next is the first book in series order the caller has not read or
	// begun; left out when there is none.
	Next *bookSummary `json:"next,omitempty"`
	// Missing are the places from 1 to its last book that no book of the
	// caller's and nothing in listed takes.
	Missing []seriesGap `json:"missing"`
	// Listed are books the sources list of it that the caller's libraries
	// lack, known while someone follows the series or an author of it.
	Listed []listedBook `json:"listed"`
	// Library is where a book wished for would join the others.
	Library uuid.UUID `json:"library"`
}

// seriesGap is a stretch of places in a series, both ends included.
type seriesGap struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// listedBook is a book of a series as a source lists it.
type listedBook struct {
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	SeriesIndex *float64 `json:"seriesIndex,omitempty"`
	// Published is as much of the date as is known; left out where no
	// source knows.
	Published string `json:"published,omitempty"`
	// Source is the provider that listed it first.
	Source string `json:"source"`
	// CoverToken fetches its cover through GET /metadata/covers/{token}.
	CoverToken *string `json:"coverToken,omitempty"`
	// ISBNs are what POST /books/wishes takes as its identifiers.
	ISBNs []string `json:"isbns"`
	// Wishable says the source is still one POST /books/wishes takes.
	Wishable bool `json:"wishable"`
	// Wished is a book wished for in a library the caller sees.
	Wished bool `json:"wished"`
}

func (s *Server) getSeries(w http.ResponseWriter, r *http.Request) error {
	scope := library.ScopeOf(*UserFrom(r.Context()))
	series, err := s.Books.Series(r.Context(), scope, pathName(r, "seriesKey"))
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such series.")
	}
	if err != nil {
		return err
	}
	listed, err := s.Releases.OfSeries(r.Context(), scope, series.Key, series.Name)
	if err != nil {
		return err
	}
	out := seriesDetail{
		Name: series.Name, Key: series.Key, Books: series.Books, Read: series.Read,
		Library: series.LibraryID, Listed: []listedBook{}, Missing: []seriesGap{},
	}
	if series.Next != nil {
		next := summaryOf(*series.Next)
		out.Next = &next
	}
	places := slices.Clone(series.Indexes)
	for _, l := range listed {
		// A title of another edition or language at a place a book takes is
		// that book.
		if l.SeriesIndex != nil && slices.Contains(series.Indexes, *l.SeriesIndex) {
			continue
		}
		out.Listed = append(out.Listed, s.listedOf(l))
		if l.SeriesIndex != nil {
			places = append(places, *l.SeriesIndex)
		}
	}
	for _, gap := range catalog.Gaps(places) {
		out.Missing = append(out.Missing, seriesGap{From: gap.From, To: gap.To})
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) listedOf(l releases.Listed) listedBook {
	out := listedBook{
		Title: l.Title, Authors: l.Authors, SeriesIndex: l.SeriesIndex, Source: l.Provider, ISBNs: l.ISBNs,
		Wishable: s.Metadata != nil && s.Metadata.Has(l.Provider), Wished: l.Wished,
	}
	if out.Authors == nil {
		out.Authors = []string{}
	}
	if out.ISBNs == nil {
		out.ISBNs = []string{}
	}
	if l.Date != nil && l.Precision != nil {
		out.Published = l.Date.Format(map[string]string{"day": time.DateOnly, "month": "2006-01", "year": "2006"}[*l.Precision])
	}
	if l.CoverURL != nil && out.Wishable {
		token := s.Metadata.CoverToken(l.Provider, *l.CoverURL)
		out.CoverToken = &token
	}
	return out
}
