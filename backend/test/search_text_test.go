//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// textResult is GET /search's answer as the tests read it.
type textResult struct {
	Books []struct {
		Book struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"book"`
		Corrected bool `json:"corrected"`
		Hits      []struct {
			FileID   string `json:"fileId"`
			Position int    `json:"position"`
			Chapter  string `json:"chapter"`
			PageFrom int    `json:"pageFrom"`
			Snippet  []struct {
				Text  string `json:"text"`
				Match bool   `json:"match"`
			} `json:"snippet"`
		} `json:"hits"`
	} `json:"books"`
	More bool `json:"more"`
}

func (a *app) searchText(c *http.Client, query url.Values) (int, textResult) {
	a.t.Helper()
	var out textResult
	status, body := a.get(c, "/search?"+query.Encode())
	if status == 200 {
		if err := json.Unmarshal(body, &out); err != nil {
			a.t.Fatal(err)
		}
	}
	return status, out
}

func titles(r textResult) []string {
	var out []string
	for _, b := range r.Books {
		out = append(out, b.Book.Title)
	}
	return out
}

func TestTheTextOfBooksIsSearched(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	meta := func(title, author, lang string) string {
		return `<dc:identifier id="uid">urn:uuid:` + title + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator opf:role="aut">` + author + `</dc:creator>
    <dc:language>` + lang + `</dc:language>`
	}
	filler := strings.Repeat("Nothing much happened in the town that week, and the weather was mild. ", 40)
	writeEPUB(t, filepath.Join(books, "Horses.epub"), meta("Horses", "Anna Reiter", "en"), nil,
		filler, "The horses runs along the river every morning, and the white whale was never seen there. "+filler)
	writeEPUB(t, filepath.Join(books, "Sea.epub"), meta("Sea", "Bert Seemann", "en"), nil,
		filler, "Out at sea they saw the white whale at last. "+filler)
	writeEPUB(t, filepath.Join(books, "Fluss.epub"), meta("Fluss", "Clara Ufer", "de"), nil,
		strings.Repeat("Die Kinder spielten am Ufer, und die alten Häuser standen still am Fluss. ", 40))
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}

	// A word finds its other forms.
	status, got := a.searchText(admin, url.Values{"q": {"running"}})
	if status != 200 || strings.Join(titles(got), ",") != "Horses" {
		t.Fatalf("running: %d %v", status, titles(got))
	}
	hit := got.Books[0].Hits[0]
	var matched []string
	for _, p := range hit.Snippet {
		if p.Match {
			matched = append(matched, p.Text)
		}
	}
	if len(matched) == 0 || matched[0] != "runs" || hit.PageFrom == 0 || hit.FileID == "" {
		t.Errorf("the hit: %+v", hit)
	}

	// A phrase finds the words only in its order.
	if _, got := a.searchText(admin, url.Values{"q": {`"white whale"`}}); len(got.Books) != 2 {
		t.Errorf(`"white whale": %v`, titles(got))
	}
	if _, got := a.searchText(admin, url.Values{"q": {`"whale white"`}}); len(got.Books) != 0 {
		t.Errorf(`"whale white": %v`, titles(got))
	}
	if _, got := a.searchText(admin, url.Values{"q": {`river "white whale"`}}); strings.Join(titles(got), ",") != "Horses" {
		t.Errorf(`river "white whale": %v`, titles(got))
	}

	// German books are stemmed as German.
	if _, got := a.searchText(admin, url.Values{"q": {"Haus Kind"}}); strings.Join(titles(got), ",") != "Fluss" {
		t.Errorf("Haus Kind: %v", titles(got))
	}

	// The book lists' filter narrows the search.
	byAuthor := `{"field":"author","op":"in","values":["Bert Seemann"]}`
	if _, got := a.searchText(admin, url.Values{"q": {`"white whale"`}, "filter": {byAuthor}}); strings.Join(titles(got), ",") != "Sea" {
		t.Errorf("white whale by Bert Seemann: %v", titles(got))
	}

	// Pages of one book each.
	_, first := a.searchText(admin, url.Values{"q": {`"white whale"`}, "limit": {"1"}})
	_, second := a.searchText(admin, url.Values{"q": {`"white whale"`}, "limit": {"1"}, "offset": {"1"}})
	if len(first.Books) != 1 || !first.More || len(second.Books) != 1 || second.More || first.Books[0].Book.ID == second.Books[0].Book.ID {
		t.Errorf("pages: %v (more %v), %v (more %v)", titles(first), first.More, titles(second), second.More)
	}

	// A query without a word is refused.
	if status, _ := a.searchText(admin, url.Values{"q": {` "" ...`}}); status != 422 {
		t.Errorf("an empty query: %d", status)
	}
}

func TestSearchKeepsToTheLibrariesTheCallerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	open, closed := t.TempDir(), t.TempDir()
	writeEPUB(t, filepath.Join(open, "Open.epub"), emmaMetadata, nil, "A lighthouse stood on the cliff above the bay.")
	writeEPUB(t, filepath.Join(closed, "Closed.epub"), emmaMetadata, nil, "A lighthouse keeper wrote letters nobody read.")
	shared := a.externalLibrary(admin, "Shared", "shared", open)
	private := a.externalLibrary(admin, "Private", "private", closed)
	a.scanNow(shared)
	a.scanNow(private)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}

	if _, got := a.searchText(admin, url.Values{"q": {"lighthouse"}}); len(got.Books) != 2 {
		t.Fatalf("the administrator finds %d books", len(got.Books))
	}
	_, got := a.searchText(reader, url.Values{"q": {"lighthouse"}})
	if len(got.Books) != 1 || len(got.Books[0].Hits) == 0 || !strings.Contains(got.Books[0].Hits[0].Snippet[len(got.Books[0].Hits[0].Snippet)-1].Text, "bay") {
		t.Errorf("the reader finds %+v", got.Books)
	}
	if _, got := a.searchText(reader, url.Values{"q": {"lighthouse"}, "library": {private}}); len(got.Books) != 0 {
		t.Errorf("the reader finds %v in the private library", titles(got))
	}
}
