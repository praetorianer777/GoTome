//go:build integration

package test

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// shelfOfTexts makes a shared library of one EPUB per title, each holding its
// text with enough around it to make a book, and reads them all.
func (a *app) shelfOfTexts(admin *http.Client, texts map[string]string) {
	a.t.Helper()
	dir := a.t.TempDir()
	filler := strings.Repeat("Nothing much happened in the town that week. ", 30)
	for title, text := range texts {
		meta := `<dc:identifier id="uid">urn:uuid:` + title + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:language>en</dc:language>`
		writeEPUB(a.t, filepath.Join(dir, title+".epub"), meta, nil, filler+text+" "+filler)
	}
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", dir))
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
}

func matches(r textResult, book int) []string {
	var out []string
	for _, h := range r.Books[book].Hits {
		for _, p := range h.Snippet {
			if p.Match {
				out = append(out, strings.ToLower(p.Text))
			}
		}
	}
	return out
}

func TestTyposAreRepairedWhenTheQueryFindsLittle(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	a.shelfOfTexts(admin, map[string]string{
		"American": "The color of the sea changed with the light over the harbour.",
		"British":  "The colour of the sea changed with the light over the harbour.",
		"Whaling":  "They followed the white whale for three days and three nights.",
	})

	// As written, "colours" is the British spelling only; repaired, it is
	// the American one too, after it.
	status, got := a.searchText(admin, url.Values{"q": {"colours"}})
	if status != 200 || strings.Join(titles(got), ",") != "British,American" {
		t.Fatalf("colours: %d %v", status, titles(got))
	}
	if got.Books[0].Corrected || !got.Books[1].Corrected {
		t.Errorf("corrected: %v, %v", got.Books[0].Corrected, got.Books[1].Corrected)
	}
	if m := matches(got, 1); len(m) == 0 || m[0] != "color" {
		t.Errorf("the repaired hit highlights %v", m)
	}

	// Two letters swapped, one letter missing; in free words and in a phrase.
	for _, q := range []string{"whael", "wite whael", `"wite whale"`} {
		_, got := a.searchText(admin, url.Values{"q": {q}})
		if strings.Join(titles(got), ",") != "Whaling" || !got.Books[0].Corrected {
			t.Errorf("%s: %v", q, titles(got))
		}
	}

	// A word no text holds anything like is not made into another.
	if _, got := a.searchText(admin, url.Values{"q": {"xylophone"}}); len(got.Books) != 0 {
		t.Errorf("xylophone: %v", titles(got))
	}
}

func TestTyposAreLeftAloneWhenTheQueryFindsEnough(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	a.shelfOfTexts(admin, map[string]string{
		"One":      "The colour of the morning sky.",
		"Two":      "A colour nobody had a name for.",
		"Three":    "Every colour of the rainbow.",
		"American": "The color of the evening sky.",
	})
	_, got := a.searchText(admin, url.Values{"q": {"colours"}})
	if len(got.Books) != 3 || strings.Contains(strings.Join(titles(got), ","), "American") {
		t.Errorf("colours: %v", titles(got))
	}
	for _, b := range got.Books {
		if b.Corrected {
			t.Errorf("%s is marked corrected", b.Book.Title)
		}
	}
}
