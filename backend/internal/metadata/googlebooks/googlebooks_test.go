package googlebooks

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

const liveVariable = "GOTOME_LIVE"

var emmaISBN = catalog.Identifier{Type: catalog.IDISBN, Value: "9780141439587"}

func emma() catalog.Book {
	return catalog.Book{
		Title: "Emma", Language: "en",
		Contributors: []catalog.Contributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
		Identifiers:  []catalog.BookIdentifier{{Identifier: emmaISBN}},
	}
}

// keyVariable holds a key for recording fixtures and for the contract test,
// for when the anonymous quota is used up. Replaying needs none: the key is
// part of neither a fixture nor its name.
const keyVariable = "GOTOME_GOOGLE_BOOKS_KEY"

func provider() *Provider {
	if key := os.Getenv(keyVariable); key != "" {
		return New(secrets{settings.GoogleBooksKey: key})
	}
	return New(nil)
}

// web replays the answers in testdata.
func web() metadata.Web { return metadata.NewFixtures("testdata", provider()) }

func checkEmma(t *testing.T, r metadata.Record) {
	t.Helper()
	if !strings.HasPrefix(r.Title, "Emma") || len(r.Contributors) == 0 || r.Contributors[0].Name != "Jane Austen" {
		t.Errorf("record = %+v", r)
	}
	if !strings.HasPrefix(r.CoverURL, "https://") || strings.Contains(r.CoverURL, "edge=") {
		t.Errorf("cover %q", r.CoverURL)
	}
}

func TestLookupByISBN(t *testing.T) {
	got, err := provider().Lookup(context.Background(), web(), emmaISBN)
	if err != nil || len(got) == 0 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	r := got[0]
	checkEmma(t, r)
	if !slices.Contains(r.Identifiers, emmaISBN) || r.ID == "" || r.Published == "" {
		t.Errorf("record = %+v", r)
	}
	if strings.ContainsAny(r.Description, "<>") {
		t.Errorf("the description has markup: %q", r.Description)
	}
	if score := metadata.Score(emma(), r); score != 1 {
		t.Errorf("score %.2f", score)
	}
}

func TestLookupOfAnUnknownISBN(t *testing.T) {
	got, err := provider().Lookup(context.Background(), web(), catalog.Identifier{Type: catalog.IDISBN, Value: "9799999999990"})
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestSearchByTitleAndAuthor(t *testing.T) {
	got, err := provider().Search(context.Background(), web(), metadata.Query{Title: "Emma", Authors: []string{"Jane Austen"}, Language: "en-GB"})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %+v, %v", got, err)
	}
	checkEmma(t, got[0])
	book := emma()
	book.Identifiers = nil
	if score := metadata.Score(book, got[0]); score < 0.9 {
		t.Errorf("the first result scores %.2f: %+v", score, got[0])
	}
}

type secrets map[string]string

func (s secrets) Secret(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

// askingWeb keeps the requests and answers with nothing found.
type askingWeb struct{ asked []metadata.Request }

func (w *askingWeb) Get(_ context.Context, r metadata.Request) ([]byte, error) {
	w.asked = append(w.asked, r)
	return []byte(`{"totalItems":0}`), nil
}

func TestTheKeyIsSentOnlyWhenSet(t *testing.T) {
	for name, s := range map[string]Secrets{"no store": nil, "no key": secrets{}, "a key": secrets{settings.GoogleBooksKey: "the-key"}} {
		w := &askingWeb{}
		if _, err := New(s).Search(context.Background(), w, metadata.Query{Title: "Emma"}); err != nil {
			t.Fatal(err)
		}
		r := w.asked[0]
		if got, want := r.SecretQuery.Get("key"), map[string]string{"a key": "the-key"}[name]; got != want {
			t.Errorf("%s: key %q, want %q", name, got, want)
		}
		if strings.Contains(r.URL, "the-key") || r.Busy == nil {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestQuotaUsedUp(t *testing.T) {
	quota := []byte(`{"error":{"code":403,"errors":[{"domain":"usageLimits","reason":"dailyLimitExceeded"}]}}`)
	denied := []byte(`{"error":{"code":403,"errors":[{"domain":"global","reason":"forbidden"}]}}`)
	if !quotaUsedUp(http.StatusForbidden, quota) || quotaUsedUp(http.StatusForbidden, denied) || quotaUsedUp(http.StatusOK, quota) {
		t.Error("quotaUsedUp mistakes one answer for another")
	}
}

func TestARefusalSaysWhy(t *testing.T) {
	disabled := []byte(`{"error":{"code":403,"message":"Books API has not been used in project 1 before or it is disabled.","status":"PERMISSION_DENIED"}}`)
	if got := refusal(disabled); got != "Books API has not been used in project 1 before or it is disabled." {
		t.Errorf("refusal = %q", got)
	}
	if got := refusal([]byte("<html>Forbidden</html>")); got != "" {
		t.Errorf("not JSON: %q", got)
	}
	long := refusal([]byte(`{"error":{"message":"` + strings.Repeat("ä", maxReason) + `"}}`))
	if !utf8.ValidString(long) || len(long) > maxReason+len("…") {
		t.Errorf("a long message: %d bytes", len(long))
	}
}

func TestDatesAndCovers(t *testing.T) {
	for in, want := range map[string]string{"2003": "2003", "2003-05": "2003-05", "2003-05-01": "2003-05-01", "2003-05-01T00:00:00Z": "2003-05-01", "200?": "", "": ""} {
		if got := publishedDate(in); got != want {
			t.Errorf("publishedDate(%q) = %q, want %q", in, got, want)
		}
	}
	got := coverURL("http://books.google.com/books/content?id=abc&printsec=frontcover&img=1&zoom=1&edge=curl&source=gbs_api")
	if got != "https://books.google.com/books/content?id=abc&img=1&printsec=frontcover&source=gbs_api&zoom=1" {
		t.Errorf("coverURL = %q", got)
	}
}

// TestLiveContract asks Google Books itself, with the key in keyVariable if
// there is one. It runs nightly, never in the gate.
func TestLiveContract(t *testing.T) {
	if os.Getenv(liveVariable) == "" {
		t.Skip("set " + liveVariable + "=1 to ask the live API")
	}
	live := metadata.LiveWeb(provider())
	got, err := provider().Lookup(context.Background(), live, emmaISBN)
	if errors.Is(err, metadata.ErrBusy) && os.Getenv(keyVariable) == "" {
		// Google's anonymous quota is shared by everyone behind one address,
		// which on a hosted runner is many.
		t.Skip("the anonymous quota is used up; set " + keyVariable + " to test with a key")
	}
	if err != nil || len(got) == 0 {
		t.Fatalf("Lookup: %+v, %v", got, err)
	}
	checkEmma(t, got[0])
	found, err := provider().Search(context.Background(), live, metadata.Query{Title: "Emma", Authors: []string{"Jane Austen"}})
	if err != nil || len(found) == 0 {
		t.Fatalf("Search: %+v, %v", found, err)
	}
}
