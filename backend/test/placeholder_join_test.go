//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func (a *app) libraryBook(t *testing.T, lib uuid.UUID, title, author string) uuid.UUID {
	t.Helper()
	id, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{
		LibraryID: lib, Title: title, Contributors: []catalog.NewContributor{{Name: author, Role: catalog.RoleAuthor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (a *app) importAll(c *http.Client, library string) (string, map[string]any) {
	a.t.Helper()
	_, preview := a.importFile(c, "goodreads.csv", map[string]string{"library": library})
	id := preview["id"].(string)
	a.call(c, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{})
	a.runImport(id)
	return id, preview
}

func (a *app) placeholderOf(t *testing.T, c *http.Client, title string) string {
	t.Helper()
	_, history, _ := a.call(c, http.MethodGet, "/me/reading?"+url.Values{"q": {title}}.Encode(), nil)
	entries := history["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("history of %s: %v", title, history)
	}
	return entries[0].(map[string]any)["book"].(map[string]any)["id"].(string)
}

func TestAPlaceholderJoinsTheLibrarysBookWhenItGainsTheIsbnForEveryoneWhoReadIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	other, _ := a.signedIn("other", "reader")
	lib := a.library(admin, "Shelf", "shared")
	book := a.libraryBook(t, uuid.MustParse(lib), "Ironbridge Revised", "Ivo Marek")

	_, preview := a.importFile(reader, "goodreads.csv", map[string]string{"library": lib})
	if preview["maybe"] != 1.0 {
		t.Fatalf("a retitled edition is a maybe: %v", preview)
	}
	a.call(reader, http.MethodPost, "/me/reading-imports/"+preview["id"].(string)+"/start", map[string]any{})
	a.runImport(preview["id"].(string))
	a.importAll(other, lib)

	placeholder := a.placeholderOf(t, reader, "Ironbridge")
	if placeholder == book.String() || placeholder != a.placeholderOf(t, other, "Ironbridge") {
		t.Fatalf("one placeholder for both readers, apart from the book: %s %s", placeholder, book)
	}

	status, body, _ := a.call(admin, http.MethodPatch, "/books/"+book.String(), map[string]any{
		"identifiers": []map[string]any{{"type": "isbn", "value": "978-0-19-500102-0"}},
	})
	if status != 200 {
		t.Fatalf("edit: %d %v", status, body)
	}
	if n := a.countRows(`SELECT count(*) FROM river_job WHERE kind = 'ingest.join_placeholders' AND args->>'bookId' = '` + book.String() + `'`); n != 1 {
		t.Fatalf("%d joins queued", n)
	}
	if n, err := a.scans.JoinPlaceholders(ctx, book); err != nil || n != 1 {
		t.Fatalf("joined %d: %v", n, err)
	}

	for name, c := range map[string]*http.Client{"reader": reader, "other": other} {
		if r := standing(t, a, c, book.String()); r["status"] != "completed" || r["rating"] != 4.0 || r["finishedOn"] != "2019-07-02" {
			t.Errorf("%s on the library's book: %v", name, r)
		}
		if id := a.placeholderOf(t, c, "Ironbridge"); id != book.String() {
			t.Errorf("%s: the history names %s, not the library's book", name, id)
		}
		_, again, _ := a.call(c, http.MethodGet, "/books/"+placeholder, nil)
		if again["id"] != book.String() {
			t.Errorf("%s: the placeholder's address leads to %v", name, again["id"])
		}
	}
	var runs int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM curation_runs r JOIN curation_actions x ON x.run_id = r.id
		WHERE r.kind = 'placeholders' AND x.kind = 'merge' AND $1 = ANY(x.books)`, book).Scan(&runs); err != nil || runs != 1 {
		t.Errorf("the join as a curation action: %d %v", runs, err)
	}
	if n, err := a.scans.JoinPlaceholders(ctx, book); err != nil || n != 0 {
		t.Errorf("joining again: %d %v", n, err)
	}
}

func TestAPlaceholderStaysApartFromABookItsReadersCannotSee(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := a.library(admin, "Shelf", "shared")
	private := a.library(admin, "Mine", "private")
	a.importAll(reader, shared)
	book := a.libraryBook(t, uuid.MustParse(private), "Ironbridge", "Ivo Marek")

	if n, err := a.scans.JoinPlaceholders(ctx, book); err != nil || n != 0 {
		t.Fatalf("joined %d: %v", n, err)
	}
	if id := a.placeholderOf(t, reader, "Ironbridge"); id == book.String() {
		t.Error("the reader's history leads into a library they do not see")
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+book.String(), nil); status != 404 {
		t.Errorf("the private book: %d", status)
	}
}

func TestTwoLibraryBooksThatFitAPlaceholderAreForAPersonToChooseBetween(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	a.importAll(reader, lib)
	first := a.libraryBook(t, uuid.MustParse(lib), "Ironbridge", "Ivo Marek")
	a.libraryBook(t, uuid.MustParse(lib), "Ironbridge", "Ivo Marek")

	if n, err := a.scans.JoinPlaceholders(context.Background(), first); err != nil || n != 0 {
		t.Errorf("an ambiguous join: %d %v", n, err)
	}
}

func TestAPersonJoinsAPlaceholderToTheLibrarysBookByHandAndTakesItBack(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	shared := a.library(admin, "Shelf", "shared")
	private := a.library(admin, "Mine", "private")
	book := a.libraryBook(t, uuid.MustParse(shared), "Das Eisenbruecken-Buch", "Ivo Marek")
	hidden := a.libraryBook(t, uuid.MustParse(private), "Ironbridge", "Ivo Marek")
	a.importAll(reader, shared)
	placeholder := a.placeholderOf(t, reader, "Ironbridge")
	path := "/books/" + placeholder + "/join-library-book"

	if status, _, _ := a.call(stranger, http.MethodPost, path, map[string]any{"book": book.String()}); status != 404 {
		t.Errorf("someone who does not read it: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, path, map[string]any{"book": hidden.String()}); status != 404 {
		t.Errorf("a book they do not see: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, path, map[string]any{"book": placeholder}); status != 422 {
		t.Errorf("a book joined to itself: %d", status)
	}
	status, joined, _ := a.call(reader, http.MethodPost, path, map[string]any{"book": book.String()})
	if status != 200 || joined["book"] != book.String() {
		t.Fatalf("join: %d %v", status, joined)
	}
	if r := standing(t, a, reader, book.String()); r["status"] != "completed" || r["rating"] != 4.0 {
		t.Errorf("the reading moved: %v", r)
	}
	action := joined["actionId"].(string)
	if status, _, _ := a.call(stranger, http.MethodPost, "/placeholder-joins/"+action+"/undo", nil); status != 404 {
		t.Errorf("undoing another person's join: %d", status)
	}
	status, undone, _ := a.call(reader, http.MethodPost, "/placeholder-joins/"+action+"/undo", nil)
	if status != 200 || undone["state"] != "undone" {
		t.Fatalf("undo: %d %v", status, undone)
	}
	if id := a.placeholderOf(t, reader, "Ironbridge"); id != placeholder {
		t.Errorf("the placeholder is back: %s", id)
	}
	if r := standing(t, a, reader, book.String()); r["status"] != "unread" {
		t.Errorf("the library's book after the undo: %v", r)
	}
}

func TestTheImportPreviewOffersALooseMatchAndTheAnswerTakesTheLibrarysBook(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	private := a.library(admin, "Mine", "private")
	book := a.libraryBook(t, uuid.MustParse(lib), "Ironbridge Revised", "Ivo Marek")
	a.libraryBook(t, uuid.MustParse(private), "Ironbridge Unseen", "Ivo Marek")

	_, preview := a.importFile(reader, "goodreads.csv", map[string]string{"library": lib})
	maybes := preview["maybes"].([]any)
	if preview["maybe"] != 1.0 || len(maybes) != 1 || preview["new"] != 4.0 {
		t.Fatalf("preview: %v", preview)
	}
	candidates := maybes[0].(map[string]any)["candidates"].([]any)
	if len(candidates) != 1 || candidates[0].(map[string]any)["id"] != book.String() {
		t.Fatalf("only a book they see is offered: %v", candidates)
	}
	id := preview["id"].(string)
	row := int(maybes[0].(map[string]any)["row"].(float64))
	put := "/me/reading-imports/" + id + "/rows/" + string(rune('0'+row))
	if status, body, _ := a.call(reader, http.MethodPut, put, map[string]any{"book": uuid.NewString()}); status != 422 {
		t.Errorf("a book that was not offered: %d %v", status, body)
	}
	if status, body, _ := a.call(reader, http.MethodPut, put, map[string]any{"book": book.String()}); status != 204 {
		t.Fatalf("accepting: %d %v", status, body)
	}
	a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{})
	a.runImport(id)
	if r := standing(t, a, reader, book.String()); r["status"] != "completed" || r["rating"] != 4.0 {
		t.Errorf("the accepted book: %v", r)
	}
	_, history, _ := a.call(reader, http.MethodGet, "/me/reading?"+url.Values{"q": {"ironbridge"}}.Encode(), nil)
	if entries := history["entries"].([]any); len(entries) != 1 || entries[0].(map[string]any)["book"].(map[string]any)["placeholder"] != false {
		t.Errorf("no placeholder beside it: %v", history)
	}
}
