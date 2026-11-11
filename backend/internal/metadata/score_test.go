package metadata

import (
	"math"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func ptr[T any](v T) *T { return &v }

// buddenbrooks is a book with every signal known, and a record that agrees
// with it in all of them but what a test changes.
func buddenbrooks() (catalog.Book, Record) {
	book := catalog.Book{
		Title: "Buddenbrooks", Language: "de", Publisher: "Beispielverlag",
		Series: "Beispielreihe", SeriesIndex: ptr(3.0), PageCount: ptr(int32(600)),
		Contributors: []catalog.Contributor{{Name: "Thomas Mann", Role: catalog.RoleAuthor}},
	}
	r := Record{
		Title: "Buddenbrooks", Language: "de", Publisher: "Beispielverlag",
		Series: "Beispielreihe", SeriesIndex: ptr(3.0), PageCount: ptr(int32(600)),
		Contributors: []catalog.NewContributor{{Name: "Thomas Mann", Role: catalog.RoleAuthor}},
	}
	return book, r
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s: %.4f, want %.4f", name, got, want)
	}
}

// bare is the same pair with the further signals unknown, so that a test
// sees what one of them adds.
func bare() (catalog.Book, Record) {
	book, r := buddenbrooks()
	book.Publisher, book.Series, book.SeriesIndex, book.PageCount = "", "", nil, nil
	r.Publisher, r.Series, r.SeriesIndex, r.PageCount = "", "", nil, nil
	return book, r
}

func TestLanguageOfAnotherCostsMuch(t *testing.T) {
	book, r := bare()
	same := Score(book, r)
	r.Language = "en"
	other := Score(book, r)
	if same < 0.9 || other > 0.65 || other >= same {
		t.Errorf("same language %.2f, other %.2f", same, other)
	}
	r.Language = "de-AT"
	near(t, "a variety of the language", Score(book, r), same)
	r.Language = ""
	if got := Score(book, r); got > same || got < other {
		t.Errorf("unknown language %.2f", got)
	}
	book.Language = ""
	r.Language = "en"
	if got := Score(book, r); got < 0.9 {
		t.Errorf("a book of no language: %.2f", got)
	}
}

func TestYearsSettleEditions(t *testing.T) {
	book, r := bare()
	book.PublishedOn, book.PublishedPrecision = ptr(mustDate(1901)), catalog.PrecisionYear
	r.Published = "1901"
	exact := Score(book, r)
	r.Published = "1902"
	oneOff := Score(book, r)
	r.Published = "1950"
	far := Score(book, r)
	r.Published = ""
	unknown := Score(book, r)
	if !(exact > oneOff && oneOff == unknown && unknown > far) {
		t.Errorf("exact %.3f, one off %.3f, unknown %.3f, far %.3f", exact, oneOff, unknown, far)
	}
}

func TestPageCountWithinAMargin(t *testing.T) {
	book, r := bare()
	none := Score(book, r)
	book.PageCount = ptr(int32(600))
	for _, c := range []struct {
		pages int32
		delta float64
	}{{600, pagesNear}, {640, pagesNear}, {541, pagesNear}, {500, 0}, {300, pagesFar}, {2000, pagesFar}} {
		r.PageCount = ptr(c.pages)
		near(t, "pages", Score(book, r)-none, c.delta)
	}
	r.PageCount = nil
	near(t, "one side silent", Score(book, r), none)
	book.PageCount, r.PageCount = ptr(int32(40)), ptr(int32(52))
	near(t, "short books use the least margin", Score(book, r)-none, pagesNear)
}

func TestPublisher(t *testing.T) {
	book, r := bare()
	none := Score(book, r)
	book.Publisher = "Penguin"
	for _, c := range []struct {
		publisher string
		delta     float64
	}{{"Penguin", publisherSame}, {"Penguin Books", publisherSame}, {"PENGUIN", publisherSame}, {"Reclam", publisherDiff}, {"", 0}} {
		r.Publisher = c.publisher
		near(t, "publisher "+c.publisher, Score(book, r)-none, c.delta)
	}
	book.Publisher, r.Publisher = "Penguin", "Penguinhaus"
	near(t, "a word is not a part of a word", Score(book, r)-none, publisherDiff)
}

func TestSeriesAndPosition(t *testing.T) {
	book, r := bare()
	none := Score(book, r)
	book.Series, book.SeriesIndex = "Beispielreihe", ptr(3.0)

	r.Series = "Beispielreihe"
	near(t, "series without a position", Score(book, r)-none, seriesSame)
	r.SeriesIndex = ptr(3.0)
	near(t, "same place", Score(book, r)-none, seriesSame+positionSame)
	r.SeriesIndex = ptr(4.0)
	near(t, "another place", Score(book, r)-none, seriesSame+positionDiff)
	r.Series = "Andere Reihe"
	near(t, "another series, its place says nothing", Score(book, r)-none, seriesDiff)
	r.Series, r.SeriesIndex = "", nil
	near(t, "the record has none", Score(book, r), none)
}

func TestSilenceOnOneSideChangesNothing(t *testing.T) {
	book, r := buddenbrooks()
	full := Score(book, r)
	bookBare, rBare := bare()
	// Nothing known beyond the old four signals: the score is the old one.
	near(t, "full agreement is capped", full, bestWithoutIdentifier)
	if got := Score(bookBare, rBare); got < 0.9 || got > bestWithoutIdentifier {
		t.Errorf("bare agreement %.3f", got)
	}
	book.Publisher, book.Series, book.SeriesIndex, book.PageCount = "", "", nil, nil
	near(t, "the book silent on all of them", Score(book, r), Score(bookBare, rBare))
}

func TestAnEditionOfAnotherPlaceCannotBeTakenAsIs(t *testing.T) {
	book, r := buddenbrooks()
	r.SeriesIndex = ptr(4.0)
	if got := Score(book, r); got >= 0.95 {
		t.Errorf("the next volume scores %.3f, over the threshold", got)
	}
	book, r = buddenbrooks()
	r.Language = "en"
	if got := Score(book, r); got >= 0.95 {
		t.Errorf("a translation scores %.3f, over the threshold", got)
	}
}

func TestCandidatesOfTheBooksLanguageComeFirstAmongEqualScores(t *testing.T) {
	c := []Candidate{
		{Record: Record{ID: "en", Language: "en"}, Score: 0.8},
		{Record: Record{ID: "none"}, Score: 0.8},
		{Record: Record{ID: "de-at", Language: "de-AT"}, Score: 0.8},
		{Record: Record{ID: "de", Language: "de"}, Score: 0.8},
		{Record: Record{ID: "better", Language: "en"}, Score: 0.9},
	}
	got := sortCandidates(c, "de-DE")
	var ids []string
	for _, x := range got {
		ids = append(ids, x.ID)
	}
	want := []string{"better", "de-at", "de", "en", "none"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("order %v, want %v", ids, want)
		}
	}
}

func mustDate(year int) (d time.Time) { return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC) }

func TestSameBookIsTheTitleWithAnAuthor(t *testing.T) {
	book, r := bare()
	if !SameBook(book, r) {
		t.Fatal("the same title and author is not the same book")
	}
	r.Title = "Buddenbrooks Verfall einer Familie"
	if SameBook(book, r) {
		t.Error("a longer title was taken for the same book")
	}
	r.Title, r.Subtitle = "Buddenbrooks", "Verfall einer Familie"
	if !SameBook(book, r) {
		t.Error("the record's subtitle made it another book")
	}
	r.Subtitle = ""
	r.Contributors = []catalog.NewContributor{{Name: "Heinrich Mann", Role: catalog.RoleAuthor}}
	if SameBook(book, r) {
		t.Error("another author's book of the title was taken for the same book")
	}
	r.Contributors = []catalog.NewContributor{{Name: "Thomas Mann", Role: catalog.RoleTranslator}}
	if SameBook(book, r) {
		t.Error("a translator was taken for the author")
	}
}

func TestSameEditionNeedsThePublisherAndANearYear(t *testing.T) {
	book, r := buddenbrooks()
	if !SameEdition(book, r) {
		t.Fatal("the same title, author and publisher is not the same edition")
	}
	r.Publisher = "Anderer Verlag"
	if SameEdition(book, r) {
		t.Error("another publisher's edition was taken")
	}
	r.Publisher = ""
	if SameEdition(book, r) {
		t.Error("a record without a publisher was taken")
	}
	r.Publisher = "Beispielverlag"
	on := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	book.PublishedOn, book.PublishedPrecision = &on, "year"
	r.Published = "2002"
	if !SameEdition(book, r) {
		t.Error("a year apart was taken for another edition")
	}
	r.Published = "2010"
	if SameEdition(book, r) {
		t.Error("an edition nine years later was taken")
	}
}

func TestTheEbookOfAnEditionBeatsItsPrintedTwin(t *testing.T) {
	book, r := bare()
	book.Files = []catalog.File{{Kind: catalog.KindEbook}}
	r.Medium = MediumEbook
	ebook := Score(book, r)
	r.Medium = MediumPrint
	printed := Score(book, r)
	r.Medium = ""
	unknown := Score(book, r)
	if !(ebook > unknown && unknown > printed) {
		t.Errorf("ebook %.3f, unknown %.3f, printed %.3f", ebook, unknown, printed)
	}
	book.Files = append(book.Files, catalog.File{Kind: catalog.KindAudio})
	r.Medium = MediumPrint
	near(t, "a book of both kinds", Score(book, r), unknown)
}
