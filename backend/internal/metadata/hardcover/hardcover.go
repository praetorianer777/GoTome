// Package hardcover asks Hardcover (hardcover.app) what it knows about a
// book, through its GraphQL API. Hardcover knows series and their order
// better than most sources. It needs a token, which a person takes from
// their Hardcover account; without one the provider asks nothing.
package hardcover

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Name is what records, sources and settings call the provider.
const Name = "hardcover"

const (
	api = "https://api.hardcover.app/v1/graphql"
	// interval keeps to the 60 requests a minute Hardcover allows a token.
	interval      = 1100 * time.Millisecond
	searchResults = 10
	maxAuthors    = 5
	maxTags       = 8
)

// Token is the person's API token: the setting metadata.hardcoverToken.
// Empty is none.
type Token func(ctx context.Context) string

// Provider is Hardcover.
type Provider struct {
	token Token
}

// New returns the provider. With a nil token, or an empty one, it asks
// nothing and finds nothing.
func New(token Token) *Provider { return &Provider{token: token} }

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{Interval: interval} }

// Hardcover answers at most three levels deep, so an edition and its book
// are two requests.
const editionsByISBN = `query EditionsByISBN($isbn: String!) {
  editions(where: {isbn_13: {_eq: $isbn}}, limit: 5) {
    id title subtitle isbn_10 isbn_13 pages release_date book_id
    publisher { name }
    language { code2 }
    image { url }
  }
}`

const bookByID = `query BookByID($id: Int!) {
  books(where: {id: {_eq: $id}}) {
    id title subtitle description release_date pages rating cached_tags
    image { url }
    contributions { contribution author { name } }
    book_series(order_by: {featured: desc}, limit: 1) { position series { name } }
  }
}`

const searchBooks = `query SearchBooks($query: String!, $perPage: Int!) {
  search(query: $query, query_type: "Book", per_page: $perPage) { results }
}`

type image struct {
	URL string `json:"url"`
}

type edition struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	ISBN10      string `json:"isbn_10"`
	ISBN13      string `json:"isbn_13"`
	Pages       int32  `json:"pages"`
	ReleaseDate string `json:"release_date"`
	BookID      int    `json:"book_id"`
	Publisher   *struct {
		Name string `json:"name"`
	} `json:"publisher"`
	Language *struct {
		Code2 string `json:"code2"`
	} `json:"language"`
	Image *image `json:"image"`
}

type contribution struct {
	Contribution *string `json:"contribution"`
	Author       *struct {
		Name string `json:"name"`
	} `json:"author"`
}

type book struct {
	ID            int             `json:"id"`
	Title         string          `json:"title"`
	Subtitle      string          `json:"subtitle"`
	Description   string          `json:"description"`
	ReleaseDate   string          `json:"release_date"`
	Pages         int32           `json:"pages"`
	Rating        *float64        `json:"rating"`
	CachedTags    json.RawMessage `json:"cached_tags"`
	Image         *image          `json:"image"`
	Contributions []contribution  `json:"contributions"`
	BookSeries    []struct {
		Position *float64 `json:"position"`
		Series   *struct {
			Name string `json:"name"`
		} `json:"series"`
	} `json:"book_series"`
	// Editions are asked for only by the works queries, for the language.
	Editions []struct {
		Language *struct {
			Code2 string `json:"code2"`
		} `json:"language"`
	} `json:"editions"`
}

// language is the one language of the book's editions Hardcover names, or
// none where they name several or none: Hardcover keeps a translation as
// a book of its own.
func (b book) language() string {
	found := ""
	for _, e := range b.Editions {
		if e.Language == nil || e.Language.Code2 == "" {
			continue
		}
		if found != "" && found != e.Language.Code2 {
			return ""
		}
		found = e.Language.Code2
	}
	return found
}

// roles are Hardcover's contributions as GOtome's roles; one it names
// otherwise, a foreword or a cover, is left out.
var roles = map[string]string{
	"": catalog.RoleAuthor, "author": catalog.RoleAuthor, "translator": catalog.RoleTranslator,
	"editor": catalog.RoleEditor, "illustrator": catalog.RoleIllustrator, "narrator": catalog.RoleNarrator,
}

// Lookup finds the editions with an ISBN and describes the first by what
// it and its book say: the edition its title, publisher and date, the book
// its authors, series and description.
func (p *Provider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	token := p.tokenOf(ctx)
	if id.Type != catalog.IDISBN || token == "" {
		return nil, nil
	}
	var found struct {
		Editions []edition `json:"editions"`
	}
	if err := p.query(ctx, web, token, editionsByISBN, map[string]any{"isbn": id.Value}, &found); err != nil {
		return nil, err
	}
	if len(found.Editions) == 0 {
		return nil, metadata.ErrNotFound
	}
	ed := found.Editions[0]
	var b book
	if ed.BookID != 0 {
		var books struct {
			Books []book `json:"books"`
		}
		if err := p.query(ctx, web, token, bookByID, map[string]any{"id": ed.BookID}, &books); err != nil {
			return nil, err
		}
		if len(books.Books) > 0 {
			b = books.Books[0]
		}
	}
	r := b.record()
	r.Title = cmp.Or(ed.Title, r.Title)
	r.Subtitle = cmp.Or(ed.Subtitle, r.Subtitle)
	r.Published = cmp.Or(ed.ReleaseDate, r.Published)
	if ed.Pages > 0 {
		r.PageCount = &ed.Pages
	}
	if ed.Publisher != nil {
		r.Publisher = ed.Publisher.Name
	}
	if ed.Language != nil {
		r.Language = ed.Language.Code2
	}
	if ed.Image != nil && ed.Image.URL != "" {
		r.CoverURL = ed.Image.URL
	}
	var ids []catalog.Identifier
	for _, isbn := range []string{ed.ISBN13, ed.ISBN10} {
		if i, ok := catalog.NormalizeIdentifier(catalog.IDISBN, isbn); ok && isbn != "" && !slices.Contains(ids, i) {
			ids = append(ids, i)
		}
	}
	r.Identifiers = append(ids, r.Identifiers...)
	if r.Title == "" {
		return nil, metadata.ErrNotFound
	}
	return []metadata.Record{r}, nil
}

func (b book) record() metadata.Record {
	r := metadata.Record{
		Title: b.Title, Subtitle: b.Subtitle, Description: strings.TrimSpace(b.Description),
		Published: b.ReleaseDate, Rating: b.Rating,
	}
	if b.ID != 0 {
		r.ID = strconv.Itoa(b.ID)
		r.Identifiers = []catalog.Identifier{{Type: catalog.IDHardcover, Value: r.ID}}
	}
	if b.Pages > 0 {
		r.PageCount = &b.Pages
	}
	if b.Image != nil {
		r.CoverURL = b.Image.URL
	}
	for _, c := range b.Contributions {
		role, ok := roles[""]
		if c.Contribution != nil {
			role, ok = roles[strings.ToLower(*c.Contribution)]
		}
		if !ok || c.Author == nil || c.Author.Name == "" || len(r.Contributors) == maxAuthors {
			continue
		}
		r.Contributors = append(r.Contributors, catalog.NewContributor{Name: c.Author.Name, Role: role})
	}
	if len(b.BookSeries) > 0 && b.BookSeries[0].Series != nil {
		r.Series = b.BookSeries[0].Series.Name
		r.SeriesIndex = b.BookSeries[0].Position
	}
	r.Tags = genres(b.CachedTags)
	return r
}

// genres are the genres among a book's cached tags, which Hardcover keeps
// by category: {"Genre": [{"tag": "Fantasy", ...}], "Mood": [...]}.
func genres(cached json.RawMessage) []string {
	var byCategory map[string][]struct {
		Tag string `json:"tag"`
	}
	if json.Unmarshal(cached, &byCategory) != nil {
		return nil
	}
	var out []string
	for _, t := range byCategory["Genre"] {
		if t.Tag != "" && len(out) < maxTags {
			out = append(out, t.Tag)
		}
	}
	return out
}

// hit is a book as Hardcover's search finds it.
type hit struct {
	ID                     json.Number `json:"id"`
	Title                  string      `json:"title"`
	Subtitle               string      `json:"subtitle"`
	AuthorNames            []string    `json:"author_names"`
	SeriesNames            []string    `json:"series_names"`
	FeaturedSeriesPosition *float64    `json:"featured_series_position"`
	ReleaseYear            int         `json:"release_year"`
	Pages                  int32       `json:"pages"`
	Rating                 *float64    `json:"rating"`
	Description            string      `json:"description"`
	Genres                 []string    `json:"genres"`
	Image                  *image      `json:"image"`
}

// Search finds books by title and first author. A book stands for all its
// editions, so its records carry no ISBN: any one would be a guess.
func (p *Provider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	token := p.tokenOf(ctx)
	title := strings.TrimSpace(q.Title)
	if token == "" || title == "" {
		return nil, nil
	}
	text := title
	if len(q.Authors) > 0 {
		text += " " + q.Authors[0]
	}
	var found struct {
		Search struct {
			Results struct {
				Hits []struct {
					Document hit `json:"document"`
				} `json:"hits"`
			} `json:"results"`
		} `json:"search"`
	}
	if err := p.query(ctx, web, token, searchBooks, map[string]any{"query": text, "perPage": searchResults}, &found); err != nil {
		return nil, err
	}
	var out []metadata.Record
	for _, h := range found.Search.Results.Hits {
		d := h.Document
		if d.Title == "" {
			continue
		}
		r := metadata.Record{
			ID: d.ID.String(), Title: d.Title, Subtitle: d.Subtitle, Rating: d.Rating,
			Description: strings.TrimSpace(d.Description), Tags: d.Genres[:min(len(d.Genres), maxTags)],
		}
		for i, name := range d.AuthorNames {
			if i < maxAuthors {
				r.Contributors = append(r.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
			}
		}
		if len(d.SeriesNames) > 0 {
			r.Series, r.SeriesIndex = d.SeriesNames[0], d.FeaturedSeriesPosition
		}
		if d.ReleaseYear > 0 {
			r.Published = strconv.Itoa(d.ReleaseYear)
		}
		if d.Pages > 0 {
			pages := d.Pages
			r.PageCount = &pages
		}
		if d.Image != nil {
			r.CoverURL = d.Image.URL
		}
		if r.ID != "" {
			r.Identifiers = []catalog.Identifier{{Type: catalog.IDHardcover, Value: r.ID}}
		}
		out = append(out, r)
	}
	return out, nil
}

// worksLimit is how many of an author's or a series' books are asked for,
// the newest first: what is new is among them.
const worksLimit = 50

const searchNamed = `query SearchNamed($query: String!, $type: String!) {
  search(query: $query, query_type: $type, per_page: 5) { results }
}`

// The books an author is credited on; ByAuthor keeps those they wrote.
const booksByAuthor = `query BooksByAuthor($id: Int!, $limit: Int!) {
  books(where: {contributions: {author_id: {_eq: $id}}},
        order_by: {release_date: desc_nulls_last}, limit: $limit) {
    id title subtitle release_date
    image { url }
    contributions { contribution author { name } }
    book_series(order_by: {featured: desc}, limit: 1) { position series { name } }
    editions(limit: 5) { language { code2 } }
  }
}`

const booksInSeries = `query BooksInSeries($id: Int!, $limit: Int!) {
  books(where: {book_series: {series_id: {_eq: $id}}},
        order_by: {release_date: desc_nulls_last}, limit: $limit) {
    id title subtitle release_date
    image { url }
    contributions { contribution author { name } }
    book_series(where: {series_id: {_eq: $id}}, limit: 1) { position series { name } }
    editions(limit: 5) { language { code2 } }
  }
}`

// ByAuthor lists the books of the author Hardcover's search finds under
// the name, newest first, leaving out those they only narrated or
// translated; release dates in the future are announcements.
func (p *Provider) ByAuthor(ctx context.Context, web metadata.Web, name string) ([]metadata.Record, error) {
	records, err := p.works(ctx, web, "Author", booksByAuthor, name)
	return slices.DeleteFunc(records, func(r metadata.Record) bool {
		return !slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool {
			return c.Role == catalog.RoleAuthor && metadata.SameName(c.Name, name)
		})
	}), err
}

// BySeries lists the books of the series Hardcover's search finds under
// the name, newest first.
func (p *Provider) BySeries(ctx context.Context, web metadata.Web, name string) ([]metadata.Record, error) {
	return p.works(ctx, web, "Series", booksInSeries, name)
}

func (p *Provider) works(ctx context.Context, web metadata.Web, kind, query, name string) ([]metadata.Record, error) {
	token := p.tokenOf(ctx)
	name = metadata.NaturalName(name)
	if token == "" || name == "" {
		return nil, nil
	}
	id, err := p.findNamed(ctx, web, token, kind, name)
	if err != nil {
		return nil, err
	}
	var found struct {
		Books []book `json:"books"`
	}
	// Asked afresh every time: what is new is the point.
	if err := p.queryWith(ctx, web, token, query, map[string]any{"id": id, "limit": worksLimit}, &found, true); err != nil {
		return nil, err
	}
	var out []metadata.Record
	for _, b := range found.Books {
		if r := b.record(); r.Title != "" {
			r.Language = b.language()
			out = append(out, r)
		}
	}
	return out, nil
}

// findNamed is the ID of the author or series Hardcover's search finds
// under exactly the name, in either order.
func (p *Provider) findNamed(ctx context.Context, web metadata.Web, token, kind, name string) (int, error) {
	var found struct {
		Search struct {
			Results struct {
				Hits []struct {
					Document struct {
						ID   json.Number `json:"id"`
						Name string      `json:"name"`
					} `json:"document"`
				} `json:"hits"`
			} `json:"results"`
		} `json:"search"`
	}
	if err := p.query(ctx, web, token, searchNamed, map[string]any{"query": name, "type": kind}, &found); err != nil {
		return 0, err
	}
	for _, h := range found.Search.Results.Hits {
		if metadata.SameName(h.Document.Name, name) {
			if id, err := strconv.Atoi(h.Document.ID.String()); err == nil {
				return id, nil
			}
		}
	}
	return 0, metadata.ErrNotFound
}

func (p *Provider) tokenOf(ctx context.Context) string {
	if p.token == nil {
		return ""
	}
	// The settings page shows "Bearer" in Hardcover's own instructions;
	// a token pasted with it works the same.
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p.token(ctx)), "Bearer "))
}

// errUnauthorized is Hardcover refusing the token.
var errUnauthorized = errors.New("hardcover refused the API token; set a new one in Settings")

// query posts a GraphQL query. Hardcover answers errors with 200 and an
// errors list, which is an error here.
func (p *Provider) query(ctx context.Context, web metadata.Web, token, query string, vars map[string]any, into any) error {
	return p.queryWith(ctx, web, token, query, vars, into, false)
}

func (p *Provider) queryWith(ctx context.Context, web metadata.Web, token, query string, vars map[string]any, into any, fresh bool) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	answer, err := web.Get(ctx, metadata.Request{
		Method: http.MethodPost, URL: api, Body: body, NoCache: fresh,
		Header: http.Header{"Authorization": {"Bearer " + token}, "Content-Type": {"application/json"}},
	})
	if err != nil {
		// The Web says only the status of an answer other than 200 or 404.
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			return errUnauthorized
		}
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(bytes.NewReader(answer)).Decode(&envelope); err != nil {
		return fmt.Errorf("hardcover: %w", err)
	}
	if len(envelope.Errors) > 0 {
		var msgs []string
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("hardcover: %s", strings.Join(msgs, "; "))
	}
	if err := json.Unmarshal(envelope.Data, into); err != nil {
		return fmt.Errorf("hardcover: %w", err)
	}
	return nil
}
