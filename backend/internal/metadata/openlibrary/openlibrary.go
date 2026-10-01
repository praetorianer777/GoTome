// Package openlibrary asks OpenLibrary (openlibrary.org), which needs no key
// and knows most books in print, what it knows about a book.
package openlibrary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Name is what records, sources and settings call the provider.
const Name = "openlibrary"

const (
	site       = "https://openlibrary.org"
	coversSite = "https://covers.openlibrary.org"
	// interval keeps to the three requests a second OpenLibrary allows a
	// client that names itself.
	interval = 400 * time.Millisecond
	// maxAuthors and maxTags bound what one record brings: some editions
	// credit dozens, and works carry hundreds of subjects.
	maxAuthors    = 5
	maxTags       = 8
	searchResults = 10
)

// Provider is OpenLibrary.
type Provider struct{}

// New returns the provider.
func New() *Provider { return &Provider{} }

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{Interval: interval} }

// text is a value OpenLibrary writes either as a string or as an object
// with a type and the string as its value.
type text string

func (t *text) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*t = text(s)
		return nil
	}
	var v struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*t = text(v.Value)
	return nil
}

type ref struct {
	Key string `json:"key"`
}

type edition struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle"`
	Publishers    []string `json:"publishers"`
	PublishDate   string   `json:"publish_date"`
	NumberOfPages int32    `json:"number_of_pages"`
	ISBN10        []string `json:"isbn_10"`
	ISBN13        []string `json:"isbn_13"`
	Languages     []ref    `json:"languages"`
	Covers        []int64  `json:"covers"`
	Works         []ref    `json:"works"`
	Authors       []ref    `json:"authors"`
	Description   text     `json:"description"`
}

type work struct {
	Description text     `json:"description"`
	Subjects    []string `json:"subjects"`
	Covers      []int64  `json:"covers"`
	Authors     []struct {
		Author ref `json:"author"`
	} `json:"authors"`
}

type author struct {
	Name string `json:"name"`
}

// Lookup finds the edition with an ISBN and fills in from its work and
// authors what the edition leaves out.
func (p *Provider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	if id.Type != catalog.IDISBN {
		return nil, nil
	}
	var ed edition
	if err := getJSON(ctx, web, site+"/isbn/"+url.PathEscape(id.Value)+".json", &ed); err != nil {
		return nil, err
	}
	r := metadata.Record{
		ID:          strings.TrimPrefix(ed.Key, "/books/"),
		Title:       ed.Title,
		Subtitle:    ed.Subtitle,
		Description: cleanDescription(string(ed.Description)),
		Published:   publishDate(ed.PublishDate),
	}
	if len(ed.Publishers) > 0 {
		r.Publisher = ed.Publishers[0]
	}
	if ed.NumberOfPages > 0 {
		r.PageCount = &ed.NumberOfPages
	}
	for _, l := range ed.Languages {
		if tag := languageTag(strings.TrimPrefix(l.Key, "/languages/")); tag != "" {
			r.Language = tag
			break
		}
	}
	for _, isbn := range append(ed.ISBN13, ed.ISBN10...) {
		if ident, ok := catalog.NormalizeIdentifier(catalog.IDISBN, isbn); ok && !slices.Contains(r.Identifiers, ident) {
			r.Identifiers = append(r.Identifiers, ident)
		}
	}
	if r.ID != "" {
		r.Identifiers = append(r.Identifiers, catalog.Identifier{Type: catalog.IDOpenLibrary, Value: r.ID})
	}
	r.CoverURL = coverURL(ed.Covers)

	authors := ed.Authors
	if len(ed.Works) > 0 {
		var w work
		err := getJSON(ctx, web, site+ed.Works[0].Key+".json", &w)
		if err != nil && !errors.Is(err, metadata.ErrNotFound) {
			return nil, err
		}
		if r.Description == "" {
			r.Description = cleanDescription(string(w.Description))
		}
		r.Tags = firstTags(w.Subjects)
		if r.CoverURL == "" {
			r.CoverURL = coverURL(w.Covers)
		}
		if len(authors) == 0 {
			for _, a := range w.Authors {
				authors = append(authors, a.Author)
			}
		}
	}
	for i, a := range authors {
		if i == maxAuthors {
			break
		}
		var who author
		err := getJSON(ctx, web, site+a.Key+".json", &who)
		if errors.Is(err, metadata.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if who.Name != "" {
			r.Contributors = append(r.Contributors, catalog.NewContributor{Name: who.Name, Role: catalog.RoleAuthor})
		}
	}
	if r.Title == "" {
		return nil, nil
	}
	return []metadata.Record{r}, nil
}

type searchAnswer struct {
	Docs []struct {
		Key                 string   `json:"key"`
		Title               string   `json:"title"`
		Subtitle            string   `json:"subtitle"`
		AuthorName          []string `json:"author_name"`
		FirstPublishYear    int      `json:"first_publish_year"`
		Language            []string `json:"language"`
		CoverI              int64    `json:"cover_i"`
		NumberOfPagesMedian int32    `json:"number_of_pages_median"`
		Subject             []string `json:"subject"`
	} `json:"docs"`
}

// searchFields are the fields a search asks for; the rest of a work's
// document can be large.
const searchFields = "key,title,subtitle,author_name,first_publish_year,language,cover_i,number_of_pages_median,subject"

// Search finds works by title and first author. A work stands for all its
// editions, so its records carry no ISBN: any one would be a guess.
func (p *Provider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	if strings.TrimSpace(q.Title) == "" {
		return nil, nil
	}
	params := url.Values{"title": {q.Title}, "limit": {strconv.Itoa(searchResults)}, "fields": {searchFields}}
	if len(q.Authors) > 0 {
		params.Set("author", q.Authors[0])
	}
	var answer searchAnswer
	if err := getJSON(ctx, web, site+"/search.json?"+params.Encode(), &answer); err != nil {
		return nil, err
	}
	want := languageCode(q.Language)
	var out []metadata.Record
	for _, d := range answer.Docs {
		if d.Title == "" {
			continue
		}
		id := strings.TrimPrefix(d.Key, "/works/")
		r := metadata.Record{ID: id, Title: d.Title, Subtitle: d.Subtitle, Tags: firstTags(d.Subject)}
		for i, name := range d.AuthorName {
			if i == maxAuthors {
				break
			}
			r.Contributors = append(r.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
		}
		if d.FirstPublishYear > 0 {
			r.Published = strconv.Itoa(d.FirstPublishYear)
		}
		// A work in many languages is in the one asked for, if it is in
		// that one at all; otherwise it is in its only one.
		switch {
		case want != "" && slices.Contains(d.Language, want):
			r.Language = languageTag(want)
		case len(d.Language) == 1:
			r.Language = languageTag(d.Language[0])
		}
		if d.NumberOfPagesMedian > 0 {
			pages := d.NumberOfPagesMedian
			r.PageCount = &pages
		}
		if d.CoverI > 0 {
			r.CoverURL = coverURL([]int64{d.CoverI})
		}
		if id != "" {
			r.Identifiers = []catalog.Identifier{{Type: catalog.IDOpenLibrary, Value: id}}
		}
		out = append(out, r)
	}
	return out, nil
}

func getJSON(ctx context.Context, web metadata.Web, u string, into any) error {
	body, err := web.Get(ctx, metadata.Request{URL: u})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%s: %w", u, err)
	}
	return nil
}

// coverURL is the large size of the first real cover. default=false makes
// OpenLibrary answer 404 instead of a blank image where it has none.
func coverURL(ids []int64) string {
	for _, id := range ids {
		if id > 0 {
			return coversSite + "/b/id/" + strconv.FormatInt(id, 10) + "-L.jpg?default=false"
		}
	}
	return ""
}

// bibliographic are the MARC language codes that differ from ISO 639-3,
// which OpenLibrary uses: "ger" where ISO says "deu".
var bibliographic = map[string]string{
	"alb": "sqi", "arm": "hye", "baq": "eus", "bur": "mya", "chi": "zho", "cze": "ces", "dut": "nld",
	"fre": "fra", "geo": "kat", "ger": "deu", "gre": "ell", "ice": "isl", "mac": "mkd", "mao": "mri",
	"may": "msa", "per": "fas", "rum": "ron", "slo": "slk", "tib": "bod", "wel": "cym",
}

// languageTag turns OpenLibrary's MARC codes, "eng" or "ger", into BCP 47.
func languageTag(code string) string {
	if iso, ok := bibliographic[code]; ok {
		code = iso
	}
	base, err := language.ParseBase(code)
	if err != nil {
		return ""
	}
	return base.String()
}

// languageCode is the other way: "en-GB" to "eng".
func languageCode(tag string) string {
	if tag == "" {
		return ""
	}
	base, _ := language.Make(tag).Base()
	code := base.ISO3()
	for marc, iso := range bibliographic {
		if iso == code {
			return marc
		}
	}
	return code
}

var (
	yearOnly    = regexp.MustCompile(`^\d{4}$`)
	anyYear     = regexp.MustCompile(`(?:^|\D)(1[0-9]{3}|20[0-9]{2})(?:\D|$)`)
	dateLayouts = []struct{ layout, out string }{
		{"2006-01-02", "2006-01-02"}, {"January 2, 2006", "2006-01-02"}, {"Jan 2, 2006", "2006-01-02"},
		{"2 January 2006", "2006-01-02"}, {"January 2006", "2006-01"}, {"Jan 2006", "2006-01"}, {"2006-01", "2006-01"},
	}
)

// publishDate reads the date as OpenLibrary's editions write it, which is
// whatever the cataloguer typed: "2003", "December 1, 2003", "c1996".
func publishDate(s string) string {
	s = strings.TrimSpace(s)
	if yearOnly.MatchString(s) {
		return s
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l.layout, s); err == nil {
			return t.Format(l.out)
		}
	}
	if m := anyYear.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// cleanDescription drops the list of links some descriptions end in.
func cleanDescription(s string) string {
	if before, _, found := strings.Cut(s, "\n----------"); found {
		s = before
	}
	return strings.TrimSpace(s)
}

func firstTags(subjects []string) []string {
	var tags []string
	for _, s := range subjects {
		if len(tags) == maxTags {
			break
		}
		// Machine tags, "open_syllabus_project" or "nyt:hardcover-fiction",
		// are bookkeeping, not subjects.
		if s = strings.TrimSpace(s); s != "" && !strings.ContainsAny(s, "_:") && !slices.Contains(tags, s) {
			tags = append(tags, s)
		}
	}
	return tags
}
