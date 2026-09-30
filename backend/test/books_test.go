//go:build integration

package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// listAll reads every page of a list and returns the titles in order, and
// how many pages it took.
func (a *app) listAll(c *http.Client, query url.Values) ([]string, int) {
	a.t.Helper()
	var titles []string
	pages := 0
	for {
		status, body, _ := a.call(c, http.MethodGet, "/books?"+query.Encode(), nil)
		if status != 200 {
			a.t.Fatalf("list %v: %d %v", query, status, body)
		}
		pages++
		for _, b := range body["books"].([]any) {
			titles = append(titles, b.(map[string]any)["title"].(string))
		}
		next, _ := body["nextCursor"].(string)
		if next == "" {
			return titles, pages
		}
		query.Set("cursor", next)
		if pages > 100 {
			a.t.Fatal("the list does not end")
		}
	}
}

func TestBooksAreListedAPageAtATime(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	// Titles that tie, and authors that tie, so that the ID has to decide.
	for i := range 120 {
		_, err := books.CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: fmt.Sprintf("Book %03d", i%40),
			Contributors: []catalog.NewContributor{{Name: fmt.Sprintf("Author %c", 'A'+rune(i%7))}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, sort := range []string{"title", "author", "added"} {
		for _, order := range []string{"asc", "desc"} {
			whole, pages := a.listAll(admin, url.Values{"sort": {sort}, "order": {order}, "limit": {"200"}})
			paged, n := a.listAll(admin, url.Values{"sort": {sort}, "order": {order}, "limit": {"50"}, "library": {lib.String()}})
			if pages != 1 || n != 3 || len(whole) != 120 || !slices.Equal(whole, paged) {
				t.Errorf("%s %s: %d books on %d pages of 50, %d on %d pages of 200, not the same list",
					sort, order, len(paged), n, len(whole), pages)
			}
		}
	}
	titles, _ := a.listAll(admin, url.Values{"sort": {"title"}})
	if titles[0] != "Book 000" || titles[119] != "Book 039" {
		t.Errorf("by title the list runs from %q to %q", titles[0], titles[119])
	}

	status, body, _ := a.call(admin, http.MethodGet, "/books?sort=author&cursor=nonsense", nil)
	if status != 422 || fieldError(body, "cursor") == "" {
		t.Errorf("a cursor that is none: %d %v", status, body)
	}
	_, first, _ := a.call(admin, http.MethodGet, "/books?sort=title&limit=10", nil)
	status, body, _ = a.call(admin, http.MethodGet, "/books?sort=author&cursor="+first["nextCursor"].(string), nil)
	if status != 422 || fieldError(body, "cursor") == "" {
		t.Errorf("the cursor of another order: %d %v", status, body)
	}
	status, body, _ = a.call(admin, http.MethodGet, "/books?sort=colour&limit=0", nil)
	if status != 422 || fieldError(body, "sort") == "" || fieldError(body, "limit") == "" {
		t.Errorf("an unknown order and a limit of 0: %d %v", status, body)
	}
}

func TestBooksOfHiddenLibrariesAreNowhere(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	private := a.externalLibrary(admin, "Private", "private", books)
	a.scanNow(private)
	file := a.shelfFiles()["Emma.epub"]

	if titles, _ := a.listAll(reader, url.Values{}); len(titles) != 0 {
		t.Errorf("a reader is listed %v from a library they may not see", titles)
	}
	if titles, _ := a.listAll(reader, url.Values{"library": {private}}); len(titles) != 0 {
		t.Errorf("asking for the library by its ID lists %v", titles)
	}
	for what, path := range map[string]string{
		"book":     "/books/" + file.BookID.String(),
		"download": "/files/" + file.ID.String() + "/download",
	} {
		_, hidden, _ := a.call(reader, http.MethodGet, path, nil)
		_, missing, _ := a.call(reader, http.MethodGet, strings.Replace(path, file.BookID.String(), uuid.NewString(), 1), nil)
		status, _, _ := a.call(reader, http.MethodGet, path, nil)
		delete(hidden["error"].(map[string]any), "requestId")
		if what == "book" {
			delete(missing["error"].(map[string]any), "requestId")
			if !mapsEqual(hidden, missing) {
				t.Errorf("a hidden book answers %v, a missing one %v", hidden, missing)
			}
		}
		if status != 404 {
			t.Errorf("the %s of a hidden library: %d, want 404", what, status)
		}
	}
	if titles, _ := a.listAll(admin, url.Values{}); !slices.Equal(titles, []string{"Emma"}) {
		t.Errorf("the administrator is listed %v", titles)
	}
}

func TestBookDetail(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Austen", "Emma.epub"), emmaMetadata, coverPNG(t), "Emma Woodhouse, handsome, clever, and rich.")
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Austen/Emma.epub"]
	a.extract(file.ID)

	status, book, _ := a.call(reader, http.MethodGet, "/books/"+file.BookID.String(), nil)
	if status != 200 {
		t.Fatalf("get book: %d %v", status, book)
	}
	if book["title"] != "Emma" || book["published"] != "1815-12" || book["series"] != "Austen Novels" ||
		book["publisher"] != "Penguin Classics" || book["coverKey"] == nil {
		t.Errorf("book = %v", book)
	}
	contributors := book["contributors"].([]any)
	if len(contributors) != 2 || contributors[0].(map[string]any)["name"] != "Jane Austen" || contributors[1].(map[string]any)["role"] != "illustrator" {
		t.Errorf("contributors = %v", contributors)
	}
	files := book["files"].([]any)
	f := files[0].(map[string]any)
	if len(files) != 1 || f["name"] != "Emma.epub" || f["format"] != "epub" || f["missing"] != false || f["relPath"] != nil {
		t.Errorf("files as a reader sees them = %v", files)
	}
	_, forAdmin, _ := a.call(admin, http.MethodGet, "/books/"+file.BookID.String(), nil)
	if forAdmin["files"].([]any)[0].(map[string]any)["relPath"] != "Austen/Emma.epub" {
		t.Errorf("an administrator is not told where the file lies: %v", forAdmin["files"])
	}
}

func TestFilesAreDownloadedInRanges(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := t.TempDir()
	content := strings.Repeat("0123456789", 1000)
	os.WriteFile(filepath.Join(books, "Brontë – Jane Eyre.pdf"), []byte(content), 0o644)
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	var file shelfFile
	for _, f := range a.shelfFiles() {
		file = f
	}
	address := a.url + httpapi.APIPrefix + "/files/" + file.ID.String() + "/download"

	get := func(header http.Header) (*http.Response, string) {
		req, _ := http.NewRequest(http.MethodGet, address, nil)
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := reader.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}
	whole, body := get(nil)
	if whole.StatusCode != 200 || body != content || whole.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("download: %d %s, %d bytes", whole.StatusCode, whole.Header.Get("Content-Type"), len(body))
	}
	if cd := whole.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "Bront%C3%AB") {
		t.Errorf("Content-Disposition = %q, want an attachment with the name encoded", cd)
	}
	etag := whole.Header.Get("ETag")
	part, body := get(http.Header{"Range": {"bytes=100-109"}})
	if part.StatusCode != 206 || body != "0123456789" || part.Header.Get("Content-Range") != "bytes 100-109/10000" {
		t.Errorf("a range: %d %q %s", part.StatusCode, body, part.Header.Get("Content-Range"))
	}
	if resumed, body := get(http.Header{"Range": {"bytes=9990-"}, "If-Range": {etag}}); resumed.StatusCode != 206 || len(body) != 10 {
		t.Errorf("resuming with the ETag: %d, %d bytes", resumed.StatusCode, len(body))
	}
	if stale, _ := get(http.Header{"Range": {"bytes=9990-"}, "If-Range": {`"another"`}}); stale.StatusCode != 200 {
		t.Errorf("resuming a file that changed: %d, want the whole file", stale.StatusCode)
	}

	// Gone from the disk since the scan.
	os.Remove(filepath.Join(books, "Brontë – Jane Eyre.pdf"))
	if gone, _ := get(nil); gone.StatusCode != 404 {
		t.Errorf("a file that is gone: %d, want 404", gone.StatusCode)
	}
}
