// Package metadata asks outside sources, such as OpenLibrary, what they know
// about a book, and ranks what they answer by how well it fits the book.
// Every source is a Provider; all of them reach the network only through the
// Web they are handed, which spaces their requests as each source asks,
// remembers answers, refuses addresses inside the server's own network, and
// in tests replays recorded answers instead.
package metadata

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// Record is what one provider says about one book. An empty field says
// nothing.
type Record struct {
	// Provider is the Name of the provider that said it, and ID what that
	// provider calls the book.
	Provider     string
	ID           string
	Title        string
	Subtitle     string
	Contributors []catalog.NewContributor
	Description  string
	// Language is a BCP 47 tag.
	Language string
	// Published is "2010", "2010-08" or "2010-08-31".
	Published   string
	Publisher   string
	Series      string
	SeriesIndex *float64
	PageCount   *int32
	Tags        []string
	// Identifiers are in the form catalog.NormalizeIdentifier gives.
	Identifiers []catalog.Identifier
	// CoverURL is where the provider keeps a cover, fetched with Cover.
	CoverURL string
	// Rating is what the provider's readers think, from 0 to 5.
	Rating *float64
}

// Query is what a search by text looks for.
type Query struct {
	Title   string
	Authors []string
	// Language is the one answers are preferred in, as a BCP 47 tag.
	Language string
}

// Limits are how a provider wants to be asked.
type Limits struct {
	// Interval is the least time between two requests to it, from all jobs
	// of the process together.
	Interval time.Duration
}

// Provider is one source of metadata. It finds books and turns what its
// source answers into Records; the Web it is handed does the rest.
type Provider interface {
	// Name names the provider in records, sources and settings: openlibrary.
	Name() string
	Limits() Limits
	// Lookup finds the books with an identifier, usually one.
	Lookup(ctx context.Context, web Web, id catalog.Identifier) ([]Record, error)
	// Search finds books by title and authors.
	Search(ctx context.Context, web Web, q Query) ([]Record, error)
}

// Candidate is a record and how well it fits the book it was found for.
type Candidate struct {
	Record
	// Score is from 0 to 1; 1 only for a record with one of the book's
	// identifiers.
	Score float64
}

// ProviderError is a provider that failed while others may have answered.
type ProviderError struct {
	Provider string
	Err      error
}

func (e *ProviderError) Error() string { return e.Provider + ": " + e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// maxLookups is how many of a book's ISBNs are looked up at most: an omnibus
// can list dozens.
const maxLookups = 3

// Candidates asks every provider about the book: by its ISBNs first, and by
// its title and authors where those find nothing. The records come ranked,
// best first. A provider that fails is reported in the error, joined from
// ProviderErrors, beside what the others found.
func (s *Service) Candidates(ctx context.Context, book catalog.Book) ([]Candidate, error) {
	var isbns []catalog.Identifier
	for _, id := range book.Identifiers {
		if id.Type == catalog.IDISBN && !slices.Contains(isbns, id.Identifier) && len(isbns) < maxLookups {
			isbns = append(isbns, id.Identifier)
		}
	}
	q := Query{Title: book.Title, Authors: book.Authors(), Language: book.Language}
	if q.Language == "" {
		q.Language = s.language
	}

	var found []Candidate
	var errs []error
	for _, p := range s.providers {
		web := s.webs[p.Name()]
		records, err := func() ([]Record, error) {
			var records []Record
			for _, isbn := range isbns {
				got, err := p.Lookup(ctx, web, isbn)
				if err != nil && !errors.Is(err, ErrNotFound) {
					return nil, err
				}
				records = append(records, got...)
			}
			if len(records) > 0 {
				return records, nil
			}
			records, err := p.Search(ctx, web, q)
			if errors.Is(err, ErrNotFound) {
				return nil, nil
			}
			return records, err
		}()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			errs = append(errs, &ProviderError{Provider: p.Name(), Err: err})
			continue
		}
		for _, r := range records {
			r.Provider = p.Name()
			found = append(found, Candidate{Record: r, Score: Score(book, r)})
		}
	}
	slices.SortStableFunc(found, func(a, b Candidate) int { return cmp.Compare(b.Score, a.Score) })
	return found, errors.Join(errs...)
}

// maxCoverBytes is the largest cover taken from a provider.
const maxCoverBytes = 10 << 20

// Cover fetches a cover a provider's record points to. Covers are not kept in
// the answer cache: the cover store keeps the one that is chosen.
func (s *Service) Cover(ctx context.Context, provider, url string) ([]byte, error) {
	web, ok := s.webs[provider]
	if !ok {
		return nil, fmt.Errorf("no provider %q", provider)
	}
	return web.Get(ctx, Request{URL: url, NoCache: true, MaxBytes: maxCoverBytes})
}
