package crossref

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

var (
	eslDOI  = catalog.Identifier{Type: catalog.IDDOI, Value: "10.1007/978-0-387-84858-7"}
	eslISBN = catalog.Identifier{Type: catalog.IDISBN, Value: "9780387848570"}
)

// web replays the answers in testdata, or records them with
// GOTOME_RECORD_FIXTURES=1 (make record-fixtures).
func web() metadata.Web { return metadata.NewFixtures("testdata", New(nil)) }

// checkESL holds a record to what CrossRef says of The Elements of
// Statistical Learning, a book in a series.
func checkESL(t *testing.T, r metadata.Record) {
	t.Helper()
	if r.Title != "The Elements of Statistical Learning" || len(r.Contributors) == 0 ||
		r.Contributors[0].Name != "Trevor Hastie" || r.Contributors[0].Role != catalog.RoleAuthor {
		t.Errorf("record = %+v", r)
	}
	if !slices.Contains(r.Identifiers, eslDOI) || !slices.Contains(r.Identifiers, eslISBN) || r.ID != eslDOI.Value {
		t.Errorf("identifiers %v, ID %q", r.Identifiers, r.ID)
	}
	if r.Publisher == "" || r.Published != "2009" || r.Series != "Springer Series in Statistics" {
		t.Errorf("publisher %q, published %q, series %q", r.Publisher, r.Published, r.Series)
	}
}

func TestLookupByDOI(t *testing.T) {
	got, err := New(nil).Lookup(context.Background(), web(), eslDOI)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkESL(t, got[0])
	book := catalog.Book{Title: "Elements", Identifiers: []catalog.BookIdentifier{{Identifier: eslDOI}}}
	if score := metadata.Score(book, got[0]); score != 1 {
		t.Errorf("score %.2f, want 1 for the same DOI", score)
	}
}

func TestLookupOfAPaperByItsDOI(t *testing.T) {
	got, err := New(nil).Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDDOI, Value: "10.1038/nature14539"})
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	r := got[0]
	if r.Title != "Deep learning" || len(r.Contributors) < 3 || r.Contributors[0].Name != "Yann LeCun" {
		t.Errorf("record = %+v", r)
	}
	// A journal is no series of books.
	if r.Series != "" || r.Published == "" {
		t.Errorf("series %q, published %q", r.Series, r.Published)
	}
}

func TestLookupByISBN(t *testing.T) {
	got, err := New(nil).Lookup(context.Background(), web(), eslISBN)
	if err != nil || len(got) == 0 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkESL(t, got[0])
}

func TestLookupOfWhatCrossRefDoesNotKnow(t *testing.T) {
	_, err := New(nil).Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDDOI, Value: "10.9999/no-such-work-in-gotome"})
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("an unknown DOI: %v", err)
	}
	_, err = New(nil).Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDISBN, Value: "9799999999990"})
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("an unknown ISBN: %v", err)
	}
	if got, err := New(nil).Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDASIN, Value: "B000"}); got != nil || err != nil {
		t.Errorf("an ASIN: %+v, %v", got, err)
	}
}

func TestSearchByTitleAndAuthor(t *testing.T) {
	got, err := New(nil).Search(context.Background(), web(), metadata.Query{
		Title: "The Elements of Statistical Learning", Authors: []string{"Trevor Hastie"},
	})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	i := slices.IndexFunc(got, func(r metadata.Record) bool { return slices.Contains(r.Identifiers, eslDOI) })
	if i < 0 {
		t.Fatalf("the book is not among %d results", len(got))
	}
	checkESL(t, got[i])
	if got, err := New(nil).Search(context.Background(), web(), metadata.Query{Title: " "}); got != nil || err != nil {
		t.Errorf("no title: %+v, %v", got, err)
	}
}

// asked is a Web that keeps the requests and answers each with nothing.
type asked []metadata.Request

func (a *asked) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	*a = append(*a, r)
	return []byte(`{"message":{"items":[]}}`), nil
}

func TestRequestsCarryTheContactAddressButNeverKeepIt(t *testing.T) {
	var got asked
	p := New(func(context.Context) string { return "library@example.org" })
	if _, err := p.Search(context.Background(), &got, metadata.Query{Title: "Emma"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SecretQuery.Get("mailto") != "library@example.org" {
		t.Fatalf("requests %+v", got)
	}
	// The URL is the cache key and the fixture's name.
	if u := got[0].URL; strings.Contains(u, "@") || strings.Contains(u, "mailto") {
		t.Errorf("the address is in the URL %q", u)
	}

	got = nil
	if _, err := New(func(context.Context) string { return "" }).Search(context.Background(), &got, metadata.Query{Title: "Emma"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SecretQuery != nil {
		t.Errorf("without an address: %+v", got)
	}
}

// TestLiveContract asks CrossRef itself whether it still answers as the
// fixtures say.
func TestLiveContract(t *testing.T) {
	if os.Getenv(liveVariable) == "" {
		t.Skip("set " + liveVariable + "=1 to ask the live API")
	}
	got, err := New(nil).Lookup(context.Background(), metadata.LiveWeb(New(nil)), eslDOI)
	if err != nil || len(got) != 1 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkESL(t, got[0])
}
