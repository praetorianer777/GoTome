// Package dnb asks the Deutsche Nationalbibliothek (dnb.de), which holds
// what is published in German and announces forthcoming titles from the
// publishers' notices, through its free SRU interface. It needs no key and
// answers in MARC 21 XML.
package dnb

import (
	"context"
	"encoding/xml"
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
const Name = "dnb"

const (
	endpoint = "https://services.dnb.de/sru/dnb"
	// interval is slower than the DNB's terms need, which ask for no more
	// than a few requests a second from one client.
	interval      = time.Second
	lookupResults = 5
	searchResults = 10
	worksResults  = 100
	maxAuthors    = 5
	maxTags       = 8
	// worksYears is how far back a list of an author's books goes: the
	// request is the point of following, which is what is new.
	worksYears = 1
)

// Provider is the DNB.
type Provider struct {
	now func() time.Time
}

// New returns the provider.
func New() *Provider { return &Provider{now: time.Now} }

func (*Provider) Name() string { return Name }

func (*Provider) Limits() metadata.Limits { return metadata.Limits{Interval: interval} }

type subfield struct {
	Code string `xml:"code,attr"`
	Text string `xml:",chardata"`
}

type datafield struct {
	Tag  string     `xml:"tag,attr"`
	Ind1 string     `xml:"ind1,attr"`
	Ind2 string     `xml:"ind2,attr"`
	Subs []subfield `xml:"subfield"`
}

func (d datafield) sub(code string) string {
	for _, s := range d.Subs {
		if s.Code == code {
			return clean(s.Text)
		}
	}
	return ""
}

func (d datafield) all(code string) []string {
	var out []string
	for _, s := range d.Subs {
		if s.Code == code {
			out = append(out, clean(s.Text))
		}
	}
	return out
}

type controlfield struct {
	Tag  string `xml:"tag,attr"`
	Text string `xml:",chardata"`
}

type marc struct {
	Control []controlfield `xml:"controlfield"`
	Data    []datafield    `xml:"datafield"`
}

type response struct {
	Number      int `xml:"numberOfRecords"`
	Diagnostics []struct {
		Message string `xml:"message"`
		Details string `xml:"details"`
	} `xml:"diagnostics>diagnostic"`
	Records []marc `xml:"records>record>recordData>record"`
}

func (m marc) fields(tag string) []datafield {
	var out []datafield
	for _, d := range m.Data {
		if d.Tag == tag {
			out = append(out, d)
		}
	}
	return out
}

func (m marc) first(tag, code string) string {
	for _, d := range m.fields(tag) {
		if v := d.sub(code); v != "" {
			return v
		}
	}
	return ""
}

func (m marc) control(tag string) string {
	for _, c := range m.Control {
		if c.Tag == tag {
			return strings.TrimSpace(c.Text)
		}
	}
	return ""
}

// clean drops what the DNB puts into titles to mark the words a sort order
// skips (U+0098 and U+009C, or the sign of a negation), and whitespace.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u0098', '\u009c', '¬':
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "/:;,= "))
}

var (
	number = regexp.MustCompile(`\d+([.,]\d+)?`)
	pages  = regexp.MustCompile(`(?i)(\d+)\s*(S\.|Seiten?|pages|p\.)`)
	digits = regexp.MustCompile(`\d{4}`)
)

// roles maps the relator codes of $4 to the roles of a book.
var roles = map[string]string{
	"aut": catalog.RoleAuthor, "trl": catalog.RoleTranslator, "edt": catalog.RoleEditor,
	"hrsg": catalog.RoleEditor, "ill": catalog.RoleIllustrator, "nrt": catalog.RoleNarrator,
	"spk": catalog.RoleNarrator,
}

// languages are the codes of MARC 21 (ISO 639-2) the DNB writes, as BCP 47.
var languages = map[string]string{
	"ger": "de", "eng": "en", "fre": "fr", "spa": "es", "ita": "it", "dut": "nl",
	"lat": "la", "dan": "da", "swe": "sv", "nor": "no", "pol": "pl", "rus": "ru",
	"tur": "tr", "por": "pt", "cze": "cs", "hun": "hu", "gre": "el", "fin": "fi",
}

func languageTag(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if tag, ok := languages[code]; ok {
		return tag
	}
	if base, err := language.ParseBase(code); err == nil && len(code) >= 2 {
		return base.String()
	}
	return ""
}

func (m marc) record() metadata.Record {
	r := metadata.Record{ID: m.control("001")}
	if t := m.fields("245"); len(t) > 0 {
		r.Title, r.Subtitle = t[0].sub("a"), t[0].sub("b")
		if r.Subtitle == "" {
			// A part of a multi-volume work names its part in $n and $p.
			r.Subtitle = strings.TrimSpace(t[0].sub("n") + " " + t[0].sub("p"))
		}
	}

	people := append(m.fields("100"), m.fields("700")...)
	for i, d := range people {
		name := d.sub("a")
		if name == "" {
			continue
		}
		role := ""
		for _, code := range d.all("4") {
			if mapped, ok := roles[strings.ToLower(code)]; ok {
				role = mapped
				break
			}
		}
		if role == "" && d.Tag == "100" {
			role = catalog.RoleAuthor
		}
		if role == "" || i >= maxAuthors && role == catalog.RoleAuthor {
			continue
		}
		c := catalog.NewContributor{Name: metadata.NaturalName(name), Role: role}
		if !slices.Contains(r.Contributors, c) {
			r.Contributors = append(r.Contributors, c)
		}
	}

	for _, d := range m.fields("020") {
		for _, v := range append([]string{d.sub("a")}, d.all("9")...) {
			if id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, v); ok && !slices.Contains(r.Identifiers, id) {
				r.Identifiers = append(r.Identifiers, id)
			}
		}
	}

	for _, tag := range []string{"264", "260"} {
		for _, d := range m.fields(tag) {
			if tag == "264" && d.Ind2 != "1" {
				continue
			}
			if r.Publisher == "" {
				r.Publisher = d.sub("b")
			}
			if r.Published == "" {
				if y := digits.FindString(d.sub("c")); y != "" {
					r.Published = y
				}
			}
		}
	}
	// 263 is the date a notice gives for a book not out yet, as YYYYMM or
	// YYYYMMDD; it is exacter than the year of 264.
	if d := m.first("263", "a"); len(d) >= 6 && digits.MatchString(d[:4]) && (r.Published == "" || strings.HasPrefix(d, r.Published)) {
		r.Published = d[:4] + "-" + d[4:6]
		if len(d) >= 8 {
			r.Published += "-" + d[6:8]
		}
	}
	if r.Published == "" {
		if f := m.control("008"); len(f) >= 11 {
			if y := f[7:11]; digits.MatchString(y) {
				r.Published = y
			}
		}
	}

	if tag := languageTag(m.first("041", "a")); tag != "" {
		r.Language = tag
	} else if f := m.control("008"); len(f) >= 38 {
		r.Language = languageTag(f[35:38])
	}

	if match := pages.FindAllStringSubmatch(m.first("300", "a"), -1); len(match) > 0 {
		if n, err := strconv.Atoi(match[len(match)-1][1]); err == nil && n > 0 && n < 100000 {
			n32 := int32(n)
			r.PageCount = &n32
		}
	}

	for _, tag := range []string{"830", "490", "800"} {
		for _, d := range m.fields(tag) {
			name := d.sub("a")
			if tag == "800" {
				name = d.sub("t")
			}
			if name == "" {
				continue
			}
			r.Series = name
			if n := number.FindString(d.sub("v")); n != "" {
				if f, err := strconv.ParseFloat(strings.Replace(n, ",", ".", 1), 64); err == nil {
					r.SeriesIndex = &f
				}
			}
			break
		}
		if r.Series != "" {
			break
		}
	}

	for _, d := range m.fields("689") {
		if tag := d.sub("a"); tag != "" && len(r.Tags) < maxTags && !slices.Contains(r.Tags, tag) {
			r.Tags = append(r.Tags, tag)
		}
	}
	return r
}

// Lookup finds the books with an ISBN. The DNB's index "num" holds every
// number a record carries.
func (p *Provider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	if id.Type != catalog.IDISBN {
		return nil, nil
	}
	found, err := p.query(ctx, web, "num="+id.Value, lookupResults, false)
	if err != nil {
		return nil, err
	}
	found = slices.DeleteFunc(found, func(r metadata.Record) bool { return !slices.Contains(r.Identifiers, id) })
	if len(found) == 0 {
		return nil, metadata.ErrNotFound
	}
	return found, nil
}

// Search finds books by title and first author.
func (p *Provider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, nil
	}
	cql := "tit=" + quote(title)
	if len(q.Authors) > 0 {
		cql += " and per=" + quote(metadata.NaturalName(q.Authors[0]))
	}
	return p.query(ctx, web, cql, searchResults, false)
}

// ByAuthor lists the books of the author from the year before this one on,
// newest first: the DNB lists a book from the publisher's notice on, with
// the month it is to come out in.
func (p *Provider) ByAuthor(ctx context.Context, web metadata.Web, name string) ([]metadata.Record, error) {
	name = metadata.NaturalName(name)
	if name == "" {
		return nil, nil
	}
	from := p.now().Year() - worksYears
	cql := fmt.Sprintf("per=%s and jhr>=%d", quote(name), from)
	found, err := p.query(ctx, web, cql, worksResults, true)
	if err != nil {
		return nil, err
	}
	found = slices.DeleteFunc(found, func(r metadata.Record) bool {
		return !slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool {
			return c.Role == catalog.RoleAuthor && metadata.SameName(c.Name, name)
		})
	})
	sortNewest(found)
	return found, nil
}

// BySeries lists the books the catalogue puts in the series (MARC 490, 800
// and 830), newest first. The SRU interface has no index of series; `tit`
// searches the series title too, and books that only bear the name in
// their title are dropped.
func (p *Provider) BySeries(ctx context.Context, web metadata.Web, name string) ([]metadata.Record, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	found, err := p.query(ctx, web, "tit="+quote(name), worksResults, true)
	if err != nil {
		return nil, err
	}
	found = slices.DeleteFunc(found, func(r metadata.Record) bool { return !metadata.SameName(r.Series, name) })
	sortNewest(found)
	return found, nil
}

func sortNewest(records []metadata.Record) {
	slices.SortStableFunc(records, func(a, b metadata.Record) int { return -strings.Compare(a.Published, b.Published) })
}

// quote makes a phrase of CQL from text; a double quote inside it is
// dropped, as CQL would end the phrase there.
func quote(s string) string {
	return `"` + strings.Join(strings.Fields(strings.ReplaceAll(s, `"`, " ")), " ") + `"`
}

func (p *Provider) query(ctx context.Context, web metadata.Web, cql string, rows int, fresh bool) ([]metadata.Record, error) {
	params := url.Values{
		"version":        {"1.1"},
		"operation":      {"searchRetrieve"},
		"query":          {cql},
		"recordSchema":   {"MARC21-xml"},
		"maximumRecords": {strconv.Itoa(rows)},
	}
	u := endpoint + "?" + params.Encode()
	body, err := web.Get(ctx, metadata.Request{URL: u, NoCache: fresh})
	if err != nil {
		return nil, err
	}
	var answer response
	if err := xml.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("%s: %w", endpoint, err)
	}
	if len(answer.Diagnostics) > 0 {
		d := answer.Diagnostics[0]
		return nil, fmt.Errorf("%s: %s %s", endpoint, d.Message, d.Details)
	}
	var out []metadata.Record
	for _, m := range answer.Records {
		if r := m.record(); r.Title != "" {
			out = append(out, r)
		}
	}
	return out, nil
}
