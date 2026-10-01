// Package googlebooks asks Google Books what it knows about a book. It works
// without a key, within the small quota Google gives anonymous callers, and
// with the key an administrator sets, within that key's.
package googlebooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/markup"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// Name is what records, sources and settings call the provider.
const Name = "google"

const (
	api = "https://www.googleapis.com/books/v1/volumes"
	// interval keeps well inside Google's per-second limit; the daily quota
	// is what runs out, and the Web backs off when it does.
	interval      = 500 * time.Millisecond
	searchResults = 10
	maxTags       = 8
	maxReason     = 300
)

// Secrets is where the provider finds its API key: settings.Store.
type Secrets interface {
	Secret(ctx context.Context, key string) (string, bool, error)
}

// Provider is Google Books.
type Provider struct {
	secrets Secrets
}

// New returns the provider. With nil secrets it asks without a key.
func New(secrets Secrets) *Provider { return &Provider{secrets: secrets} }

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{Interval: interval} }

type volume struct {
	ID         string `json:"id"`
	VolumeInfo struct {
		Title               string   `json:"title"`
		Subtitle            string   `json:"subtitle"`
		Authors             []string `json:"authors"`
		Publisher           string   `json:"publisher"`
		PublishedDate       string   `json:"publishedDate"`
		Description         string   `json:"description"`
		IndustryIdentifiers []struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"industryIdentifiers"`
		PageCount     int32    `json:"pageCount"`
		Categories    []string `json:"categories"`
		AverageRating float64  `json:"averageRating"`
		Language      string   `json:"language"`
		ImageLinks    struct {
			Thumbnail  string `json:"thumbnail"`
			Small      string `json:"small"`
			Medium     string `json:"medium"`
			Large      string `json:"large"`
			ExtraLarge string `json:"extraLarge"`
		} `json:"imageLinks"`
	} `json:"volumeInfo"`
}

type volumes struct {
	Items []volume `json:"items"`
}

// Lookup finds the volumes with an ISBN, and reads the first one whole: a
// search answer leaves out the larger covers.
func (p *Provider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	if id.Type != catalog.IDISBN {
		return nil, nil
	}
	var found volumes
	if err := p.get(ctx, web, api+"?"+url.Values{"q": {"isbn:" + id.Value}}.Encode(), &found); err != nil {
		return nil, err
	}
	var out []metadata.Record
	for _, v := range found.Items {
		r := record(v)
		// A search by ISBN also finds volumes that only mention it.
		if !slices.Contains(r.Identifiers, id) {
			continue
		}
		if len(out) == 0 {
			var whole volume
			err := p.get(ctx, web, api+"/"+url.PathEscape(v.ID), &whole)
			if err != nil && !errors.Is(err, metadata.ErrNotFound) {
				return nil, err
			}
			if err == nil {
				r = record(whole)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Search finds volumes by title and first author, in the language asked
// for if there is one.
func (p *Provider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, nil
	}
	terms := "intitle:" + quote(title)
	if len(q.Authors) > 0 {
		terms += " inauthor:" + quote(q.Authors[0])
	}
	params := url.Values{"q": {terms}, "maxResults": {strconv.Itoa(searchResults)}, "printType": {"books"}}
	if q.Language != "" {
		lang, _, _ := strings.Cut(q.Language, "-")
		params.Set("langRestrict", lang)
	}
	var found volumes
	if err := p.get(ctx, web, api+"?"+params.Encode(), &found); err != nil {
		return nil, err
	}
	out := make([]metadata.Record, 0, len(found.Items))
	for _, v := range found.Items {
		if v.VolumeInfo.Title != "" {
			out = append(out, record(v))
		}
	}
	return out, nil
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, "") + `"`
	}
	return s
}

func (p *Provider) get(ctx context.Context, web metadata.Web, u string, into any) error {
	req := metadata.Request{URL: u, Busy: quotaUsedUp, Reason: refusal}
	if p.secrets != nil {
		key, ok, err := p.secrets.Secret(ctx, settings.GoogleBooksKey)
		if err != nil {
			return err
		}
		if ok && key != "" {
			req.SecretQuery = url.Values{"key": {key}}
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

// quotaUsedUp is Google's way of saying 429: a 403 whose reason is a
// limit.
func quotaUsedUp(status int, body []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	for _, reason := range []string{"rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded", "quotaExceeded"} {
		if bytes.Contains(body, []byte(`"`+reason+`"`)) {
			return true
		}
	}
	return false
}

// refusal is the message of Google's error answer, which tells a key that
// is wrong from an API not enabled for it or a restriction it fails.
func refusal(body []byte) string {
	var answer struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	if m := answer.Error.Message; len(m) <= maxReason {
		return m
	}
	return strings.ToValidUTF8(answer.Error.Message[:maxReason], "") + "…"
}

func record(v volume) metadata.Record {
	info := v.VolumeInfo
	r := metadata.Record{
		ID: v.ID, Title: info.Title, Subtitle: info.Subtitle, Publisher: info.Publisher,
		Language: info.Language, Published: publishedDate(info.PublishedDate),
	}
	if info.Description != "" {
		// Google's descriptions are HTML.
		r.Description, _ = markup.Text([]byte(info.Description))
	}
	for _, name := range info.Authors {
		r.Contributors = append(r.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
	}
	for _, ident := range info.IndustryIdentifiers {
		if ident.Type != "ISBN_13" && ident.Type != "ISBN_10" {
			continue
		}
		if id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, ident.Identifier); ok && !slices.Contains(r.Identifiers, id) {
			r.Identifiers = append(r.Identifiers, id)
		}
	}
	if v.ID != "" {
		r.Identifiers = append(r.Identifiers, catalog.Identifier{Type: catalog.IDGoogle, Value: v.ID})
	}
	if info.PageCount > 0 {
		pages := info.PageCount
		r.PageCount = &pages
	}
	if info.AverageRating > 0 {
		rating := info.AverageRating
		r.Rating = &rating
	}
	// "Fiction / Romance / Regency" is three tags.
	for _, c := range info.Categories {
		for part := range strings.SplitSeq(c, "/") {
			if part = strings.TrimSpace(part); part != "" && !slices.Contains(r.Tags, part) && len(r.Tags) < maxTags {
				r.Tags = append(r.Tags, part)
			}
		}
	}
	links := info.ImageLinks
	for _, link := range []string{links.ExtraLarge, links.Large, links.Medium, links.Small, links.Thumbnail} {
		if link != "" {
			r.CoverURL = coverURL(link)
			break
		}
	}
	return r
}

// coverURL asks over HTTPS and without the page curl Google paints onto
// thumbnails.
func coverURL(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	u.Scheme = "https"
	q := u.Query()
	q.Del("edge")
	u.RawQuery = q.Encode()
	return u.String()
}

// publishedDate keeps what catalog.ParsePublished reads of Google's dates:
// "2003", "2003-05", "2003-05-01".
func publishedDate(s string) string {
	if len(s) > 10 {
		s = s[:10]
	}
	if _, _, ok := catalog.ParsePublished(s); ok {
		return s
	}
	return ""
}
