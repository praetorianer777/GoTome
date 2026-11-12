// Package canned is a metadata provider that answers from a file of
// records instead of a source on the network. It is for the browser tests,
// whose stack reaches no provider (GOTOME_OFFLINE): only
// GOTOME_TEST_METADATA puts it in place of the others.
package canned

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Name is what records, sources and settings call the provider.
const Name = "canned"

// entry is one record as the file writes it.
type entry struct {
	ID          string   `json:"id"`
	Work        string   `json:"work"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle"`
	Authors     []string `json:"authors"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Published   string   `json:"published"`
	Publisher   string   `json:"publisher"`
	Series      string   `json:"series"`
	SeriesIndex *float64 `json:"seriesIndex"`
	PageCount   *int32   `json:"pageCount"`
	Tags        []string `json:"tags"`
	ISBNs       []string `json:"isbns"`
}

// Provider answers from the records of one file.
type Provider struct {
	records []metadata.Record
}

// Open reads the records of a file: a JSON array of objects with the
// fields of entry.
func Open(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p := &Provider{}
	for _, e := range entries {
		r := metadata.Record{
			ID: e.ID, Work: e.Work, Title: e.Title, Subtitle: e.Subtitle, Description: e.Description, Language: e.Language,
			Published: e.Published, Publisher: e.Publisher, Series: e.Series, SeriesIndex: e.SeriesIndex,
			PageCount: e.PageCount, Tags: e.Tags,
		}
		for _, name := range e.Authors {
			r.Contributors = append(r.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
		}
		for _, isbn := range e.ISBNs {
			id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, isbn)
			if !ok {
				return nil, fmt.Errorf("%s: %q of %q is not an ISBN", path, isbn, e.Title)
			}
			r.Identifiers = append(r.Identifiers, id)
		}
		p.records = append(p.records, r)
	}
	return p, nil
}

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{} }

// Lookup finds the records that carry the identifier.
func (p *Provider) Lookup(_ context.Context, _ metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	return p.where(func(r metadata.Record) bool { return slices.Contains(r.Identifiers, id) })
}

// Search finds the records every word of whose title the query's title
// holds, so that a test's title made unique by a stamp still finds them.
func (p *Provider) Search(_ context.Context, _ metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	asked := strings.Fields(catalog.Key(q.Title))
	return p.where(func(r metadata.Record) bool {
		words := strings.Fields(catalog.Key(r.Title))
		return len(words) > 0 && !slices.ContainsFunc(words, func(w string) bool { return !slices.Contains(asked, w) })
	})
}

// ByAuthor lists the records of an author, whichever way the name is
// spelled.
func (p *Provider) ByAuthor(_ context.Context, _ metadata.Web, name string) ([]metadata.Record, error) {
	key := catalog.PersonKey(name)
	return p.where(func(r metadata.Record) bool {
		return slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool { return catalog.PersonKey(c.Name) == key })
	})
}

// BySeries lists the records of a series.
func (p *Provider) BySeries(_ context.Context, _ metadata.Web, name string) ([]metadata.Record, error) {
	key := catalog.Key(name)
	return p.where(func(r metadata.Record) bool { return r.Series != "" && catalog.Key(r.Series) == key })
}

func (p *Provider) where(keep func(metadata.Record) bool) ([]metadata.Record, error) {
	var out []metadata.Record
	for _, r := range p.records {
		if keep(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, metadata.ErrNotFound
	}
	return out, nil
}
