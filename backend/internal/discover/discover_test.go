package discover

import (
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

func record(title string, authors ...string) metadata.Record {
	r := metadata.Record{Provider: "fake", ID: title, Title: title}
	for _, a := range authors {
		r.Contributors = append(r.Contributors, catalog.NewContributor{Name: a, Role: catalog.RoleAuthor})
	}
	return r
}

func volume(title string, index float64) metadata.Record {
	r := record(title, "Tomas Reed")
	r.Series, r.SeriesIndex = "Lantern Cycle", &index
	return r
}

func TestSignalsComeBeforeATasteThatIsOnlyAGuess(t *testing.T) {
	five, three := ask{kind: kindAuthor, weight: 1}, ask{kind: kindAuthor, weight: 0.3}
	genre := ask{kind: kindGenre, share: 1}
	cosine := func(f float64) *float64 { return &f }
	next := score(signalOf(ask{kind: kindSeries}, ReasonSeriesNext), cosine(0.3))
	liked := score(signalOf(five, ReasonAuthor), cosine(0.3))
	middling := score(signalOf(three, ReasonAuthor), cosine(0.3))
	taste := score(signalOf(genre, ReasonGenre), cosine(1))
	if !(next > liked && liked > middling) {
		t.Errorf("next volume %.2f, 5 star author %.2f, 3 star author %.2f", next, liked, middling)
	}
	if score(0.5, cosine(-0.7)) != 0.5 || score(0.5, nil) != 0.5 {
		t.Error("a cosine under zero, or none, adds to the signal")
	}
	if taste <= score(signalOf(genre, ReasonGenre), cosine(0.2)) {
		t.Errorf("a nearer book scores %.2f, a farther one %.2f", taste, score(signalOf(genre, ReasonGenre), cosine(0.2)))
	}
	if signalOf(ask{}, ReasonSeriesNext) <= signalOf(ask{}, ReasonSeriesGap) || signalOf(ask{}, ReasonSeriesGap) <= signalOf(ask{}, ReasonSeriesMore) {
		t.Error("next, gap and later volumes are not in that order")
	}
}

func TestTheNextVolumeTheGapsAndAFewMoreAreOffered(t *testing.T) {
	a := ask{kind: kindSeries, key: catalog.Key("Lantern Cycle"), lastRead: 3, held: []float64{1, 3}}
	got := seriesBooks(a, []metadata.Record{
		volume("One", 1), volume("Two", 2), volume("Three", 3), volume("Four", 4), volume("Five", 5), volume("Six", 6), volume("Seven", 7),
		record("No place"), func() metadata.Record { r := volume("Elsewhere", 4); r.Series = "Another Cycle"; return r }(),
	})
	reasons := map[string]string{}
	for _, f := range got {
		reasons[f.rec.Title] = f.reason
	}
	want := map[string]string{"Two": ReasonSeriesGap, "Four": ReasonSeriesNext, "Five": ReasonSeriesMore, "Six": ReasonSeriesMore}
	if len(reasons) != len(want) {
		t.Fatalf("offered %v", reasons)
	}
	for title, reason := range want {
		if reasons[title] != reason {
			t.Errorf("%s: %q, want %q", title, reasons[title], reason)
		}
	}
}

func TestACandidateIsKnownByItsTitlesAuthorsAndIsbns(t *testing.T) {
	r := record("The Harbour Lamp: A Novel", "Moss, Ada")
	r.Identifiers = []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780306406157"}, {Type: catalog.IDOpenLibrary, Value: "OL1W"}}
	k := keysOf(r)
	if !slices.Contains(k.titles, "the harbour lamp a novel") || !slices.Contains(k.titles, "the harbour lamp") {
		t.Errorf("titles %v", k.titles)
	}
	for _, want := range []string{"moss ada", "ada moss"} {
		if !slices.Contains(k.authors, want) {
			t.Errorf("authors %v lack %q", k.authors, want)
		}
	}
	if !slices.Equal(k.isbns, []string{"9780306406157"}) {
		t.Errorf("isbns %v", k.isbns)
	}
	for _, key := range append(append([]string{}, k.titles...), k.authors...) {
		if strings.Contains(key, "|") {
			t.Errorf("key %q holds the separator", key)
		}
	}
}

func TestEditionsOfOneWorkAreOneBook(t *testing.T) {
	a := record("Fresh Title", "Ada Moss")
	a.Work = "w1"
	b := record("Fresh Title (Large Print)", "Moss, Ada")
	b.Work = "w1"
	c := record("Fresh Title", "Pia Quill")
	d := record("Another", "Ada Moss")
	if !sameBook(a, b) {
		t.Error("the editions of one work are two books")
	}
	if sameBook(a, c) || sameBook(a, d) {
		t.Error("a book by another author, or another book, is the same")
	}
	if got := withoutDuplicates([]found{{rec: a}, {rec: b}, {rec: c}, {rec: d}}); len(got) != 3 {
		t.Errorf("%d books remain", len(got))
	}
}

func TestAGenreIsAskedAsTheWordsAProviderFilesItUnder(t *testing.T) {
	for key, want := range map[string]string{
		"crime": "detective and mystery stories", "fantasy": "fantasy", "science-fiction": "science fiction", "biography": "biography",
		"politics": "politics and society",
	} {
		if got := subjectOf(key); got != want && key != "politics" {
			t.Errorf("subjectOf(%q) = %q, want %q", key, got, want)
		}
	}
	if subjectOf("no such genre") != "" {
		t.Error("an unknown genre has a subject")
	}
}

func TestADescriptionIsBoundedAndHoldsWhatMakesABook(t *testing.T) {
	r := record("The Harbour Lamp", "Ada Moss")
	r.Series, r.Tags, r.Description = "Lantern Cycle", []string{"Sea stories"}, strings.Repeat("word ", 400)
	text := describe(r)
	for _, want := range []string{"The Harbour Lamp", "Ada Moss", "Lantern Cycle", "Sea stories"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q lacks %q", text, want)
		}
	}
	if len([]rune(text)) > 700 {
		t.Errorf("%d characters", len([]rune(text)))
	}
}
