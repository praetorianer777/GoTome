package releases

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

// ErrMalformedFeed is an answer that is not an RSS 2.0 or Atom feed.
var ErrMalformedFeed = errors.New("not an RSS or Atom feed")

const (
	// maxItems is how many of a feed's items are read, the first in it.
	maxItems = 500
	// maxTitle is how long a title or a name is kept, in characters.
	maxTitle = 500
)

const (
	atomSpace = "http://www.w3.org/2005/Atom"
	dcSpace   = "http://purl.org/dc/elements/1.1/"
)

// parsedFeed is what a feed says, whichever of the formats it is in.
type parsedFeed struct {
	Title string
	Items []feedItem
}

// feedItem is one entry of a feed.
type feedItem struct {
	// Key is what the item is told apart by: its guid or id, else its link,
	// else its title.
	Key        string
	Title      string
	Link       string
	Authors    []string
	Categories []string
	// Summary is its description or content as text.
	Summary   string
	Published *time.Time
}

type rssDoc struct {
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string    `xml:"title"`
	Links       []rssLink `xml:"link"`
	GUID        string    `xml:"guid"`
	Authors     []string  `xml:"author"`
	Creators    []string  `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Categories  []string  `xml:"category"`
	Description string    `xml:"description"`
	PubDate     string    `xml:"pubDate"`
	Date        string    `xml:"http://purl.org/dc/elements/1.1/ date"`
}

// rssLink is RSS's link, or an atom:link a feed puts beside it.
type rssLink struct {
	XMLName xml.Name
	Href    string `xml:"href,attr"`
	Rel     string `xml:"rel,attr"`
	Text    string `xml:",chardata"`
}

type atomDoc struct {
	Title   atomText    `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string     `xml:"id"`
	Title      atomText   `xml:"title"`
	Links      []atomLink `xml:"link"`
	Authors    []atomName `xml:"author"`
	Categories []struct {
		Term  string `xml:"term,attr"`
		Label string `xml:"label,attr"`
	} `xml:"category"`
	Summary   atomText `xml:"summary"`
	Content   atomText `xml:"content"`
	Published string   `xml:"published"`
	Updated   string   `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomName struct {
	Name string `xml:"name"`
}

// atomText is Atom's text construct: plain text, escaped HTML, or XHTML
// inline.
type atomText struct {
	Type  string `xml:"type,attr"`
	Chars string `xml:",chardata"`
	Inner string `xml:",innerxml"`
}

func (t atomText) text() string {
	switch t.Type {
	case "html":
		return htmlText(t.Chars)
	case "xhtml":
		return htmlText(t.Inner)
	}
	return plain(t.Chars)
}

// parseFeed reads an RSS 2.0 or Atom document, fetched from base, which
// links in it are taken relative to.
func parseFeed(body []byte, base *url.URL) (parsedFeed, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.CharsetReader = charset.NewReaderLabel
	for {
		tok, err := d.Token()
		if err != nil {
			return parsedFeed{}, malformed(err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case start.Name.Local == "rss":
			var doc rssDoc
			if err := d.DecodeElement(&doc, &start); err != nil {
				return parsedFeed{}, malformed(err)
			}
			return rssFeed(doc, base), nil
		case start.Name.Local == "feed" && start.Name.Space == atomSpace:
			var doc atomDoc
			if err := d.DecodeElement(&doc, &start); err != nil {
				return parsedFeed{}, malformed(err)
			}
			return atomFeed(doc, base), nil
		}
		return parsedFeed{}, fmt.Errorf("%w: the document is <%s>", ErrMalformedFeed, start.Name.Local)
	}
}

func malformed(err error) error {
	return fmt.Errorf("%w: %v", ErrMalformedFeed, err)
}

func rssFeed(doc rssDoc, base *url.URL) parsedFeed {
	out := parsedFeed{Title: clip(plain(doc.Channel.Title))}
	for _, it := range doc.Channel.Items {
		item := feedItem{
			Title:      clip(plain(it.Title)),
			Summary:    htmlText(it.Description),
			Categories: trimmed(it.Categories),
			Published:  parseDate(it.PubDate, it.Date),
		}
		for _, l := range it.Links {
			href := l.Text
			if l.XMLName.Space == atomSpace {
				if l.Rel != "" && l.Rel != "alternate" {
					continue
				}
				href = l.Href
			}
			if item.Link = resolve(base, href); item.Link != "" {
				break
			}
		}
		for _, a := range append(it.Authors, it.Creators...) {
			item.Authors = append(item.Authors, rssAuthor(a))
		}
		item.Authors = trimmed(item.Authors)
		if keyed(&item, it.GUID) {
			out.Items = append(out.Items, item)
		}
		if len(out.Items) == maxItems {
			break
		}
	}
	return out
}

func atomFeed(doc atomDoc, base *url.URL) parsedFeed {
	out := parsedFeed{Title: clip(doc.Title.text())}
	for _, e := range doc.Entries {
		item := feedItem{
			Title:     clip(e.Title.text()),
			Summary:   e.Summary.text(),
			Published: parseDate(e.Published, e.Updated),
		}
		if item.Summary == "" {
			item.Summary = e.Content.text()
		}
		for _, l := range e.Links {
			if l.Rel != "" && l.Rel != "alternate" {
				continue
			}
			if item.Link = resolve(base, l.Href); item.Link != "" {
				break
			}
		}
		for _, a := range e.Authors {
			item.Authors = append(item.Authors, a.Name)
		}
		item.Authors = trimmed(item.Authors)
		for _, c := range e.Categories {
			if c.Label != "" {
				item.Categories = append(item.Categories, c.Label)
			} else {
				item.Categories = append(item.Categories, c.Term)
			}
		}
		item.Categories = trimmed(item.Categories)
		if keyed(&item, e.ID) {
			out.Items = append(out.Items, item)
		}
		if len(out.Items) == maxItems {
			break
		}
	}
	return out
}

// keyed gives the item its key and a title, and reports whether it has
// both: an item without either is nothing to tell of.
func keyed(item *feedItem, id string) bool {
	if item.Title == "" {
		item.Title = clip(item.Summary)
	}
	item.Key = strings.TrimSpace(id)
	if item.Key == "" {
		item.Key = item.Link
	}
	if item.Key == "" {
		item.Key = catalog.Key(item.Title)
	}
	return item.Key != "" && item.Title != ""
}

// rssAuthor is the name in RSS's author, which is an address with the name
// after it in brackets, though feeds often write the name alone.
func rssAuthor(s string) string {
	s = strings.TrimSpace(s)
	if open := strings.Index(s, "("); open > 0 && strings.HasSuffix(s, ")") && strings.Contains(s[:open], "@") {
		return strings.TrimSpace(s[open+1 : len(s)-1])
	}
	return s
}

// resolve is a link of the feed as an absolute web address, or "" for one
// that is not: a page links to it, and a javascript: address there would
// run in the page.
func resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04 -0700", time.DateOnly,
}

// parseDate is the first of the dates that reads as one.
func parseDate(values ...string) *time.Time {
	for _, v := range values {
		v = strings.TrimSpace(v)
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, v); err == nil {
				t = t.UTC()
				return &t
			}
		}
	}
	return nil
}

// plain collapses a text's whitespace.
func plain(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// htmlText is the text of a piece of HTML, on one line.
func htmlText(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return plain(s)
	}
	text, _ := markup.Text([]byte(s))
	return plain(text)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxTitle {
		return s
	}
	return string([]rune(s)[:maxTitle]) + "…"
}

func trimmed(list []string) []string {
	var out []string
	for _, s := range list {
		if s = clip(plain(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
