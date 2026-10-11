// Package opds writes the catalogue that e-readers browse: OPDS 1.2 feeds,
// which are Atom documents, and the OpenSearch description their search
// field is made from.
package opds

import (
	"encoding/xml"
	"io"
	"time"
)

// Media types of the documents and links.
const (
	NavigationType  = "application/atom+xml;profile=opds-catalog;kind=navigation"
	AcquisitionType = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	OpenSearchType  = "application/opensearchdescription+xml"
)

// Link relations OPDS defines besides Atom's own.
const (
	RelAcquisition = "http://opds-spec.org/acquisition"
	RelImage       = "http://opds-spec.org/image"
	RelThumbnail   = "http://opds-spec.org/image/thumbnail"
	RelSubsection  = "subsection"
)

// Feed is an Atom feed: a navigation feed lists other feeds, an
// acquisition feed lists books.
type Feed struct {
	XMLName    xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	DC         string   `xml:"xmlns:dc,attr"`
	OPDS       string   `xml:"xmlns:opds,attr"`
	ID         string   `xml:"id"`
	Title      string   `xml:"title"`
	Updated    Time     `xml:"updated"`
	AuthorName string   `xml:"author>name"`
	Links      []Link   `xml:"link"`
	Entries    []Entry  `xml:"entry"`
}

// Entry is a feed in a navigation feed, or a book in an acquisition feed.
type Entry struct {
	ID       string   `xml:"id"`
	Title    string   `xml:"title"`
	Updated  Time     `xml:"updated"`
	Authors  []Person `xml:"author,omitempty"`
	Language string   `xml:"dc:language,omitempty"`
	Issued   string   `xml:"dc:issued,omitempty"`
	Content  *Content `xml:"content,omitempty"`
	Links    []Link   `xml:"link"`
}

// Person is an entry's author.
type Person struct {
	Name string `xml:"name"`
}

// Content is an entry's text, plain.
type Content struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

// Link points from a feed or an entry to another document.
type Link struct {
	Rel    string `xml:"rel,attr,omitempty"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr,omitempty"`
	Title  string `xml:"title,attr,omitempty"`
	Length int64  `xml:"length,attr,omitempty"`
}

// Time is written as RFC 3339, which Atom wants.
type Time time.Time

func (t Time) MarshalText() ([]byte, error) {
	return []byte(time.Time(t).UTC().Format(time.RFC3339)), nil
}

// TextContent is plain text as an entry's content, or nil for none.
func TextContent(text string) *Content {
	if text == "" {
		return nil
	}
	return &Content{Type: "text", Text: text}
}

// NewFeed is a feed with the namespaces its entries use.
func NewFeed(id, title string, updated time.Time) Feed {
	return Feed{
		DC: "http://purl.org/dc/terms/", OPDS: "http://opds-spec.org/2010/catalog",
		ID: id, Title: title, Updated: Time(updated), AuthorName: "GOtome",
	}
}

// Write writes the feed as an XML document.
func (f Feed) Write(w io.Writer) error {
	return write(w, f)
}

// Search is an OpenSearch description: where the catalogue's search is, as
// a template with {searchTerms} in it.
type Search struct {
	XMLName     xml.Name `xml:"http://a9.com/-/spec/opensearch/1.1/ OpenSearchDescription"`
	ShortName   string   `xml:"ShortName"`
	Description string   `xml:"Description"`
	URL         struct {
		Type     string `xml:"type,attr"`
		Template string `xml:"template,attr"`
	} `xml:"Url"`
}

// NewSearch is the description of a search at the template.
func NewSearch(name, description, template string) Search {
	s := Search{ShortName: name, Description: description}
	s.URL.Type = AcquisitionType
	s.URL.Template = template
	return s
}

// Write writes the description as an XML document.
func (s Search) Write(w io.Writer) error {
	return write(w, s)
}

func write(w io.Writer, v any) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", " ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}
