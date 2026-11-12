package dnb

import (
	"context"
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// liveVariable, set to anything, runs the contract test against the live
// API. Only the nightly workflow sets it.
const liveVariable = "GOTOME_LIVE"

var effiISBN = catalog.Identifier{Type: catalog.IDISBN, Value: "9783866471177"}

// The answers below are made by hand after the DNB's documented SRU and
// MARC 21 format, not recorded from it: they test the reading of the
// fields, while the tests that replay testdata hold the provider to what
// the DNB really says. Fixture files for those are recorded by the owner
// with make record-fixtures, and the tests skip until they exist.
func sru(records ...string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<searchRetrieveResponse xmlns="http://www.loc.gov/zing/srw/"><version>1.1</version>
<numberOfRecords>` + string(rune('0'+len(records))) + `</numberOfRecords><records>` +
		strings.Join(records, "") + `</records></searchRetrieveResponse>`)
}

func marcRecord(fields string) string {
	return `<record><recordSchema>MARC21-xml</recordSchema><recordPacking>xml</recordPacking><recordData>
<record xmlns="http://www.loc.gov/MARC21/slim"><leader>00000nam a22000003c 4500</leader>` + fields +
		`</record></recordData></record>`
}

func field(tag, ind2 string, subs ...string) string {
	out := `<datafield tag="` + tag + `" ind1=" " ind2="` + ind2 + `">`
	for i := 0; i+1 < len(subs); i += 2 {
		out += `<subfield code="` + subs[i] + `">` + subs[i+1] + `</subfield>`
	}
	return out + `</datafield>`
}

const effiFields = `<controlfield tag="001">1000000001</controlfield>
<controlfield tag="008">200101s2008    gw ||||| |||| 00||||ger  </controlfield>` +
	`<datafield tag="020" ind1=" " ind2=" "><subfield code="a">9783866471177</subfield><subfield code="9">978-3-86647-117-7</subfield></datafield>` +
	`<datafield tag="041" ind1=" " ind2=" "><subfield code="a">ger</subfield></datafield>` +
	`<datafield tag="100" ind1="1" ind2=" "><subfield code="a">Fontane, Theodor</subfield><subfield code="4">aut</subfield></datafield>` +
	`<datafield tag="245" ind1="1" ind2="0"><subfield code="a">Effi Briest</subfield><subfield code="b">Roman</subfield></datafield>` +
	`<datafield tag="264" ind1=" " ind2="1"><subfield code="a">Berlin</subfield><subfield code="b">Beispielverlag</subfield><subfield code="c">2008</subfield></datafield>` +
	`<datafield tag="300" ind1=" " ind2=" "><subfield code="a">XII, 356 Seiten</subfield></datafield>` +
	`<datafield tag="700" ind1="1" ind2=" "><subfield code="a">Muster, Maria</subfield><subfield code="4">edt</subfield></datafield>` +
	`<datafield tag="689" ind1="0" ind2="0"><subfield code="a">Gesellschaftsroman</subfield></datafield>`

// canned is a Web that answers every request with one answer, and keeps
// the requests.
type canned struct {
	answer []byte
	asked  []metadata.Request
}

func (c *canned) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	c.asked = append(c.asked, r)
	return c.answer, nil
}

func (c *canned) cql(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(c.asked[len(c.asked)-1].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("recordSchema") != "MARC21-xml" || u.Query().Get("operation") != "searchRetrieve" {
		t.Errorf("request %s", u)
	}
	return u.Query().Get("query")
}

func TestARecordIsReadFromItsFields(t *testing.T) {
	web := &canned{answer: sru(marcRecord(effiFields))}
	got, err := New().Lookup(context.Background(), web, effiISBN)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	r := got[0]
	if web.cql(t) != "num=9783866471177" {
		t.Errorf("query %q", web.cql(t))
	}
	if r.ID != "1000000001" || r.Title != "Effi Briest" || r.Subtitle != "Roman" || r.Language != "de" ||
		r.Publisher != "Beispielverlag" || r.Published != "2008" || r.PageCount == nil || *r.PageCount != 356 {
		t.Errorf("record = %+v", r)
	}
	want := []catalog.NewContributor{{Name: "Theodor Fontane", Role: catalog.RoleAuthor}, {Name: "Maria Muster", Role: catalog.RoleEditor}}
	if !slices.Equal(r.Contributors, want) {
		t.Errorf("contributors %+v", r.Contributors)
	}
	if !slices.Equal(r.Identifiers, []catalog.Identifier{effiISBN}) || !slices.Equal(r.Tags, []string{"Gesellschaftsroman"}) {
		t.Errorf("identifiers %v, tags %v", r.Identifiers, r.Tags)
	}
	book := catalog.Book{Title: "Effi Briest", Identifiers: []catalog.BookIdentifier{{Identifier: effiISBN}}}
	if score := metadata.Score(book, r); score != 1 {
		t.Errorf("score %.2f, want 1 for the same ISBN", score)
	}
}

func TestSortingMarksAreLeftOutOfTitles(t *testing.T) {
	rec := marcRecord(field("245", "0", "a", "Der \u0098Stechlin\u009c /", "b", "Roman"))
	got, err := New().Search(context.Background(), &canned{answer: sru(rec)}, metadata.Query{Title: "Der Stechlin"})
	if err != nil || len(got) != 1 || got[0].Title != "Der Stechlin" {
		t.Fatalf("Search: %+v, %v", got, err)
	}
}

func TestAnAnnouncedBookHasItsMonth(t *testing.T) {
	rec := marcRecord(field("245", "0", "a", "Der Stechlin") + field("264", "1", "c", "[2027]") + field("263", " ", "a", "202703"))
	got, err := New().Search(context.Background(), &canned{answer: sru(rec)}, metadata.Query{Title: "Der Stechlin"})
	if err != nil || len(got) != 1 || got[0].Published != "2027-03" {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	other := marcRecord(field("245", "0", "a", "Der Stechlin") + field("264", "1", "c", "2026") + field("263", " ", "a", "202703"))
	got, _ = New().Search(context.Background(), &canned{answer: sru(other)}, metadata.Query{Title: "Der Stechlin"})
	if got[0].Published != "2026" {
		t.Errorf("a date of another year than the 264: %q", got[0].Published)
	}
}

func TestASeriesAndItsPlaceAreRead(t *testing.T) {
	cases := []struct {
		name, fields, series string
		index                float64
	}{
		{"830", field("490", "1", "a", "Sammlung Beispiel", "v", "7") + field("830", " ", "a", "Beispielreihe", "v", "Bd. 3"), "Beispielreihe", 3},
		{"490", field("490", "1", "a", "Beispielreihe", "v", "12,5"), "Beispielreihe", 12.5},
		{"800", field("800", "1", "a", "May, Karl", "t", "Beispielreihe", "v", "2"), "Beispielreihe", 2},
	}
	for _, c := range cases {
		got, err := New().Search(context.Background(), &canned{answer: sru(marcRecord(field("245", "0", "a", "Band") + c.fields))}, metadata.Query{Title: "Band"})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %+v, %v", c.name, got, err)
		}
		if got[0].Series != c.series || got[0].SeriesIndex == nil || *got[0].SeriesIndex != c.index {
			t.Errorf("%s: series %q at %v", c.name, got[0].Series, got[0].SeriesIndex)
		}
	}
}

func TestSearchAsksByTitleAndAuthor(t *testing.T) {
	web := &canned{answer: sru()}
	got, err := New().Search(context.Background(), web, metadata.Query{Title: `Effi "Briest"`, Authors: []string{"Fontane, Theodor"}})
	if err != nil || got != nil {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	if q := web.cql(t); q != `woe="Effi" and woe="Briest" and woe="Fontane"` {
		t.Errorf("last query %q", q)
	}
	u, _ := url.Parse(web.asked[0].URL)
	if q := u.Query().Get("query"); q != `tit="Effi Briest" and per="Theodor Fontane"` {
		t.Errorf("first query %q", q)
	}
	if got, err := New().Search(context.Background(), web, metadata.Query{Title: " "}); got != nil || err != nil {
		t.Errorf("no title: %+v, %v", got, err)
	}
}

// byQuery answers each request by its query, an empty result for the rest.
type byQuery struct {
	answers map[string][]byte
	asked   []string
}

func (b *byQuery) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	q := u.Query().Get("query")
	b.asked = append(b.asked, q)
	if a, ok := b.answers[q]; ok {
		return a, nil
	}
	return sru(), nil
}

func TestSearchAsksAgainWhenNothingIsFound(t *testing.T) {
	q := metadata.Query{Title: "Effi Briest: Roman (Beispielreihe 3)", Authors: []string{"Fontane, Theodor"}}
	rec := sru(marcRecord(field("245", "0", "a", "Effi Briest")))
	cases := []struct {
		name   string
		answer string
		asked  []string
	}{
		{"short title", `tit="Effi Briest" and per="Theodor Fontane"`, []string{
			`tit="Effi Briest: Roman (Beispielreihe 3)" and per="Theodor Fontane"`,
			`tit="Effi Briest" and per="Theodor Fontane"`}},
		{"last name", `tit="Effi Briest" and per="Fontane"`, []string{
			`tit="Effi Briest: Roman (Beispielreihe 3)" and per="Theodor Fontane"`,
			`tit="Effi Briest" and per="Theodor Fontane"`,
			`tit="Effi Briest" and per="Fontane"`}},
		{"words", `woe="Effi" and woe="Briest" and woe="Fontane"`, []string{
			`tit="Effi Briest: Roman (Beispielreihe 3)" and per="Theodor Fontane"`,
			`tit="Effi Briest" and per="Theodor Fontane"`,
			`tit="Effi Briest" and per="Fontane"`,
			`woe="Effi" and woe="Briest" and woe="Fontane"`}},
	}
	for _, c := range cases {
		web := &byQuery{answers: map[string][]byte{c.answer: rec}}
		got, err := New().Search(context.Background(), web, q)
		if err != nil || len(got) != 1 || !slices.Equal(web.asked, c.asked) {
			t.Errorf("%s: %+v, %v, asked %q", c.name, got, err, web.asked)
		}
	}
	web := &byQuery{answers: map[string][]byte{`tit="Effi Briest: Roman (Beispielreihe 3)" and per="Theodor Fontane"`: rec}}
	if _, err := New().Search(context.Background(), web, q); err != nil || len(web.asked) != 1 {
		t.Errorf("a found book is asked again: %q, %v", web.asked, err)
	}
	web = &byQuery{}
	if _, err := New().Search(context.Background(), web, metadata.Query{Title: "Effi Briest", Authors: []string{"Fontane"}}); err != nil || len(web.asked) != 2 {
		t.Errorf("a plain title repeats a question: %q", web.asked)
	}
}

func TestShortTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Effi Briest":                   "Effi Briest",
		"Effi Briest : Roman":           "Effi Briest",
		"Effi Briest: Roman":            "Effi Briest",
		"Effi Briest - Roman":           "Effi Briest",
		"Effi Briest (Beispielreihe 3)": "Effi Briest",
		"Effi Briest [Neuausgabe]":      "Effi Briest",
		"Beispielreihe, Band 3":         "Beispielreihe",
		"Kapitel 451":                   "Kapitel 451",
		": Roman":                       ": Roman",
	} {
		if got := shortTitle(in); got != want {
			t.Errorf("shortTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLookupOfWhatTheDNBDoesNotKnow(t *testing.T) {
	_, err := New().Lookup(context.Background(), &canned{answer: sru()}, effiISBN)
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("an unknown ISBN: %v", err)
	}
	// Another book that merely mentions the number is not the book.
	other := marcRecord(field("245", "0", "a", "Anderes") + field("020", " ", "a", "9783150000014"))
	_, err = New().Lookup(context.Background(), &canned{answer: sru(other)}, effiISBN)
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("a record of another ISBN: %v", err)
	}
	if got, err := New().Lookup(context.Background(), &canned{}, catalog.Identifier{Type: catalog.IDDOI, Value: "10.1/x"}); got != nil || err != nil {
		t.Errorf("a DOI: %+v, %v", got, err)
	}
}

func TestADiagnosticIsAnError(t *testing.T) {
	answer := []byte(`<searchRetrieveResponse xmlns="http://www.loc.gov/zing/srw/"><diagnostics>` +
		`<diagnostic xmlns="http://www.loc.gov/zing/srw/diagnostic/"><uri>info:srw/diagnostic/1/10</uri><message>Query syntax error</message></diagnostic>` +
		`</diagnostics></searchRetrieveResponse>`)
	if _, err := New().Search(context.Background(), &canned{answer: answer}, metadata.Query{Title: "x"}); err == nil {
		t.Error("a diagnostic went unnoticed")
	}
}

func TestByAuthorListsTheirNewBooksNewestFirst(t *testing.T) {
	book := func(title, date string) string {
		return marcRecord(field("100", "1", "a", "Fontane, Theodor", "4", "aut") + field("245", "0", "a", title) + field("263", " ", "a", date))
	}
	// The last is a book Fontane only wrote a foreword to.
	foreword := marcRecord(field("100", "1", "a", "Muster, Maria", "4", "aut") + field("700", "1", "a", "Fontane, Theodor", "4", "wvr") +
		field("245", "0", "a", "Anderes") + field("263", " ", "a", "202701"))
	web := &canned{answer: sru(book("Alt", "202511"), book("Neu", "202703"), foreword)}
	p := &Provider{now: func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) }}
	got, err := p.ByAuthor(context.Background(), web, "Theodor Fontane")
	if err != nil || len(got) != 2 || got[0].Title != "Neu" || got[1].Title != "Alt" {
		t.Fatalf("ByAuthor: %+v, %v", got, err)
	}
	if q := web.cql(t); q != `per="Theodor Fontane" and jhr>=2025` {
		t.Errorf("query %q", q)
	}
	if !web.asked[0].NoCache {
		t.Error("the list of an author is kept")
	}
}

func TestBySeriesKeepsTheBooksOfThatSeries(t *testing.T) {
	book := func(title, series, v string) string {
		return marcRecord(field("245", "0", "a", title) + field("830", " ", "a", series, "v", v) + field("264", "1", "c", "2026"))
	}
	web := &canned{answer: sru(book("Eins", "Beispielreihe", "1"), book("Zwei", "Andere Reihe", "1"))}
	got, err := New().BySeries(context.Background(), web, "Beispielreihe")
	if err != nil || len(got) != 1 || got[0].Title != "Eins" {
		t.Fatalf("BySeries: %+v, %v", got, err)
	}
	if q := web.cql(t); q != `tit="Beispielreihe"` {
		t.Errorf("query %q", q)
	}
}

// web replays the answers in testdata, or records them with
// GOTOME_RECORD_FIXTURES=1 (make record-fixtures).
func web() metadata.Web { return metadata.NewFixtures("testdata", New()) }

// recorded skips a test whose request has no recorded answer yet; the
// error names the request that make record-fixtures records.
func recorded(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, metadata.ErrNoFixture) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchAnswersAsRecorded(t *testing.T) {
	got, err := New().Search(context.Background(), web(), metadata.Query{Title: "Effi Briest", Authors: []string{"Theodor Fontane"}})
	recorded(t, err)
	if i := slices.IndexFunc(got, func(r metadata.Record) bool { return r.Title == "Effi Briest" }); i < 0 {
		t.Fatalf("the book is not among %d results", len(got))
	} else if got[i].Language != "de" || len(got[i].Identifiers) == 0 || got[i].Contributors[0].Name != "Theodor Fontane" {
		t.Errorf("record = %+v", got[i])
	}
}

func TestSearchAsksAgainAsRecorded(t *testing.T) {
	got, err := New().Search(context.Background(), web(), metadata.Query{Title: "Der Stechlin - Roman in einem Band (Beispielreihe 2)", Authors: []string{"Fontane, Theodor"}})
	recorded(t, err)
	if !slices.ContainsFunc(got, func(r metadata.Record) bool { return strings.Contains(r.Title, "Stechlin") }) {
		t.Fatalf("the book is not among %d results", len(got))
	}
}

func TestByAuthorAnswersAsRecorded(t *testing.T) {
	p := &Provider{now: func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) }}
	got, err := p.ByAuthor(context.Background(), web(), "Theodor Fontane")
	recorded(t, err)
	if len(got) == 0 {
		t.Fatal("no books")
	}
	for _, r := range got {
		if !slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool { return metadata.SameName(c.Name, "Theodor Fontane") }) {
			t.Errorf("not Fontane's: %+v", r)
		}
	}
}

// TestLiveContract asks the DNB itself whether it still answers as the
// fixtures say.
func TestLiveContract(t *testing.T) {
	if os.Getenv(liveVariable) == "" {
		t.Skip("set " + liveVariable + "=1 to ask the live API")
	}
	p := New()
	got, err := p.Search(context.Background(), metadata.LiveWeb(p), metadata.Query{Title: "Effi Briest", Authors: []string{"Theodor Fontane"}})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	i := slices.IndexFunc(got, func(r metadata.Record) bool { return r.Title == "Effi Briest" && r.Language == "de" })
	if i < 0 {
		t.Fatalf("Effi Briest is not among %d results", len(got))
	}

	var isbn catalog.Identifier
	for _, r := range got {
		if j := slices.IndexFunc(r.Identifiers, func(id catalog.Identifier) bool { return id.Type == catalog.IDISBN }); j >= 0 {
			isbn = r.Identifiers[j]
			break
		}
	}
	if isbn.Value == "" {
		t.Fatal("no edition of the search names an ISBN")
	}
	found, err := p.Lookup(context.Background(), metadata.LiveWeb(p), isbn)
	if err != nil || len(found) == 0 || !slices.Contains(found[0].Identifiers, isbn) {
		t.Errorf("Lookup(%s): %+v, %v", isbn.Value, found, err)
	}

	const series = "Theodor Fontane Trilogie"
	books, err := p.BySeries(context.Background(), metadata.LiveWeb(p), series)
	if err != nil || len(books) == 0 {
		t.Errorf("BySeries(%q): %+v, %v", series, books, err)
	}
}

func TestTheMediumIsReadFromTheCarrier(t *testing.T) {
	for _, c := range []struct{ carrier, media, want string }{
		{"Online-Ressource", "Computermedien", metadata.MediumEbook},
		{"Band", "ohne Hilfsmittel zu benutzen", metadata.MediumPrint},
		{"Audiodisk", "audio", metadata.MediumAudio},
		{"", "", ""},
	} {
		if got := medium(c.carrier, c.media); got != c.want {
			t.Errorf("medium(%q, %q) = %q, want %q", c.carrier, c.media, got, c.want)
		}
	}
}
