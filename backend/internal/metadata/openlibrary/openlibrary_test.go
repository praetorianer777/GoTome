package openlibrary

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// liveVariable, set to anything, runs the contract test against the live
// API. Only the nightly workflow sets it.
const liveVariable = "GOTOME_LIVE"

var emmaISBN = catalog.Identifier{Type: catalog.IDISBN, Value: "9780141439587"}

func emma() catalog.Book {
	return catalog.Book{
		Title: "Emma", Language: "en",
		Contributors: []catalog.Contributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
		Identifiers:  []catalog.BookIdentifier{{Identifier: emmaISBN}},
	}
}

// web replays the answers in testdata, or records them with
// GOTOME_RECORD_FIXTURES=1 (make record-fixtures).
func web() metadata.Web { return metadata.NewFixtures("testdata", New()) }

// checkEmma holds a record of Emma to what any edition of it says.
func checkEmma(t *testing.T, r metadata.Record) {
	t.Helper()
	if r.Title != "Emma" || len(r.Contributors) == 0 || r.Contributors[0].Name != "Jane Austen" || r.Contributors[0].Role != catalog.RoleAuthor {
		t.Errorf("record = %+v", r)
	}
	if r.CoverURL == "" {
		t.Error("no cover")
	}
}

func TestLookupByISBN(t *testing.T) {
	got, err := New().Lookup(context.Background(), web(), emmaISBN)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	r := got[0]
	checkEmma(t, r)
	if !slices.Contains(r.Identifiers, emmaISBN) || r.Language != "en" || r.Published == "" || r.Publisher == "" || r.ID == "" {
		t.Errorf("record = %+v", r)
	}
	for _, tag := range r.Tags {
		if strings.ContainsAny(tag, "_:") {
			t.Errorf("machine tag %q", tag)
		}
	}
	if score := metadata.Score(emma(), r); score != 1 {
		t.Errorf("score %.2f, want 1 for the same ISBN", score)
	}
}

func TestLookupOfAnUnknownISBN(t *testing.T) {
	got, err := New().Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDISBN, Value: "9799999999990"})
	if !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
	if got, err := New().Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDASIN, Value: "B000"}); err != nil || got != nil {
		t.Errorf("an ASIN: %+v, %v", got, err)
	}
}

func TestSearchByTitleAndAuthor(t *testing.T) {
	got, err := New().Search(context.Background(), web(), metadata.Query{Title: "Emma", Authors: []string{"Jane Austen"}, Language: "en"})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	checkEmma(t, got[0])
	book := emma()
	book.Identifiers = nil
	if score := metadata.Score(book, got[0]); score < 0.9 {
		t.Errorf("the first result scores %.2f: %+v", score, got[0])
	}
	for _, r := range got {
		if slices.ContainsFunc(r.Identifiers, func(i catalog.Identifier) bool { return i.Type == catalog.IDISBN }) {
			t.Errorf("a work carries an ISBN: %+v", r)
		}
	}
}

func TestCoverIsFetchedAndStored(t *testing.T) {
	got, err := New().Lookup(context.Background(), web(), emmaISBN)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %v", err)
	}
	image, err := web().Get(context.Background(), metadata.Request{URL: got[0].CoverURL, NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	store := covers.NewStore(t.TempDir())
	key, err := store.Put(image)
	if err != nil {
		t.Fatalf("store the cover: %v", err)
	}
	if _, err := store.Path(key, covers.Small); err != nil {
		t.Error(err)
	}
}

func TestPublishDate(t *testing.T) {
	for in, want := range map[string]string{
		"2003": "2003", "December 1, 2003": "2003-12-01", "Dec 1, 2003": "2003-12-01", "1 December 2003": "2003-12-01",
		"December 2003": "2003-12", "2003-12-01": "2003-12-01", "c1996.": "1996", "[1815?]": "1815", "": "", "n.d.": "",
	} {
		if got := publishDate(in); got != want {
			t.Errorf("publishDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLanguages(t *testing.T) {
	if languageTag("eng") != "en" || languageTag("ger") != "de" || languageTag("fre") != "fr" || languageTag("??") != "" {
		t.Errorf("eng %q, ger %q, fre %q", languageTag("eng"), languageTag("ger"), languageTag("fre"))
	}
	if languageCode("en-GB") != "eng" || languageCode("de") != "ger" || languageCode("") != "" {
		t.Errorf("en-GB %q", languageCode("en-GB"))
	}
}

// TestLiveContract asks OpenLibrary itself whether it still answers as the
// fixtures say. It runs nightly, never in the gate.
func TestLiveContract(t *testing.T) {
	if os.Getenv(liveVariable) == "" {
		t.Skip("set " + liveVariable + "=1 to ask the live API")
	}
	live := metadata.LiveWeb(New())
	got, err := New().Lookup(context.Background(), live, emmaISBN)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkEmma(t, got[0])
	found, err := New().Search(context.Background(), live, metadata.Query{Title: "Emma", Authors: []string{"Jane Austen"}})
	if err != nil || len(found) == 0 {
		t.Fatalf("Search: %+v, %v", found, err)
	}
	checkEmma(t, found[0])
	if _, err := live.Get(context.Background(), metadata.Request{URL: got[0].CoverURL}); err != nil {
		t.Errorf("cover: %v", err)
	}
	subject, err := New().BySubject(context.Background(), live, "science_fiction")
	if err != nil || len(subject) == 0 {
		t.Fatalf("BySubject: %d records, %v", len(subject), err)
	}
	checkSubjectWorks(t, subject)
}

func TestTheWorksOfAnAuthor(t *testing.T) {
	got, err := New().ByAuthor(context.Background(), web(), "Austen, Jane")
	if err != nil || len(got) == 0 {
		t.Fatalf("ByAuthor: %d records, %v", len(got), err)
	}
	for _, r := range got {
		if !slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool { return metadata.SameName(c.Name, "Jane Austen") }) {
			t.Errorf("%q does not credit the author: %+v", r.Title, r.Contributors)
		}
	}
	novels := []string{"Emma", "Pride and Prejudice", "Persuasion", "Sense and Sensibility", "Mansfield Park", "Northanger Abbey"}
	i := slices.IndexFunc(got, func(r metadata.Record) bool { return slices.Contains(novels, r.Title) })
	if i < 0 {
		t.Fatalf("none of her novels is among %d works", len(got))
	}
	// For a book printed for two hundred years Open Library's date is of an
	// edition, often this year's: that it is read is what counts here.
	if len(got[i].Published) != 4 {
		t.Errorf("published %q, want a year", got[i].Published)
	}
	if got, err := New().BySeries(context.Background(), web(), "Sherlock Holmes"); err != nil || got != nil {
		t.Errorf("OpenLibrary lists a series: %+v, %v", got, err)
	}
}

func TestTheWorksOfNobody(t *testing.T) {
	if got, err := New().ByAuthor(context.Background(), web(), "Qwzx Vrbnkt"); err != nil || len(got) != 0 {
		t.Errorf("an unknown author: %+v, %v", got, err)
	}
}

func TestTheEditionNamesTheWorkTheSearchFinds(t *testing.T) {
	edition, err := New().Lookup(context.Background(), web(), emmaISBN)
	if err != nil || len(edition) != 1 || edition[0].Work == "" {
		t.Fatalf("Lookup: %+v, %v", edition, err)
	}
	found, err := New().Search(context.Background(), web(), metadata.Query{Title: "Emma", Authors: []string{"Jane Austen"}, Language: "en"})
	if err != nil || len(found) == 0 || found[0].Work != edition[0].Work {
		t.Errorf("the edition's work %q, the search's first %+v, %v", edition[0].Work, found, err)
	}
}

// checkSubjectWorks holds works listed under a subject to what any listing
// of one says.
func checkSubjectWorks(t *testing.T, got []metadata.Record) {
	t.Helper()
	for _, r := range got {
		if r.Title == "" || r.ID == "" || r.Work != r.ID {
			t.Errorf("record = %+v", r)
		}
		if slices.ContainsFunc(r.Identifiers, func(i catalog.Identifier) bool { return i.Type == catalog.IDISBN }) {
			t.Errorf("a work carries an ISBN: %+v", r)
		}
	}
	withAuthor := slices.IndexFunc(got, func(r metadata.Record) bool { return len(r.Contributors) > 0 })
	if withAuthor < 0 {
		t.Error("no work names an author")
	}
}

// TestTheWorksOfASubject replays Open Library's answer for a subject. It
// skips until the answer is recorded (make record-fixtures).
func TestTheWorksOfASubject(t *testing.T) {
	got, err := New().BySubject(context.Background(), web(), "Science Fiction")
	if errors.Is(err, metadata.ErrNoFixture) {
		t.Skip(err)
	}
	if err != nil || len(got) == 0 {
		t.Fatalf("BySubject: %d records, %v", len(got), err)
	}
	checkSubjectWorks(t, got)
}

type answers map[string]string

func (a answers) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	if body, ok := a[r.URL]; ok {
		return []byte(body), nil
	}
	return nil, metadata.ErrNotFound
}

func TestASubjectIsReadIntoRecords(t *testing.T) {
	web := answers{site + "/subjects/sea_stories.json?limit=40": `{"name": "sea stories", "works": [
		{"key": "/works/OL1W", "title": "The Harbour Lamp", "cover_id": 77, "first_publish_year": 1901,
		 "subject": ["Sea stories", "open_syllabus_project"], "authors": [{"key": "/authors/OL1A", "name": "Ada Moss"}]},
		{"key": "/works/OL2W", "title": "  ", "authors": []},
		{"key": "/works/OL3W", "title": "Nameless Shore", "authors": []}]}`}
	got, err := New().BySubject(context.Background(), web, " Sea Stories ")
	if err != nil || len(got) != 2 {
		t.Fatalf("BySubject: %+v, %v", got, err)
	}
	first := got[0]
	if first.ID != "OL1W" || first.Work != "OL1W" || first.Title != "The Harbour Lamp" || first.Published != "1901" ||
		len(first.Contributors) != 1 || first.Contributors[0].Name != "Ada Moss" || !strings.Contains(first.CoverURL, "/b/id/77-L.jpg") ||
		!slices.Equal(first.Tags, []string{"Sea stories"}) {
		t.Errorf("record = %+v", first)
	}
	if got[1].Title != "Nameless Shore" || len(got[1].Contributors) != 0 || got[1].CoverURL != "" {
		t.Errorf("record = %+v", got[1])
	}
	if got, err := New().BySubject(context.Background(), web, "a/b"); err != nil || got != nil {
		t.Errorf("a subject with a slash: %+v, %v", got, err)
	}
	if _, err := New().BySubject(context.Background(), web, "unknown"); !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("an unknown subject: %v", err)
	}
}

func TestACoverIsDerivedFromAnEditionOrAnISBNAndNotFromAWork(t *testing.T) {
	p := New()
	isbn := metadata.Record{Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780306406157"}}}
	if got := p.DeriveCover(isbn); got != "https://covers.openlibrary.org/b/isbn/9780306406157-L.jpg?default=false" {
		t.Errorf("by ISBN %q", got)
	}
	edition := metadata.Record{Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780306406157"}, {Type: catalog.IDOpenLibrary, Value: "OL7353617M"}}}
	if got := p.DeriveCover(edition); got != "https://covers.openlibrary.org/b/olid/OL7353617M-L.jpg?default=false" {
		t.Errorf("by edition %q", got)
	}
	work := metadata.Record{Identifiers: []catalog.Identifier{{Type: catalog.IDOpenLibrary, Value: "OL66513W"}}}
	if got := p.DeriveCover(work); got != "" {
		t.Errorf("a work's cover is guessed: %q", got)
	}
}

// TestMostOfAnAuthorsNewestWorksHaveNoCover holds what the recorded answer
// shows: sorted newest first, Open Library's list is mostly reprints and
// box sets that carry no cover_i, so Discover cannot rely on this listing
// for a cover.
func TestMostOfAnAuthorsNewestWorksHaveNoCover(t *testing.T) {
	got, err := New().ByAuthor(context.Background(), web(), "Austen, Jane")
	if err != nil || len(got) == 0 {
		t.Fatalf("ByAuthor: %d records, %v", len(got), err)
	}
	bare := 0
	for _, r := range got {
		if r.CoverURL == "" {
			bare++
		}
	}
	if bare*2 < len(got) {
		t.Errorf("%d of %d works have no cover; the recorded answer showed most without", bare, len(got))
	}
}
