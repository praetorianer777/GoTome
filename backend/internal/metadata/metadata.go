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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// Works is a provider that also lists the books of an author or of a
// series, for following them (#69). A record's Published says when a book
// came or comes out, as exactly as the provider knows it. A name the
// provider does not know is ErrNotFound.
type Works interface {
	ByAuthor(ctx context.Context, web Web, name string) ([]Record, error)
	BySeries(ctx context.Context, web Web, name string) ([]Record, error)
}

// What WorksOf lists the books of.
const (
	WorksAuthor = "author"
	WorksSeries = "series"
)

// Answer is what one provider listed.
type Answer struct {
	Provider string
	Records  []Record
}

// WorksOf asks every enabled provider that lists works for the books of an
// author or a series. It keeps those in the metadata language, in English,
// where most books are announced first, and those of no one language: the
// providers list every translation as a book of its own. A provider that
// knows no such name answers with none; one that fails is reported in the
// error, joined from ProviderErrors, beside the answers of the others.
func (s *Service) WorksOf(ctx context.Context, kind, name string) ([]Answer, error) {
	var enabled []string
	if s.enabled != nil {
		enabled = s.enabled(ctx)
	}
	wanted := []string{"en"}
	if s.language != nil {
		if l := baseLanguage(s.language(ctx)); l != "" {
			wanted = append(wanted, l)
		}
	}
	var out []Answer
	var errs []error
	for _, p := range s.providers {
		w, ok := p.(Works)
		if !ok || (s.enabled != nil && !slices.Contains(enabled, p.Name())) {
			continue
		}
		web := s.webs[p.Name()]
		var records []Record
		var err error
		if kind == WorksSeries {
			records, err = w.BySeries(ctx, web, name)
		} else {
			records, err = w.ByAuthor(ctx, web, name)
		}
		switch {
		case errors.Is(err, ErrNotFound):
			records, err = nil, nil
		case err != nil && ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			errs = append(errs, &ProviderError{Provider: p.Name(), Err: err})
			continue
		}
		records = slices.DeleteFunc(records, func(r Record) bool {
			return r.Language != "" && !slices.Contains(wanted, baseLanguage(r.Language))
		})
		for i := range records {
			records[i].Provider = p.Name()
		}
		out = append(out, Answer{Provider: p.Name(), Records: records})
	}
	return out, errors.Join(errs...)
}

// baseLanguage is a BCP 47 tag's language alone: "de" of "de-AT".
func baseLanguage(tag string) string {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
	return base
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
// can list dozens. A DOI is looked up besides.
const maxLookups = 3

// Candidates asks every provider about the book: by its ISBNs first, and by
// its title and authors where those find nothing. The records come ranked,
// best first. A provider that fails is reported in the error, joined from
// ProviderErrors, beside what the others found.
func (s *Service) Candidates(ctx context.Context, book catalog.Book) ([]Candidate, error) {
	var ids []catalog.Identifier
	for _, id := range book.Identifiers {
		if id.Type == catalog.IDISBN && !slices.Contains(ids, id.Identifier) && len(ids) < maxLookups {
			ids = append(ids, id.Identifier)
		}
	}
	// Papers and many scholarly books have a DOI and no ISBN.
	if i := slices.IndexFunc(book.Identifiers, func(id catalog.BookIdentifier) bool { return id.Type == catalog.IDDOI }); i >= 0 {
		ids = append(ids, book.Identifiers[i].Identifier)
	}
	q := Query{Title: book.Title, Authors: book.Authors(), Language: book.Language}
	if q.Language == "" && s.language != nil {
		q.Language = s.language(ctx)
	}

	var found []Candidate
	var errs []error
	var enabled []string
	if s.enabled != nil {
		enabled = s.enabled(ctx)
	}
	for _, p := range s.providers {
		if s.enabled != nil && !slices.Contains(enabled, p.Name()) {
			continue
		}
		web := s.webs[p.Name()]
		records, err := func() ([]Record, error) {
			var records []Record
			for _, id := range ids {
				got, err := p.Lookup(ctx, web, id)
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

// Has reports whether a provider of that name is asked.
func (s *Service) Has(provider string) bool {
	_, ok := s.webs[provider]
	return ok
}

// ErrBadToken is a cover token this process did not hand out.
var ErrBadToken = errors.New("no such cover")

// CoverToken stands for a cover a record points to. Only what a token names
// is fetched for a client, so that nobody can have the server fetch an
// address of their choosing.
func (s *Service) CoverToken(provider, url string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(provider + "\x00" + url))
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.sign(payload))
}

// CoverOf fetches the cover a token stands for.
func (s *Service) CoverOf(ctx context.Context, token string) ([]byte, error) {
	payload, sig, ok := strings.Cut(token, ".")
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err != nil || !hmac.Equal(mac, s.sign(payload)) {
		return nil, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrBadToken
	}
	provider, url, _ := strings.Cut(string(raw), "\x00")
	return s.Cover(ctx, provider, url)
}

func (s *Service) sign(payload string) []byte {
	h := hmac.New(sha256.New, s.tokenKey)
	h.Write([]byte(payload))
	return h.Sum(nil)
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
