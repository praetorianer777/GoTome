// Package crossref asks CrossRef (api.crossref.org), the registry of DOIs,
// what it knows about a book or a paper: by its DOI, by its ISBN, or by
// title and author. It needs no key; given a contact address it is served
// from CrossRef's "polite pool", which is faster.
package crossref

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/markup"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Name is what records, sources and settings call the provider.
const Name = "crossref"

const (
	api = "https://api.crossref.org/works"
	// interval keeps to the three list requests a second CrossRef allows
	// the polite pool, the strictest of its limits that applies here.
	interval      = 350 * time.Millisecond
	lookupResults = 5
	searchResults = 10
	maxAuthors    = 5
	maxTags       = 8
)

// Contact says the address CrossRef may write to about the requests: the
// setting metadata.contactEmail. Empty is none.
type Contact func(ctx context.Context) string

// Provider is CrossRef.
type Provider struct {
	contact Contact
}

// New returns the provider. A nil contact gives none.
func New(contact Contact) *Provider { return &Provider{contact: contact} }

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{Interval: interval} }

type person struct {
	Given  string `json:"given"`
	Family string `json:"family"`
	// Name is an organisation's, or a person's where it is not split.
	Name string `json:"name"`
}

func (p person) String() string {
	if p.Name != "" {
		return p.Name
	}
	return strings.TrimSpace(p.Given + " " + p.Family)
}

type date struct {
	Parts [][]int `json:"date-parts"`
}

// String is "2009", "2009-08" or "2009-08-25", as far as the date is known.
func (d date) String() string {
	if len(d.Parts) == 0 || len(d.Parts[0]) == 0 || d.Parts[0][0] == 0 {
		return ""
	}
	p := d.Parts[0]
	out := fmt.Sprintf("%04d", p[0])
	for _, n := range p[1:min(len(p), 3)] {
		out += fmt.Sprintf("-%02d", n)
	}
	return out
}

type work struct {
	DOI            string   `json:"DOI"`
	Type           string   `json:"type"`
	Title          []string `json:"title"`
	Subtitle       []string `json:"subtitle"`
	ContainerTitle []string `json:"container-title"`
	Author         []person `json:"author"`
	Editor         []person `json:"editor"`
	Translator     []person `json:"translator"`
	Publisher      string   `json:"publisher"`
	Published      date     `json:"published"`
	Issued         date     `json:"issued"`
	ISBN           []string `json:"ISBN"`
	Abstract       string   `json:"abstract"`
	Language       string   `json:"language"`
	Subject        []string `json:"subject"`
}

// books are the types of work whose container is a series of books
// rather than a journal.
var books = []string{"book", "monograph", "edited-book", "reference-book"}

func (w work) record() metadata.Record {
	r := metadata.Record{ID: strings.ToLower(w.DOI), Publisher: w.Publisher}
	if len(w.Title) > 0 {
		r.Title = strings.TrimSpace(w.Title[0])
	}
	if len(w.Subtitle) > 0 {
		r.Subtitle = strings.TrimSpace(w.Subtitle[0])
	}
	if len(w.ContainerTitle) > 0 && slices.Contains(books, w.Type) {
		r.Series = w.ContainerTitle[0]
	}
	r.Published = w.Published.String()
	if r.Published == "" {
		r.Published = w.Issued.String()
	}
	for role, people := range map[string][]person{
		catalog.RoleAuthor: w.Author, catalog.RoleEditor: w.Editor, catalog.RoleTranslator: w.Translator,
	} {
		for i, p := range people {
			if name := p.String(); name != "" && i < maxAuthors {
				r.Contributors = append(r.Contributors, catalog.NewContributor{Name: name, Role: role})
			}
		}
	}
	// The map's order is random; authors first, then the rest.
	slices.SortStableFunc(r.Contributors, func(a, b catalog.NewContributor) int {
		return rank(a.Role) - rank(b.Role)
	})
	if w.Abstract != "" {
		// JATS, which reads as HTML would but for its prefixed tags.
		text, _ := markup.Text([]byte(w.Abstract))
		r.Description = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "Abstract"))
	}
	if base, err := language.ParseBase(w.Language); err == nil && w.Language != "" {
		r.Language = base.String()
	}
	if len(w.Subject) > 0 {
		r.Tags = w.Subject[:min(len(w.Subject), maxTags)]
	}
	if id, ok := catalog.NormalizeIdentifier(catalog.IDDOI, w.DOI); ok {
		r.Identifiers = append(r.Identifiers, id)
	}
	for _, isbn := range w.ISBN {
		if id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, isbn); ok && !slices.Contains(r.Identifiers, id) {
			r.Identifiers = append(r.Identifiers, id)
		}
	}
	return r
}

func rank(role string) int {
	return slices.Index([]string{catalog.RoleAuthor, catalog.RoleEditor, catalog.RoleTranslator}, role)
}

// Lookup finds the work with a DOI, or the books with an ISBN.
func (p *Provider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	switch id.Type {
	case catalog.IDDOI:
		var answer struct {
			Message work `json:"message"`
		}
		if err := p.get(ctx, web, api+"/"+doiPath(id.Value), &answer); err != nil {
			return nil, err
		}
		if r := answer.Message.record(); r.Title != "" {
			return []metadata.Record{r}, nil
		}
		return nil, metadata.ErrNotFound
	case catalog.IDISBN:
		// A book's chapters carry its ISBN too and come first; filters of
		// one name are alternatives to CrossRef.
		filter := "isbn:" + id.Value
		for _, t := range books {
			filter += ",type:" + t
		}
		params := url.Values{"filter": {filter}, "rows": {strconv.Itoa(lookupResults)}}
		found, err := p.list(ctx, web, params)
		if err != nil {
			return nil, err
		}
		found = slices.DeleteFunc(found, func(r metadata.Record) bool { return !slices.Contains(r.Identifiers, id) })
		if len(found) == 0 {
			return nil, metadata.ErrNotFound
		}
		return found, nil
	}
	return nil, nil
}

// Search finds works by title and first author.
func (p *Provider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, nil
	}
	params := url.Values{"query.bibliographic": {title}, "rows": {strconv.Itoa(searchResults)}}
	if len(q.Authors) > 0 {
		params.Set("query.author", q.Authors[0])
	}
	return p.list(ctx, web, params)
}

func (p *Provider) list(ctx context.Context, web metadata.Web, params url.Values) ([]metadata.Record, error) {
	var answer struct {
		Message struct {
			Items []work `json:"items"`
		} `json:"message"`
	}
	if err := p.get(ctx, web, api+"?"+params.Encode(), &answer); err != nil {
		return nil, err
	}
	var out []metadata.Record
	for _, w := range answer.Message.Items {
		// A chapter shares its book's ISBN and is not the book.
		if w.Type == "book-chapter" || w.Type == "book-part" || w.Type == "book-section" {
			continue
		}
		if r := w.record(); r.Title != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (p *Provider) get(ctx context.Context, web metadata.Web, u string, into any) error {
	req := metadata.Request{URL: u}
	if p.contact != nil {
		// In the query as CrossRef asks, but neither kept with the answer
		// nor part of a recorded fixture: it is a person's address.
		if mail := p.contact(ctx); mail != "" {
			req.SecretQuery = url.Values{"mailto": {mail}}
		}
	}
	body, err := web.Get(ctx, req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s: %w", u, err)
	}
	return nil
}

// doiPath escapes a DOI for the path, keeping the slashes it is made of.
func doiPath(doi string) string {
	parts := strings.Split(doi, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
