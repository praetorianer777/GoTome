//go:build integration

package test

import (
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestAWishedBookWaitsAndTheMatchingFileFulfilsIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withProvider()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	other, _ := a.signedIn("other", "reader")
	folder := t.TempDir()
	lib := a.externalLibrary(admin, "Shelf", "shared", folder)

	status, body, _ := a.call(reader, http.MethodGet, "/metadata/search?"+url.Values{"title": {"Emma"}}.Encode(), nil)
	candidates, _ := body["candidates"].([]any)
	if status != 200 || len(candidates) != 1 {
		t.Fatalf("search: %d %v", status, body)
	}
	token := candidates[0].(map[string]any)["coverToken"]

	status, wish, _ := a.call(reader, http.MethodPost, "/books/wishes", map[string]any{
		"library": lib, "provider": "shelf", "title": "Emma", "publisher": "John Murray",
		"contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}},
		"identifiers":  []map[string]string{{"type": "isbn", "value": "978-0-14-143958-7"}},
		"coverToken":   token,
	})
	if status != 201 {
		t.Fatalf("wish: %d %v", status, wish)
	}
	if wish["placeholder"] != true || len(wish["files"].([]any)) != 0 || wish["coverKey"] == nil ||
		wish["reading"].(map[string]any)["status"] != "wishlist" {
		t.Errorf("the wished book: %v", wish)
	}
	if f := field(wish, "publisher"); f["source"] != "provider" || f["locked"] != false {
		t.Errorf("the publisher's source: %v", f)
	}
	emma := wish["id"].(string)
	// One without an ISBN, found by title and author.
	_, persuasion, _ := a.call(reader, http.MethodPost, "/books/wishes", map[string]any{
		"library": lib, "provider": "shelf", "title": "Persuasion",
		"contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}},
	})

	// Not in the library yet: not listed, not found, not counted.
	if titles, _ := a.listAll(reader, url.Values{}); len(titles) != 0 {
		t.Errorf("listed: %v", titles)
	}
	if found, _ := a.search(reader, "Emma"); len(found) != 0 {
		t.Errorf("found: %v", found)
	}
	wishlist := url.Values{"placeholders": {"include"}, "filter": {`{"all":[{"field":"status","op":"in","values":["wishlist"]}]}`}}
	if titles, _ := a.listAll(reader, wishlist); !slices.Equal(titles, []string{"Emma", "Persuasion"}) {
		t.Errorf("the reader's wishlist: %v", titles)
	}
	if titles, _ := a.listAll(other, wishlist); len(titles) != 0 {
		t.Errorf("another reader's wishlist: %v", titles)
	}

	// The files arrive.
	writeEPUB(t, filepath.Join(folder, "Emma.epub"),
		`<dc:title>Emma</dc:title><dc:identifier opf:scheme="ISBN">0-14-143958-0</dc:identifier>`, nil, "Emma Woodhouse.")
	writeEPUB(t, filepath.Join(folder, "Persuasion.epub"),
		`<dc:title>Persuasion</dc:title><dc:creator opf:role="aut">Jane Austen</dc:creator>`, nil, "Sir Walter Elliot.")
	writeEPUB(t, filepath.Join(folder, "Mansfield Park.epub"),
		`<dc:title>Mansfield Park</dc:title><dc:creator opf:role="aut">Jane Austen</dc:creator>`, nil, "Fanny Price.")
	a.scanNow(lib)
	files := a.shelfFiles()
	scanned := files["Emma.epub"].BookID
	for _, name := range []string{"Emma.epub", "Persuasion.epub", "Mansfield Park.epub"} {
		a.extract(files[name].ID)
	}
	files = a.shelfFiles()
	if files["Emma.epub"].BookID.String() != emma || files["Persuasion.epub"].BookID.String() != persuasion["id"] {
		t.Errorf("the files went to %v and %v, not to the wished books", files["Emma.epub"].BookID, files["Persuasion.epub"].BookID)
	}
	if id := files["Mansfield Park.epub"].BookID.String(); id == emma || id == persuasion["id"] {
		t.Error("a book nobody wished for joined a wish")
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+scanned.String(), nil); status != 404 {
		t.Errorf("the book the scan made is still there: %d", status)
	}
	_, got, _ := a.call(reader, http.MethodGet, "/books/"+emma, nil)
	if got["placeholder"] != false || len(got["files"].([]any)) != 1 || got["publisher"] != "John Murray" ||
		got["reading"].(map[string]any)["status"] != "wishlist" {
		t.Errorf("the fulfilled book: %v", got)
	}
	if titles, _ := a.listAll(reader, url.Values{}); !slices.Equal(titles, []string{"Emma", "Mansfield Park", "Persuasion"}) {
		t.Errorf("the library now: %v", titles)
	}

	for name, req := range map[string]map[string]any{
		"no title":         {"library": lib, "provider": "shelf"},
		"no such provider": {"library": lib, "provider": "nobody", "title": "X"},
		"no such library":  {"library": uuid.NewString(), "provider": "shelf", "title": "X"},
	} {
		if status, body, _ := a.call(reader, http.MethodPost, "/books/wishes", req); status != 422 {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/metadata/search", nil); status != 422 {
		t.Errorf("a search for nothing: %d", status)
	}
}
