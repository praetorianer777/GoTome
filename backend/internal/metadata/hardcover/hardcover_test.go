package hardcover

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// liveVariable, set to anything, runs the contract test against the live
// API. Only the nightly workflow sets it.
const liveVariable = "GOTOME_LIVE"

// tokenVariable holds the token for recording fixtures and the contract
// test. The fixtures keep no header, so replaying them needs a token that
// is only not empty.
const tokenVariable = "GOTOME_HARDCOVER_TOKEN"

func token(context.Context) string {
	if t := os.Getenv(tokenVariable); t != "" {
		return t
	}
	return "replaying"
}

func provider() *Provider { return New(token) }

// web replays the answers in testdata, or records them with
// GOTOME_RECORD_FIXTURES=1 (make record-fixtures).
func web() metadata.Web { return metadata.NewFixtures("testdata", provider()) }

// fellowship is the Mariner paperback of The Fellowship of the Ring, the
// first book of a series.
var fellowship = catalog.Identifier{Type: catalog.IDISBN, Value: "9780547928210"}

func checkFellowship(t *testing.T, r metadata.Record) {
	t.Helper()
	if !strings.Contains(r.Title, "Fellowship of the Ring") || len(r.Contributors) == 0 ||
		!strings.Contains(r.Contributors[0].Name, "Tolkien") || r.Contributors[0].Role != catalog.RoleAuthor {
		t.Errorf("record = %+v", r)
	}
	if !strings.Contains(r.Series, "Lord of the Rings") || r.SeriesIndex == nil || *r.SeriesIndex != 1 {
		t.Errorf("series %q, index %v; want The Lord of the Rings, 1", r.Series, r.SeriesIndex)
	}
}

func TestLookupByISBNNamesTheSeries(t *testing.T) {
	got, err := provider().Lookup(context.Background(), web(), fellowship)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	r := got[0]
	checkFellowship(t, r)
	if !slices.Contains(r.Tags, "Fantasy") || r.CoverURL == "" || r.Language != "en" || r.Description == "" {
		t.Errorf("tags %q, cover %q, language %q, description %.40q", r.Tags, r.CoverURL, r.Language, r.Description)
	}
	if !slices.Contains(r.Identifiers, fellowship) || r.ID == "" || r.Publisher == "" || r.Published == "" {
		t.Errorf("identifiers %v, ID %q, publisher %q, published %q", r.Identifiers, r.ID, r.Publisher, r.Published)
	}
	if score := metadata.Score(catalog.Book{Title: "Fellowship", Identifiers: []catalog.BookIdentifier{{Identifier: fellowship}}}, r); score != 1 {
		t.Errorf("score %.2f, want 1 for the same ISBN", score)
	}
}

func TestLookupOfAnUnknownISBN(t *testing.T) {
	got, err := provider().Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDISBN, Value: "9799999999990"})
	if !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestSearchByTitleAndAuthor(t *testing.T) {
	got, err := provider().Search(context.Background(), web(), metadata.Query{
		Title: "The Fellowship of the Ring", Authors: []string{"J.R.R. Tolkien"},
	})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	i := slices.IndexFunc(got, func(r metadata.Record) bool { return strings.Contains(r.Title, "Fellowship of the Ring") })
	if i < 0 {
		t.Fatalf("not among %d results", len(got))
	}
	checkFellowship(t, got[i])
}

// asked is a Web that keeps the requests and answers each as given.
type asked struct {
	requests []metadata.Request
	answer   string
}

func (a *asked) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	a.requests = append(a.requests, r)
	return []byte(a.answer), nil
}

func TestWithoutATokenNothingIsAsked(t *testing.T) {
	for _, p := range []*Provider{New(nil), New(func(context.Context) string { return " " })} {
		w := &asked{}
		got, err := p.Lookup(context.Background(), w, fellowship)
		if got != nil || err != nil {
			t.Errorf("Lookup: %+v, %v", got, err)
		}
		found, err := p.Search(context.Background(), w, metadata.Query{Title: "Emma"})
		if found != nil || err != nil {
			t.Errorf("Search: %+v, %v", found, err)
		}
		if len(w.requests) != 0 {
			t.Errorf("%d requests without a token", len(w.requests))
		}
	}
}

func TestTheTokenGoesInTheHeaderOnly(t *testing.T) {
	w := &asked{answer: `{"data":{"editions":[]}}`}
	p := New(func(context.Context) string { return "Bearer abc" })
	if _, err := p.Lookup(context.Background(), w, fellowship); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	r := w.requests[0]
	if r.Header.Get("Authorization") != "Bearer abc" || strings.Contains(r.URL+string(r.Body), "abc") {
		t.Errorf("request %+v", r)
	}
}

func TestAnErrorHardcoverAnswersIsAnError(t *testing.T) {
	w := &asked{answer: `{"errors":[{"message":"field 'nonsense' not found in type: 'editions'"}]}`}
	_, err := provider().Lookup(context.Background(), w, fellowship)
	if err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("err = %v", err)
	}
}

// TestLiveContract asks Hardcover itself whether it still answers as the
// fixtures say.
func TestLiveContract(t *testing.T) {
	if os.Getenv(liveVariable) == "" || os.Getenv(tokenVariable) == "" {
		t.Skip("set " + liveVariable + "=1 and " + tokenVariable + " to ask the live API")
	}
	got, err := provider().Lookup(context.Background(), metadata.LiveWeb(provider()), fellowship)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkFellowship(t, got[0])
}

// The works tests ask about an author and his series.
func TestTheBooksOfAnAuthor(t *testing.T) {
	got, err := provider().ByAuthor(context.Background(), web(), "Rothfuss, Patrick")
	if err != nil || len(got) == 0 {
		t.Fatalf("ByAuthor: %d records, %v", len(got), err)
	}
	dated := 0
	for _, r := range got {
		if !slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool { return metadata.SameName(c.Name, "Patrick Rothfuss") }) {
			t.Errorf("%q does not credit the author: %+v", r.Title, r.Contributors)
		}
		if r.Published != "" {
			dated++
		}
	}
	// The newest are asked for: his announced novel is among them.
	if !slices.ContainsFunc(got, func(r metadata.Record) bool { return r.Title == "The Doors of Stone" }) {
		t.Errorf("his announced novel is not among %d books", len(got))
	}
	if dated == 0 {
		t.Error("no book has a release date")
	}
	// Hardcover keeps translations as books of their own; each says its
	// language, for WorksOf to keep those asked for.
	if !slices.ContainsFunc(got, func(r metadata.Record) bool { return r.Language != "" && r.Language != "en" }) {
		t.Error("no translation says its language")
	}
}

func TestTheBooksOfASeries(t *testing.T) {
	got, err := provider().BySeries(context.Background(), web(), "The Kingkiller Chronicle")
	if err != nil || len(got) == 0 {
		t.Fatalf("BySeries: %d records, %v", len(got), err)
	}
	i := slices.IndexFunc(got, func(r metadata.Record) bool { return r.Title == "The Name of the Wind" })
	if i < 0 {
		t.Fatalf("the first book is not among %d", len(got))
	}
	if r := got[i]; r.Series != "The Kingkiller Chronicle" || r.SeriesIndex == nil || *r.SeriesIndex != 1 {
		t.Errorf("series %q, index %v", r.Series, r.SeriesIndex)
	}
}

func TestTheBooksOfNobody(t *testing.T) {
	if got, err := provider().ByAuthor(context.Background(), web(), "Qwzx Vrbnkt"); !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
		t.Errorf("an unknown author: %d records, %v", len(got), err)
	}
	if got, err := New(nil).ByAuthor(context.Background(), web(), "Patrick Rothfuss"); err != nil || got != nil {
		t.Errorf("without a token: %+v, %v", got, err)
	}
}
