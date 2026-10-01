//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// field is what a book says about one field's source and lock.
func field(book map[string]any, name string) map[string]any {
	f, _ := book["fields"].(map[string]any)[name].(map[string]any)
	return f
}

func squarePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 300, 300))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (a *app) putCover(c *http.Client, bookID string, image []byte) (int, map[string]any) {
	a.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	w, err := form.CreateFormFile("file", "cover.png")
	if err != nil {
		a.t.Fatal(err)
	}
	_, _ = w.Write(image)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPut, a.url+httpapi.APIPrefix+"/books/"+bookID+"/cover", &body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestAnEditedFieldIsLockedAgainstTheNextRead(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	shared := t.TempDir()
	writeEPUB(t, filepath.Join(shared, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	lib := a.externalLibrary(admin, "Shelf", "shared", shared)
	a.scanNow(lib)
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)
	path := "/books/" + emma.BookID.String()

	_, before, _ := a.call(reader, http.MethodGet, path, nil)
	if f := field(before, "title"); f["source"] != "file" || f["detail"] != "epub" || f["locked"] != false {
		t.Errorf("the title before the edit: %v", f)
	}
	if ids := before["identifiers"].([]any); len(ids) != 1 || ids[0].(map[string]any)["fromFile"] != true {
		t.Errorf("the file's ISBN: %v", ids)
	}
	if status, _, _ := a.call(reader, http.MethodPatch, path, map[string]any{"title": "Mine"}); status != 403 {
		t.Errorf("a reader edits: %d", status)
	}

	status, body, _ := a.call(editor, http.MethodPatch, path, map[string]any{
		"title":  "Emma (Annotated)",
		"series": map[string]any{"name": "Penguin Austen", "index": 2.5},
		"contributors": []map[string]any{
			{"name": "Jane Austen", "role": "author"},
			{"name": "Fiona Stafford", "role": "editor"},
		},
		"tags":        []string{"Romance", "Classics"},
		"identifiers": []map[string]any{{"type": "isbn", "value": "978-0-14-143958-7"}},
		"pageCount":   0,
	})
	if status != 200 || body["title"] != "Emma (Annotated)" || body["series"] != "Penguin Austen" || body["seriesIndex"] != 2.5 || body["pageCount"] != nil {
		t.Fatalf("edit: %d %v", status, body)
	}
	if f := field(body, "title"); f["source"] != "manual" || f["locked"] != true {
		t.Errorf("the title after the edit: %v", f)
	}
	if f := field(body, "language"); f["source"] != "file" || f["locked"] != false {
		t.Errorf("a field the edit left alone: %v", f)
	}
	if ids := body["identifiers"].([]any); len(ids) != 1 || ids[0].(map[string]any)["fromFile"] != false {
		t.Errorf("the ISBN the book and its file both have: %v", ids)
	}
	if len(body["contributors"].([]any)) != 2 || len(body["tags"].([]any)) != 2 {
		t.Errorf("credits and tags: %v %v", body["contributors"], body["tags"])
	}

	// The file is read again, and says what it said before.
	if status, _, _ := a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/extraction", nil); status != 202 {
		t.Fatalf("read again: %d", status)
	}
	a.extract(emma.ID)
	_, after, _ := a.call(editor, http.MethodGet, path, nil)
	if after["title"] != "Emma (Annotated)" || after["series"] != "Penguin Austen" || len(after["tags"].([]any)) != 2 {
		t.Errorf("after the file was read again: %v", after)
	}

	// Unlocked, the field is the file's again at the next read.
	status, body, _ = a.call(editor, http.MethodPatch, path, map[string]any{"locks": map[string]bool{"title": false}})
	if status != 200 || field(body, "title")["locked"] != false || body["title"] != "Emma (Annotated)" {
		t.Fatalf("unlock: %d %v", status, field(body, "title"))
	}
	a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/extraction", nil)
	a.extract(emma.ID)
	_, after, _ = a.call(editor, http.MethodGet, path, nil)
	if after["title"] != "Emma" || field(after, "title")["source"] != "file" || after["series"] != "Penguin Austen" {
		t.Errorf("after unlocking the title: %v %v", after["title"], after["series"])
	}

	status, body, _ = a.call(editor, http.MethodPatch, path, map[string]any{
		"title":       " ",
		"language":    "not a language",
		"published":   "last winter",
		"identifiers": []map[string]any{{"type": "isbn", "value": "978-0-14-143958-8"}},
		"locks":       map[string]bool{"mood": true},
	})
	for _, f := range []string{"title", "language", "published", "identifiers", "locks"} {
		if fieldError(body, f) == "" {
			t.Errorf("no complaint about %s: %d %v", f, status, body)
		}
	}
}

func TestEditingNeedsABookOneMaySee(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	hidden := t.TempDir()
	writeEPUB(t, filepath.Join(hidden, "Secret.epub"), `<dc:title>Secret</dc:title><dc:creator>Sabine Secretwriter</dc:creator>`, nil, "Hush.")
	shared := t.TempDir()
	writeEPUB(t, filepath.Join(shared, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	a.scanNow(a.externalLibrary(admin, "Hidden", "private", hidden))
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", shared))
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}
	secret := "/books/" + files["Secret.epub"].BookID.String()

	if status, _, _ := a.call(editor, http.MethodPatch, secret, map[string]any{"title": "Found"}); status != 404 {
		t.Errorf("edit a hidden book: %d", status)
	}
	if status, _ := a.putCover(editor, files["Secret.epub"].BookID.String(), coverPNG(t)); status != 404 {
		t.Errorf("cover a hidden book: %d", status)
	}

	// Names are suggested from the books one may see only.
	names := func(c *http.Client, kind, q string) []any {
		status, body, _ := a.call(c, http.MethodGet, "/books/names?kind="+kind+"&q="+q, nil)
		if status != 200 {
			t.Fatalf("names: %d %v", status, body)
		}
		return body["names"].([]any)
	}
	if got := names(editor, "author", "aus"); len(got) != 1 || got[0] != "Jane Austen" {
		t.Errorf("authors for aus: %v", got)
	}
	if got := names(editor, "author", "secret"); len(got) != 0 {
		t.Errorf("an author of a hidden book is suggested: %v", got)
	}
	if got := names(admin, "author", "secret"); len(got) != 1 {
		t.Errorf("the administrator's authors for secret: %v", got)
	}
	if got := names(editor, "series", "nov"); len(got) != 1 || got[0] != "Austen Novels" {
		t.Errorf("series for nov: %v", got)
	}
	if status, _, _ := a.call(editor, http.MethodGet, "/books/names?kind=mood&q=a", nil); status != 422 {
		t.Errorf("an unknown kind of name: %d", status)
	}
}

func TestACoverPutByHandStays(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	shared := t.TempDir()
	writeEPUB(t, filepath.Join(shared, "Emma.epub"), emmaMetadata, coverPNG(t), "Emma Woodhouse.")
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", shared))
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)
	id := emma.BookID.String()
	_, before, _ := a.call(editor, http.MethodGet, "/books/"+id, nil)

	if status, body := a.putCover(editor, id, []byte("not an image")); status != 422 || fieldError(body, "file") == "" {
		t.Errorf("a cover that is no image: %d %v", status, body)
	}
	status, body := a.putCover(editor, id, squarePNG(t))
	if status != 200 || body["coverKey"] == before["coverKey"] || field(body, "cover")["source"] != "manual" {
		t.Fatalf("put a cover: %d %v", status, body)
	}
	if resp, err := editor.Get(a.url + httpapi.APIPrefix + "/books/" + id + "/covers/small?v=" + body["coverKey"].(string)); err != nil || resp.StatusCode != 200 {
		t.Errorf("the new cover: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}

	status, body, _ = a.call(editor, http.MethodDelete, "/books/"+id+"/cover", nil)
	if status != 200 || body["coverKey"] != nil || field(body, "cover")["locked"] != true {
		t.Fatalf("take the cover away: %d %v", status, body)
	}
	a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/extraction", nil)
	a.extract(emma.ID)
	if _, after, _ := a.call(editor, http.MethodGet, "/books/"+id, nil); after["coverKey"] != nil {
		t.Errorf("the file's cover came back: %v", after["coverKey"])
	}
}
