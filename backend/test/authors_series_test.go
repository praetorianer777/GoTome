//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// inSeries puts the book at the place in the series, as an edit would.
func (a *app) inSeries(admin *http.Client, book uuid.UUID, series string, index float64) {
	a.t.Helper()
	status, body, _ := a.call(admin, http.MethodPatch, "/books/"+book.String(),
		map[string]any{"series": map[string]any{"name": series, "index": index}})
	if status != http.StatusOK {
		a.t.Fatalf("placing a book in a series: %d %v", status, body)
	}
}

// titlesOf lists every page of the books the rule leaves in the order, each
// page of the size.
func (a *app) titlesOf(c *http.Client, rule, sort string, size int) []string {
	a.t.Helper()
	titles := []string{}
	cursor := ""
	for {
		q := url.Values{"filter": {rule}, "sort": {sort}, "limit": {fmt.Sprint(size)}, "cursor": {cursor}}
		status, body, _ := a.call(c, http.MethodGet, "/books?"+q.Encode(), nil)
		if status != http.StatusOK {
			a.t.Fatalf("GET /books: %d %v", status, body)
		}
		for _, b := range body["books"].([]any) {
			titles = append(titles, b.(map[string]any)["title"].(string))
		}
		next, _ := body["nextCursor"].(string)
		if next == "" {
			return titles
		}
		cursor = next
	}
}

func TestAnAuthorIsEverySpellingOfTheirName(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Open", "shared")
	a.bookBy(lib, "Das Kind", "Barbara Goldstein")
	a.bookBy(lib, "Die Baumeisterin", "Barbara Goldstein")
	a.bookBy(lib, "Der Fälscher", "Goldstein, Barbara")
	a.bookBy(lib, "Anderes", "Barbara Hambly")

	status, body, _ := a.call(admin, http.MethodGet, "/authors/"+url.PathEscape("Goldstein, Barbara"), nil)
	if status != http.StatusOK {
		t.Fatalf("GET the author: %d %v", status, body)
	}
	if body["name"] != "Barbara Goldstein" || body["key"] != "barbara goldstein" || body["books"] != 3.0 {
		t.Errorf("the author: %v", body)
	}
	if s := body["spellings"].([]any); len(s) != 2 {
		t.Errorf("the spellings: %v", s)
	}
	rule := fmt.Sprintf(`{"field": "person", "op": "in", "values": [%q]}`, body["key"])
	if got := a.titlesOf(admin, rule, "title", 2); len(got) != 3 || slices.Contains(got, "Anderes") {
		t.Errorf("the author's books: %v", got)
	}
	status, _, _ = a.call(admin, http.MethodGet, "/authors/nobody", nil)
	if status != http.StatusNotFound {
		t.Errorf("an author nobody is: %d", status)
	}
}

func TestSortingBySeriesListsEachInItsOrderThenTheRest(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Open", "shared")
	first := a.bookBy(lib, "A Third", "Ann Author")
	second := a.bookBy(lib, "Z First", "Ann Author")
	unnumbered := a.bookBy(lib, "M Unnumbered", "Ann Author")
	other := a.bookBy(lib, "Y Other", "Ann Author")
	a.bookBy(lib, "B Alone", "Ann Author")
	a.bookBy(lib, "C Alone", "Ann Author")
	a.inSeries(admin, first, "Rivers", 3)
	a.inSeries(admin, second, "Rivers", 1)
	a.inSeries(admin, unnumbered, "Rivers", 0)
	a.pool.Exec(context.Background(), `UPDATE books SET series_index = NULL WHERE id = $1`, unnumbered)
	a.inSeries(admin, other, "Lakes", 1)

	want := []string{"Y Other", "Z First", "A Third", "M Unnumbered", "B Alone", "C Alone"}
	rule := `{"field": "person", "op": "in", "values": ["ann author"]}`
	for _, size := range []int{1, 2, 50} {
		if got := a.titlesOf(admin, rule, "series", size); !slices.Equal(got, want) {
			t.Errorf("in pages of %d: %v", size, got)
		}
	}
}

func TestASeriesSaysHowFarTheReaderIsAndWhatIsMissing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withProvider()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("rita", "reader")
	lib := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")
	books := map[float64]uuid.UUID{}
	for _, index := range []float64{1, 2, 6} {
		books[index] = a.bookBy(lib, fmt.Sprintf("Book %v", index), "Sam Writer")
		a.inSeries(admin, books[index], "The Long Road", index)
	}
	a.inSeries(admin, a.bookBy(vault, "Book 3", "Sam Writer"), "The Long Road", 3)
	if status, body, _ := a.call(reader, http.MethodPut, "/books/"+books[1].String()+"/reading",
		map[string]any{"status": "completed"}); status != http.StatusOK {
		t.Fatalf("reading: %d %v", status, body)
	}

	a.follow(reader, "series", "The Long Road")
	listed := func(title string, index float64) metadata.Record {
		r := work(title, "2024", "Sam Writer")
		r.Provider, r.Series, r.SeriesIndex = "shelf", "The Long Road", &index
		return r
	}
	a.works.set(metadata.Answer{Provider: "shelf", Records: []metadata.Record{
		listed("Book 2", 2), listed("Book 4", 4), listed("Another Language", 6),
	}})
	a.poll()

	var s struct {
		Name    string
		Books   int
		Read    int
		Next    *struct{ Title string }
		Missing []struct{ From, To int }
		Listed  []struct {
			Title       string
			SeriesIndex float64
			Published   string
			Wishable    bool
			Wished      bool
		}
		Library uuid.UUID
	}
	get := func(c *http.Client) {
		t.Helper()
		status, body, _ := a.call(c, http.MethodGet, "/series/"+url.PathEscape("the long road"), nil)
		if status != http.StatusOK {
			t.Fatalf("GET the series: %d %v", status, body)
		}
		data, _ := json.Marshal(body)
		s.Listed = nil
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
	}
	get(reader)
	if s.Name != "The Long Road" || s.Books != 3 || s.Read != 1 || s.Next == nil || s.Next.Title != "Book 2" {
		t.Errorf("the reader: %+v", s)
	}
	// Book 3 lies in a library the reader does not see, 4 the source lists,
	// 2 and 6 are taken.
	if len(s.Missing) != 2 || s.Missing[0].From != 3 || s.Missing[0].To != 3 || s.Missing[1].From != 5 {
		t.Errorf("what is missing: %+v", s.Missing)
	}
	if len(s.Listed) != 1 || s.Listed[0].Title != "Book 4" || s.Listed[0].Published != "2024" || !s.Listed[0].Wishable {
		t.Errorf("what the sources list: %+v", s.Listed)
	}
	if s.Library.String() != lib {
		t.Errorf("the library for a wish: %v", s.Library)
	}
	get(admin)
	if s.Books != 4 || s.Read != 0 {
		t.Errorf("the administrator: %+v", s)
	}

	if status, body, _ := a.call(reader, http.MethodPost, "/books/wishes", map[string]any{
		"library": lib, "provider": "shelf", "title": "Book 4",
		"contributors": []map[string]string{{"name": "Sam Writer", "role": "author"}},
	}); status != http.StatusCreated {
		t.Fatalf("wishing: %d %v", status, body)
	}
	get(reader)
	if len(s.Listed) != 1 || !s.Listed[0].Wished {
		t.Errorf("after the wish: %+v", s.Listed)
	}

	if status, _, _ := a.call(reader, http.MethodGet, "/series/nothing", nil); status != http.StatusNotFound {
		t.Errorf("a series nothing is in: %d", status)
	}
}

func TestTheQuickSearchOffersAuthorsAndSeriesTheViewerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("rita", "reader")
	lib := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")
	a.bookBy(lib, "One", "Barbara Goldstein")
	a.inSeries(admin, a.bookBy(lib, "Two", "Goldstein, Barbara"), "Goldsteins Saga", 1)
	a.bookBy(vault, "Three", "Goldstein Hidden")

	names := func(c *http.Client) []string {
		t.Helper()
		status, body, _ := a.call(c, http.MethodGet, "/books/search?q=goldstein", nil)
		if status != http.StatusOK {
			t.Fatalf("searching: %d %v", status, body)
		}
		out := []string{}
		for _, n := range body["names"].([]any) {
			hit := n.(map[string]any)
			out = append(out, fmt.Sprintf("%s %s %v", hit["kind"], hit["key"], hit["books"]))
		}
		slices.Sort(out)
		return out
	}
	want := []string{"author barbara goldstein 2", "series goldsteins saga 1"}
	if got := names(reader); !slices.Equal(got, want) {
		t.Errorf("the reader: %v", got)
	}
	if got := names(admin); len(got) != 3 {
		t.Errorf("the administrator: %v", got)
	}
}

func TestTheListsOfNamesHoldEachOnceWithWhatTheViewerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("rita", "reader")
	lib := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")
	a.bookBy(lib, "One", "Barbara Goldstein")
	a.bookBy(lib, "Two", "Barbara Goldstein")
	a.bookBy(lib, "Three", "Goldstein, Barbara")
	a.bookBy(lib, "Four", "Carl Adams")
	a.bookBy(vault, "Five", "Zoe Hidden")
	a.inSeries(admin, a.bookBy(lib, "Six", "Carl Adams"), "Rivers", 1)
	a.inSeries(admin, a.bookBy(vault, "Seven", "Zoe Hidden"), "Hidden Things", 1)

	list := func(path string, c *http.Client, size int) []string {
		t.Helper()
		out := []string{}
		after := ""
		for {
			q := url.Values{"limit": {fmt.Sprint(size)}, "after": {after}}
			status, body, _ := a.call(c, http.MethodGet, path+"?"+q.Encode(), nil)
			if status != http.StatusOK {
				t.Fatalf("GET %s: %d %v", path, status, body)
			}
			for _, n := range body["names"].([]any) {
				e := n.(map[string]any)
				out = append(out, fmt.Sprintf("%s=%v", e["key"], e["books"]))
			}
			next, _ := body["nextCursor"].(string)
			if next == "" {
				return out
			}
			after = next
		}
	}
	for _, size := range []int{1, 50} {
		got := list("/authors", reader, size)
		slices.Sort(got)
		if want := []string{"adams carl=2", "barbara goldstein=3"}; !slices.Equal(got, want) {
			t.Errorf("authors in pages of %d: %v", size, got)
		}
	}
	if got := list("/series", reader, 50); !slices.Equal(got, []string{"rivers=1"}) {
		t.Errorf("series: %v", got)
	}
	if got := list("/series", admin, 50); len(got) != 2 {
		t.Errorf("the administrator's series: %v", got)
	}
}
