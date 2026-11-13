//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/calibre"
	"github.com/praetorianer777/gotome/backend/internal/calibre/calibretest"
)

// calibreLibrary writes the fixture Calibre library, whose EPUBs say other
// things about their books than Calibre does, and returns its folder.
func calibreLibrary(t *testing.T, books []calibretest.Book) string {
	t.Helper()
	dir := t.TempDir()
	scratch := t.TempDir()
	content := func(b calibretest.Book, format string) []byte {
		switch {
		case b.ID == 3:
			return nil
		case format == "EPUB":
			path := filepath.Join(scratch, b.Name()+".epub")
			writeEPUB(t, path, `
    <dc:identifier id="uid">urn:uuid:00000000-0000-4000-8000-`+fmt.Sprintf("%012d", b.ID)+`</dc:identifier>
    <dc:title>`+b.Title+` (as the file says)</dc:title>
    <dc:creator>Somebody Else</dc:creator>
    <dc:subject>From the file</dc:subject>
    <dc:language>en</dc:language>`, nil, "Some words to read.")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		return []byte("%PDF-1.4\n% " + b.Title + "\n")
	}
	if err := calibretest.Write(dir, books, content, coverJPEG(t)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func coverJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 300, 450))
	for i := range img.Pix {
		img.Pix[i] = byte(i % 241)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (a *app) calibrePreview(c *http.Client, library string) (int, map[string]any) {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/calibre/imports", map[string]any{"library": library})
	return status, body
}

func (a *app) calibreRun(c *http.Client, id string, body map[string]any) map[string]any {
	a.t.Helper()
	if status, got, _ := a.call(c, http.MethodPost, "/calibre/imports/"+id+"/start", body); status != 202 {
		a.t.Fatalf("start: %d %v", status, got)
	}
	for {
		done, err := a.server.Calibre.Run(context.Background(), uuid.MustParse(id))
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			break
		}
	}
	_, got, _ := a.call(c, http.MethodGet, "/calibre/imports/"+id, nil)
	return got
}

// scannedCalibre adds the folder as an external library, scans it and reads
// its files; the books are then what the files say.
func (a *app) scannedCalibre(admin *http.Client, dir string) string {
	a.t.Helper()
	id := a.externalLibrary(admin, "Calibre", "shared", dir)
	a.scanNow(id)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	return id
}

func bookByTitle(t *testing.T, a *app, c *http.Client, title string) (string, map[string]any) {
	t.Helper()
	var id string
	a.pool.QueryRow(context.Background(), `SELECT id::text FROM books WHERE title = $1`, title).Scan(&id)
	if id == "" {
		t.Fatalf("no book titled %q", title)
	}
	_, book, _ := a.call(c, http.MethodGet, "/books/"+id, nil)
	return id, book
}

func names(list any, key string) []string {
	var out []string
	for _, e := range list.([]any) {
		out = append(out, e.(map[string]any)[key].(string))
	}
	return out
}

func tagsOf(book map[string]any) []string {
	var out []string
	for _, tag := range book["tags"].([]any) {
		out = append(out, tag.(string))
	}
	slices.Sort(out)
	return out
}

func TestACalibreLibraryIsPreviewedImportedAndImportedAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	dir := calibreLibrary(t, calibretest.Books())
	library := a.scannedCalibre(admin, dir)

	// One folder per book, formats together: Calibre's layout is one book each.
	var n int
	a.pool.QueryRow(context.Background(), `SELECT count(*) FROM books`).Scan(&n)
	if n != 3 {
		t.Fatalf("%d books after the scan, want 3 (the fourth has no file)", n)
	}
	_, lantern := bookByTitle(t, a, admin, "The Lantern at Dusk (as the file says)")
	if len(lantern["files"].([]any)) != 2 {
		t.Fatalf("the two formats of a folder are not one book: %v", lantern["files"])
	}

	// Who may ask.
	if status, _ := a.calibrePreview(editor, library); status != 403 {
		t.Errorf("an editor previewing: %d, want 403", status)
	}
	managed := a.library(admin, "Managed", "shared")
	if status, body := a.calibrePreview(admin, managed); status != 422 {
		t.Errorf("a managed library: %d %v, want 422", status, body)
	}
	plain := a.externalLibrary(admin, "Plain folder", "shared", t.TempDir())
	if status, body := a.calibrePreview(admin, plain); status != 422 {
		t.Errorf("a folder with no metadata.db: %d %v, want 422", status, body)
	}
	if status, _ := a.calibrePreview(admin, uuid.NewString()); status != 422 {
		t.Errorf("a library that does not exist: %d, want 422", status)
	}

	status, preview := a.calibrePreview(admin, library)
	if status != 201 || preview["state"] != "preview" || preview["books"] != 4.0 || preview["ready"] != 3.0 ||
		preview["skipped"] != 1.0 || preview["missingFiles"] != 1.0 || preview["covers"] != 1.0 ||
		preview["series"] != 1.0 || preview["tags"] != 3.0 || preview["rated"] != 2.0 {
		t.Fatalf("preview: %d %v", status, preview)
	}
	formats := preview["formats"].(map[string]any)
	if formats["epub"] != 2.0 || formats["pdf"] != 2.0 {
		t.Errorf("formats %v", formats)
	}
	if got := names(preview["columns"], "label"); !slices.Equal(got, []string{"read", "shelf", "finished"}) {
		t.Errorf("custom columns %v", got)
	}
	problem := preview["problems"].([]any)[0].(map[string]any)
	if problem["title"] != "Die Brücke am Fluss" || !strings.Contains(problem["message"].(string), "scan") {
		t.Errorf("the book left out: %v", problem)
	}
	if _, after := bookByTitle(t, a, admin, "The Lantern at Dusk (as the file says)"); after["description"] != lantern["description"] {
		t.Error("the preview described a book")
	}
	id := preview["id"].(string)
	if status, body, _ := a.call(editor, http.MethodGet, "/calibre/imports/"+id, nil); status != 403 {
		t.Errorf("an editor reading the preview: %d %v", status, body)
	}
	if status, body, _ := a.call(admin, http.MethodPost, "/calibre/imports/"+id+"/start", map[string]any{"columns": map[string]any{"read": "tags", "nothing": "tags"}}); status != 422 {
		t.Errorf("a column that does not exist: %d %v", status, body)
	}
	if status, body, _ := a.call(admin, http.MethodPost, "/calibre/imports/"+id+"/start", map[string]any{"columns": map[string]any{"shelf": "status"}}); status != 422 {
		t.Errorf("a text column as the status: %d %v", status, body)
	}

	// The person rated Emma before, and says so again below the import.
	emmaID, _ := bookByTitle(t, a, admin, "Emma (as the file says)")
	if status, body, _ := a.call(admin, http.MethodPut, "/books/"+emmaID+"/reading", map[string]any{"rating": 5}); status != 200 {
		t.Fatalf("rating: %d %v", status, body)
	}

	done := a.calibreRun(admin, id, map[string]any{
		"ratings": true, "columns": map[string]any{"read": "status", "shelf": "tags", "finished": "ignore"},
	})
	outcomes := done["outcomes"].(map[string]any)
	if done["state"] != "done" || outcomes["changed"] != 3.0 || outcomes["skipped"] != 1.0 || outcomes["failed"] != nil {
		t.Fatalf("the import: %v", done)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/calibre/imports/"+id+"/start", map[string]any{}); status != 409 {
		t.Errorf("starting twice: %d", status)
	}

	lanternID, lantern := bookByTitle(t, a, admin, "The Lantern at Dusk")
	if lantern["series"] != "Halden Chronicles" || lantern["seriesIndex"] != 1.0 || lantern["publisher"] != "Brinewick Press" ||
		lantern["published"] != "1965-03-04" || lantern["language"] != "en" ||
		lantern["description"] != "A keeper trims the light.\n\nNobody comes." {
		t.Errorf("the book: %v", lantern)
	}
	if got := names(lantern["contributors"], "name"); !slices.Equal(got, []string{"Henrik Halden"}) {
		t.Errorf("authors %v", got)
	}
	if got := tagsOf(lantern); !slices.Equal(got, []string{"Fiction", "Sea", "favourites", "sailing"}) {
		t.Errorf("tags %v", got)
	}
	if isbn := names(lantern["identifiers"], "value"); !slices.Contains(isbn, "9780195001013") {
		t.Errorf("identifiers %v", isbn)
	}
	for _, f := range []string{"title", "series", "tags", "contributors", "description", "cover"} {
		if s := field(lantern, f); s["source"] != "calibre" || s["locked"] != false {
			t.Errorf("field %s: %v, want Calibre's and not locked", f, s)
		}
	}
	if lantern["coverKey"] == nil {
		t.Error("no cover")
	} else if status, _, _ := a.fetch(admin, "/api/v1/books/"+lanternID+"/covers/small"); status != 200 {
		t.Errorf("the cover: %d", status)
	}
	reading := lantern["reading"].(map[string]any)
	if reading["status"] != "completed" || reading["rating"] != 4.0 {
		t.Errorf("the importer's reading: %v", reading)
	}
	_, emma := bookByTitle(t, a, admin, "Emma")
	if r := emma["reading"].(map[string]any); r["rating"] != 5.0 || r["status"] != "unread" {
		t.Errorf("a rating the person gave stays, a no is no status: %v", r)
	}
	if got := tagsOf(emma); !slices.Equal(got, []string{"Classics"}) {
		t.Errorf("Emma's tags %v", got)
	}
	if _, pamphlet := bookByTitle(t, a, admin, "Undated Pamphlet"); pamphlet["published"] != nil {
		t.Errorf("a date Calibre does not have: %v", pamphlet["published"])
	}

	// Again changes nothing, and a lack of files is still told.
	_, again := a.calibrePreview(admin, library)
	second := a.calibreRun(admin, again["id"].(string), map[string]any{
		"ratings": true, "columns": map[string]any{"read": "status", "shelf": "tags"},
	})
	outcomes = second["outcomes"].(map[string]any)
	if outcomes["unchanged"] != 3.0 || outcomes["skipped"] != 1.0 || outcomes["changed"] != nil {
		t.Errorf("running it again: %v", second)
	}

	// What a person sets in GOtome wins, and a change in Calibre arrives
	// where they did not.
	if status, body, _ := a.call(admin, http.MethodPatch, "/books/"+lanternID, map[string]any{"title": "My Lantern"}); status != 200 {
		t.Fatalf("edit: %d %v", status, body)
	}
	changed := calibretest.Books()
	changed[0].Tags = []string{"Fiction", "Lighthouses"}
	changed[0].Publisher = "Salt Quay Books"
	calibreLibraryAgain(t, dir, changed)
	_, third := a.calibrePreview(admin, library)
	last := a.calibreRun(admin, third["id"].(string), map[string]any{"columns": map[string]any{"read": "status"}})
	if out := last["outcomes"].(map[string]any); out["changed"] != 1.0 || out["unchanged"] != 2.0 {
		t.Errorf("after a change in Calibre: %v", last)
	}
	_, lantern = bookByTitle(t, a, admin, "My Lantern")
	if lantern["publisher"] != "Salt Quay Books" || !slices.Equal(tagsOf(lantern), []string{"Fiction", "Lighthouses"}) {
		t.Errorf("what Calibre changed: %v %v", lantern["publisher"], lantern["tags"])
	}
	if s := field(lantern, "title"); s["source"] != "manual" || s["locked"] != true {
		t.Errorf("the person's title: %v", s)
	}
}

// calibreLibraryAgain writes the Calibre library over the folder as it was.
func calibreLibraryAgain(t *testing.T, dir string, books []calibretest.Book) {
	t.Helper()
	content := func(b calibretest.Book, format string) []byte {
		data, _ := os.ReadFile(filepath.Join(dir, b.Dir(), b.Name()+"."+strings.ToLower(format)))
		return data
	}
	if err := calibretest.Write(dir, books, content, nil); err != nil {
		t.Fatal(err)
	}
}

func TestImportingACalibreLibraryNeverWritesMetadataDb(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	dir := calibreLibrary(t, calibretest.Books())
	library := a.scannedCalibre(admin, dir)

	path := filepath.Join(dir, calibre.Database)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// Calibre may be running, and the server only reads: even a folder it
	// could not write to is read.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(dir)

	_, preview := a.calibrePreview(admin, library)
	a.calibreRun(admin, preview["id"].(string), map[string]any{"ratings": true})

	after, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(old) {
		t.Errorf("metadata.db changed: same bytes %v, modified %v", bytes.Equal(before, after), info.ModTime())
	}
	if now, _ := os.ReadDir(dir); len(now) != len(entries) {
		t.Errorf("the folder gained files: %v", now)
	}
	if got, err := os.ReadDir(filepath.Join(dir, "Henrik Halden", "The Lantern at Dusk (1)")); err != nil || len(got) != 3 {
		t.Errorf("the book's folder: %v %v, want its two files and cover.jpg", got, err)
	}
}

func TestAnImportWhoseMetadataDbIsGoneFailsEveryBookAndFinishes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	dir := calibreLibrary(t, calibretest.Books())
	library := a.scannedCalibre(admin, dir)
	_, preview := a.calibrePreview(admin, library)
	id := preview["id"].(string)
	_, lantern := bookByTitle(t, a, admin, "The Lantern at Dusk (as the file says)")

	if status, got, _ := a.call(admin, http.MethodPost, "/calibre/imports/"+id+"/start", map[string]any{"ratings": true}); status != 202 {
		t.Fatalf("start: %d %v", status, got)
	}
	if err := os.Remove(filepath.Join(dir, calibre.Database)); err != nil {
		t.Fatal(err)
	}
	done, err := a.server.Calibre.Run(context.Background(), uuid.MustParse(id))
	if err != nil || !done {
		t.Fatalf("run: done %v, %v", done, err)
	}

	_, got, _ := a.call(admin, http.MethodGet, "/calibre/imports/"+id, nil)
	outcomes, _ := got["outcomes"].(map[string]any)
	if got["state"] != "done" || outcomes["failed"] != 4.0 || outcomes["changed"] != nil || outcomes["unchanged"] != nil {
		t.Errorf("import: %v", got)
	}
	var unfinished int
	a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM calibre_import_books WHERE outcome IS DISTINCT FROM 'failed' OR message <> 'metadata.db could not be opened.'`).Scan(&unfinished)
	if unfinished != 0 {
		t.Errorf("%d books not failed with the one message", unfinished)
	}
	if _, after := bookByTitle(t, a, admin, "The Lantern at Dusk (as the file says)"); after["description"] != lantern["description"] {
		t.Error("a book was described although metadata.db could not be read")
	}
	if again, err := a.server.Calibre.Run(context.Background(), uuid.MustParse(id)); err != nil || !again {
		t.Errorf("a finished import run again: %v %v", again, err)
	}
}
