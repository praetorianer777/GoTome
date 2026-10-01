package epub

import (
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

// Metadata is what the package document says about the book.
type Metadata struct {
	// Version is the EPUB version the package declares, such as "3.0".
	Version      string
	Title        string
	Subtitle     string
	Contributors []Contributor
	// Language is the first language, as written: usually a BCP 47 tag.
	Language    string
	Identifiers []Identifier
	Publisher   string
	// Published is the publication date as written: a year, a date or a
	// timestamp.
	Published string
	// Description has its markup removed.
	Description string
	Subjects    []string
	Series      string
	// SeriesIndex is the position in Series; nil when the file gives none.
	SeriesIndex *float64
}

// Contributor is a person or body credited on the book.
type Contributor struct {
	Name string
	// FileAs is the name in sorting order, "Austen, Jane", when given.
	FileAs string
	// Role is a MARC relator code: "aut" for an author, "trl" for a
	// translator, "nrt" for a narrator. Creators without one are authors.
	Role string
}

// Identifier is one of the book's identifiers.
type Identifier struct {
	// Scheme is lower case: "isbn", "uuid", "doi", "asin", or whatever the
	// file says. Empty when it says nothing.
	Scheme string
	Value  string
}

// Authors are the names of the contributors whose role is author.
func (m Metadata) Authors() []string {
	var names []string
	for _, c := range m.Contributors {
		if c.Role == "aut" {
			names = append(names, c.Name)
		}
	}
	return names
}

type opfPackage struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Titles       []opfText    `xml:"title"`
		Creators     []opfCreator `xml:"creator"`
		Contributors []opfCreator `xml:"contributor"`
		Languages    []string     `xml:"language"`
		Identifiers  []opfIdent   `xml:"identifier"`
		Publishers   []string     `xml:"publisher"`
		Dates        []string     `xml:"date"`
		Descriptions []string     `xml:"description"`
		Subjects     []string     `xml:"subject"`
		Metas        []opfMeta    `xml:"meta"`
	} `xml:"metadata"`
	Manifest []opfItem `xml:"manifest>item"`
	Spine    struct {
		Toc   string `xml:"toc,attr"`
		Items []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type opfText struct {
	ID    string `xml:"id,attr"`
	Value string `xml:",chardata"`
}

type opfCreator struct {
	ID     string `xml:"id,attr"`
	FileAs string `xml:"file-as,attr"`
	Role   string `xml:"role,attr"`
	Value  string `xml:",chardata"`
}

type opfIdent struct {
	ID     string `xml:"id,attr"`
	Scheme string `xml:"scheme,attr"`
	Value  string `xml:",chardata"`
}

// opfMeta is both kinds of meta element: EPUB 2's name and content
// attributes, and EPUB 3's property with the value as text.
type opfMeta struct {
	ID       string `xml:"id,attr"`
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	Value    string `xml:",chardata"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

func (i *opfItem) hasProperty(name string) bool {
	for _, p := range strings.Fields(i.Properties) {
		if p == name {
			return true
		}
	}
	return false
}

func (i *opfItem) isImage() bool { return strings.HasPrefix(i.MediaType, "image/") }

// isDocument accepts the media types content documents are declared with,
// and the extension where a file declares none.
func (i *opfItem) isDocument() bool {
	switch i.MediaType {
	case "application/xhtml+xml", "text/html", "application/x-dtbook+xml":
		return true
	case "":
		lower := strings.ToLower(i.Href)
		return strings.HasSuffix(lower, ".xhtml") || strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm")
	}
	return false
}

func parsePackage(data []byte) (*opfPackage, error) {
	var pkg opfPackage
	if err := decodeXML(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (p *opfPackage) item(id string) *opfItem {
	if id == "" {
		return nil
	}
	for i := range p.Manifest {
		if p.Manifest[i].ID == id {
			return &p.Manifest[i]
		}
	}
	return nil
}

// spineItems are the manifest items in reading order. A document listed twice
// is read once.
func (p *opfPackage) spineItems() []*opfItem {
	var items []*opfItem
	seen := map[string]bool{}
	for _, ref := range p.Spine.Items {
		if item := p.item(ref.IDRef); item != nil && !seen[item.ID] {
			seen[item.ID] = true
			items = append(items, item)
		}
	}
	return items
}

// refinements collects the EPUB 3 meta elements that say more about the
// element with the given id.
func (p *opfPackage) refinements(id string) map[string]string {
	out := map[string]string{}
	if id == "" {
		return out
	}
	for _, m := range p.Metadata.Metas {
		if m.Refines == "#"+id && m.Property != "" {
			out[m.Property] = collapse(m.Value)
		}
	}
	return out
}

func (p *opfPackage) metadata() Metadata {
	md := Metadata{
		Version:   strings.TrimSpace(p.Version),
		Language:  first(p.Metadata.Languages),
		Publisher: first(p.Metadata.Publishers),
		Published: first(p.Metadata.Dates),
	}

	for _, t := range p.Metadata.Titles {
		value := collapse(t.Value)
		if value == "" {
			continue
		}
		switch kind := p.refinements(t.ID)["title-type"]; {
		case kind == "subtitle":
			if md.Subtitle == "" {
				md.Subtitle = value
			}
		case md.Title == "" && (kind == "" || kind == "main"):
			md.Title = value
		}
	}
	if md.Title == "" && len(p.Metadata.Titles) > 0 {
		md.Title = collapse(p.Metadata.Titles[0].Value)
	}

	add := func(list []opfCreator, fallbackRole string) {
		for _, c := range list {
			name := collapse(c.Value)
			if name == "" {
				continue
			}
			refined := p.refinements(c.ID)
			contributor := Contributor{
				Name:   name,
				FileAs: firstNonEmpty(collapse(c.FileAs), refined["file-as"]),
				Role:   strings.ToLower(firstNonEmpty(strings.TrimSpace(c.Role), refined["role"], fallbackRole)),
			}
			md.Contributors = append(md.Contributors, contributor)
		}
	}
	add(p.Metadata.Creators, "aut")
	add(p.Metadata.Contributors, "")

	for _, id := range p.Metadata.Identifiers {
		if ident, ok := identifier(id.Scheme, id.Value); ok {
			md.Identifiers = append(md.Identifiers, ident)
		}
	}
	for _, s := range p.Metadata.Subjects {
		if s = collapse(s); s != "" {
			md.Subjects = append(md.Subjects, s)
		}
	}
	if d := first(p.Metadata.Descriptions); d != "" {
		// Descriptions often carry the publisher's HTML.
		if strings.ContainsAny(d, "<&") {
			d, _ = markup.Text([]byte(d))
		}
		md.Description = d
	}

	md.Series, md.SeriesIndex = p.series()
	return md
}

// series reads the EPUB 3 collection, and failing that the calibre metas that
// most EPUB 2 files carry their series in.
func (p *opfPackage) series() (string, *float64) {
	for _, m := range p.Metadata.Metas {
		if m.Property != "belongs-to-collection" {
			continue
		}
		name := collapse(m.Value)
		if name == "" {
			continue
		}
		refined := p.refinements(m.ID)
		if kind := refined["collection-type"]; kind != "" && kind != "series" {
			continue
		}
		return name, parseIndex(refined["group-position"])
	}
	var name, index string
	for _, m := range p.Metadata.Metas {
		switch m.Name {
		case "calibre:series":
			name = collapse(m.Content)
		case "calibre:series_index":
			index = m.Content
		}
	}
	if name == "" {
		return "", nil
	}
	return name, parseIndex(index)
}

func parseIndex(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 || v != v {
		return nil
	}
	return &v
}

// identifier normalises the ways files name a scheme: an attribute, a URN
// prefix, or nothing at all around a bare ISBN.
func identifier(scheme, value string) (Identifier, bool) {
	value = collapse(value)
	if value == "" {
		return Identifier{}, false
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	lower := strings.ToLower(value)
	if rest, ok := strings.CutPrefix(lower, "urn:"); ok {
		// urn:isbn:…, and what GOtome writes for the others: urn:asin:….
		if name, _, found := strings.Cut(rest, ":"); found && name != "" {
			value = strings.TrimSpace(value[len("urn:")+len(name)+1:])
			if scheme == "" {
				scheme = name
			}
		}
	} else {
		for _, prefix := range []string{"isbn:", "uuid:", "doi:"} {
			if strings.HasPrefix(lower, prefix) {
				value = strings.TrimSpace(value[len(prefix):])
				if scheme == "" {
					scheme = strings.TrimSuffix(prefix, ":")
				}
				break
			}
		}
	}
	if scheme == "" && looksLikeISBN(value) {
		scheme = "isbn"
	}
	if value == "" {
		return Identifier{}, false
	}
	return Identifier{Scheme: scheme, Value: value}, true
}

func looksLikeISBN(s string) bool {
	digits := 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' || r == ' ':
		case (r == 'X' || r == 'x') && i == len(s)-1:
			digits++
		default:
			return false
		}
	}
	return digits == 10 || digits == 13
}

func first(list []string) string {
	for _, s := range list {
		if s = collapse(s); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// collapse trims a value and turns every run of whitespace into one space.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
