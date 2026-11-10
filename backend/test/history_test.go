//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

func (a *app) importFile(c *http.Client, name string, fields map[string]string) (int, map[string]any) {
	a.t.Helper()
	data, err := os.ReadFile("../internal/readimport/testdata/" + name)
	if err != nil {
		a.t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = form.WriteField(k, v)
	}
	w, _ := form.CreateFormFile("file", name)
	_, _ = w.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, a.url+httpapi.APIPrefix+"/me/reading-imports", &body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded
}

func (a *app) runImport(id string) {
	a.t.Helper()
	for {
		done, err := a.server.Imports.Run(context.Background(), uuid.MustParse(id))
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			return
		}
	}
}

func (a *app) historyBooks(t *testing.T, lib uuid.UUID) (dune, emma1, emma2 uuid.UUID) {
	t.Helper()
	books := catalog.NewService(a.pool)
	make := func(title, author string, isbn string, year string) uuid.UUID {
		in := catalog.NewBook{LibraryID: lib, Title: title, Published: year,
			Contributors: []catalog.NewContributor{{Name: author, Role: catalog.RoleAuthor}}}
		if isbn != "" {
			in.Identifiers = []catalog.Identifier{{Type: catalog.IDISBN, Value: isbn}}
		}
		id, err := books.CreateBook(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	return make("Dune", "Frank Herbert", "9780441013593", "1965"), make("Emma", "Jane Austen", "", "1815"), make("Emma", "Jane Austen", "", "1816")
}

func standing(t *testing.T, a *app, c *http.Client, id string) map[string]any {
	t.Helper()
	_, book, _ := a.call(c, http.MethodGet, "/books/"+id, nil)
	return book["reading"].(map[string]any)
}

func TestAnImportIsPreviewedThenSetAndRunningItAgainChangesNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	dune, emma1, emma2 := a.historyBooks(t, lib)

	status, preview := a.importFile(reader, "goodreads.csv", nil)
	if status != 201 || preview["source"] != "goodreads" || preview["state"] != "preview" ||
		preview["matched"] != 1.0 || preview["new"] != 3.0 || preview["ambiguous"] != 1.0 || preview["skipped"] != 1.0 {
		t.Fatalf("preview: %d %v", status, preview)
	}
	id := preview["id"].(string)
	if r := standing(t, a, reader, dune.String()); r["status"] != "unread" {
		t.Errorf("the preview wrote: %v", r)
	}
	ambiguity := preview["ambiguities"].([]any)[0].(map[string]any)
	candidates := ambiguity["candidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("candidates: %v", ambiguity)
	}
	row := int(ambiguity["row"].(float64))
	path := "/me/reading-imports/" + id + "/rows/"
	if status, body, _ := a.call(reader, http.MethodPut, path+"3", map[string]any{"book": uuid.NewString()}); status != 422 {
		t.Errorf("a book that is none of the candidates: %d %v", status, body)
	}
	if status, body, _ := a.call(reader, http.MethodPut, path+"3", map[string]any{"book": emma2.String()}); status != 204 || row != 3 {
		t.Errorf("choosing: %d %v", status, body)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/me/reading-imports/"+id, nil); status != 404 {
		t.Errorf("another person's import: %d", status)
	}

	if status, body, _ := a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{}); status != 202 {
		t.Fatalf("start: %d %v", status, body)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{}); status != 409 {
		t.Errorf("starting twice: %d", status)
	}
	a.runImport(id)
	_, done, _ := a.call(reader, http.MethodGet, "/me/reading-imports/"+id, nil)
	outcomes := done["outcomes"].(map[string]any)
	if done["state"] != "done" || outcomes["created"] != 3.0 || outcomes["changed"] != 2.0 || outcomes["skipped"] != 1.0 {
		t.Errorf("outcomes: %v", done)
	}

	r := standing(t, a, reader, dune.String())
	if r["status"] != "completed" || r["rating"] != 5.0 || r["finishedOn"] != "2021-03-14" || r["note"] != "Spice must flow." {
		t.Errorf("Dune: %v", r)
	}
	if r := standing(t, a, reader, emma2.String()); r["status"] != "completed" || r["rating"] != 3.0 {
		t.Errorf("the chosen Emma: %v", r)
	}
	if r := standing(t, a, reader, emma1.String()); r["status"] != "unread" {
		t.Errorf("the other Emma: %v", r)
	}
	if r := standing(t, a, admin, dune.String()); r["status"] != "unread" || r["note"] != nil {
		t.Errorf("another person's Dune: %v", r)
	}

	_, history, _ := a.call(reader, http.MethodGet, "/me/reading?"+url.Values{"status": {"completed"}}.Encode(), nil)
	entries := history["entries"].([]any)
	if len(entries) != 3 || !slices.Equal(history["years"].([]any), []any{2026.0, 2021.0, 2019.0, 2018.0}) {
		t.Fatalf("history: %v", history)
	}
	_, history, _ = a.call(reader, http.MethodGet, "/me/reading?"+url.Values{"year": {"2019"}, "q": {"asimov"}}.Encode(), nil)
	entries = history["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["book"].(map[string]any)["placeholder"] != true {
		t.Errorf("Foundation, a book the library lacks: %v", history)
	}
	_, history, _ = a.call(reader, http.MethodGet, "/me/reading?stars=5", nil)
	if len(history["entries"].([]any)) != 1 {
		t.Errorf("five stars: %v", history)
	}
	if _, history, _ = a.call(admin, http.MethodGet, "/me/reading", nil); len(history["entries"].([]any)) != 0 {
		t.Errorf("another person's history: %v", history)
	}
	if titles, _ := a.listAll(reader, url.Values{}); len(titles) != 3 {
		t.Errorf("placeholders are not in the library: %v", titles)
	}
	_, stats, _ := a.call(reader, http.MethodGet, "/me/stats?tz=UTC", nil)
	years := stats["years"].([]any)
	if len(years) != 3 {
		t.Errorf("finishes count in their years: %v", years)
	}
	if stats["undated"] != 0.0 {
		t.Errorf("every imported read has its date: %v", stats["undated"])
	}
	if status, body, _ := a.call(reader, http.MethodPut, "/books/"+emma1.String()+"/reading", map[string]any{"status": "completed"}); status != http.StatusOK {
		t.Fatalf("completing: %d %v", status, body)
	}
	if _, stats, _ = a.call(reader, http.MethodGet, "/me/stats?tz=UTC", nil); stats["undated"] != 1.0 || len(stats["years"].([]any)) != 3 {
		t.Errorf("a book completed with no date is counted apart: %v", stats)
	}
	_, shelves, _ := a.call(reader, http.MethodGet, "/collections", nil)
	names := []string{}
	for _, c := range shelves["collections"].([]any) {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"fantasy", "favorites", "sci-fi"}) {
		t.Errorf("shelves: %v", names)
	}
	var finishes, books int
	count := func() {
		if err := a.pool.QueryRow(context.Background(),
			`SELECT (SELECT count(*) FROM reading_finishes), (SELECT count(*) FROM books WHERE deleted_at IS NULL)`).Scan(&finishes, &books); err != nil {
			t.Fatal(err)
		}
	}
	count()
	finishesBefore, booksBefore := finishes, books

	// The same file again: the placeholders are matched, nothing is made or
	// changed.
	_, again := a.importFile(reader, "goodreads.csv", nil)
	if again["matched"] != 4.0 || again["new"] != 0.0 || again["ambiguous"] != 1.0 {
		t.Fatalf("second preview: %v", again)
	}
	id = again["id"].(string)
	a.call(reader, http.MethodPut, "/me/reading-imports/"+id+"/rows/3", map[string]any{"book": emma2.String()})
	a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{})
	a.runImport(id)
	_, second, _ := a.call(reader, http.MethodGet, "/me/reading-imports/"+id, nil)
	outcomes = second["outcomes"].(map[string]any)
	if outcomes["unchanged"] != 5.0 || outcomes["created"] != nil || outcomes["changed"] != nil {
		t.Errorf("second outcomes: %v", second)
	}
	count()
	if finishes != finishesBefore || books != booksBefore {
		t.Errorf("finishes %d to %d, books %d to %d", finishesBefore, finishes, booksBefore, books)
	}
}

func TestAnImportKeepsWhatThePersonSetUnlessToldToOverwrite(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	dune, _, _ := a.historyBooks(t, lib)
	path := "/books/" + dune.String() + "/reading"
	a.call(reader, http.MethodPut, path, map[string]any{"status": "completed", "rating": 2, "finishedOn": "2020-01-01"})

	run := func(overwrite bool) {
		_, preview := a.importFile(reader, "storygraph.csv", nil)
		id := preview["id"].(string)
		a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{"overwrite": overwrite})
		a.runImport(id)
	}
	run(false)
	if r := standing(t, a, reader, dune.String()); r["rating"] != 2.0 || r["finishedOn"] != "2020-01-01" {
		t.Errorf("without overwrite: %v", r)
	}
	run(true)
	if r := standing(t, a, reader, dune.String()); r["rating"] != 5.0 || r["finishedOn"] != "2021-03-14" {
		t.Errorf("with overwrite: %v", r)
	}
}

func TestAnImportOnlyMatchesBooksOfLibrariesThePersonSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	private := uuid.MustParse(a.library(admin, "Mine", "private"))
	dune, _, _ := a.historyBooks(t, private)

	_, preview := a.importFile(reader, "goodreads.csv", map[string]string{"library": shared.String()})
	if preview["matched"] != 0.0 || preview["new"] != 5.0 {
		t.Errorf("a private library's book: %v", preview)
	}
	id := preview["id"].(string)
	a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{})
	a.runImport(id)
	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+dune.String(), nil); status != 404 {
		t.Errorf("the private book: %d", status)
	}
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_books WHERE book_id = $1`, dune).Scan(&n); err != nil || n != 0 {
		t.Errorf("state on a book the person does not see: %d %v", n, err)
	}
	if status, body := a.importFile(reader, "goodreads.csv", map[string]string{"library": private.String()}); status != 422 {
		t.Errorf("a library they do not see: %d %v", status, body)
	}
}

func TestABookReadButNotOwnedIsAddedFromTheSearchAndNotesStayPrivate(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	other, _ := a.signedIn("other", "reader")
	lib := a.library(admin, "Shelf", "shared")
	a.withProvider()

	status, book, _ := a.call(reader, http.MethodPost, "/books/wishes", map[string]any{
		"provider": "shelf", "title": "Emma", "status": "completed", "rating": 4, "finishedOn": "2017-05-06",
		"contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}},
	})
	if status != 201 || book["placeholder"] != true || book["libraryId"] != lib {
		t.Fatalf("add as read: %d %v", status, book)
	}
	r := book["reading"].(map[string]any)
	if r["status"] != "completed" || r["rating"] != 4.0 || r["finishedOn"] != "2017-05-06" {
		t.Errorf("reading: %v", r)
	}
	id := book["id"].(string)
	_, stats, _ := a.call(reader, http.MethodGet, "/me/stats?tz=UTC", nil)
	if years := stats["years"].([]any); len(years) != 1 || years[0].(map[string]any)["year"] != 2017.0 {
		t.Errorf("stats: %v", stats["years"])
	}

	if status, _, _ := a.call(reader, http.MethodPut, "/books/"+id+"/note", map[string]any{"note": "Lent by my aunt."}); status != 204 {
		t.Errorf("note: %d", status)
	}
	if r := standing(t, a, reader, id); r["note"] != "Lent by my aunt." {
		t.Errorf("the note: %v", r)
	}
	if r := standing(t, a, other, id); r["note"] != nil || r["status"] != "unread" {
		t.Errorf("another person sees: %v", r)
	}
	a.call(reader, http.MethodPut, "/books/"+id+"/note", map[string]any{"note": ""})
	if r := standing(t, a, reader, id); r["note"] != nil {
		t.Errorf("the note after removing: %v", r)
	}
}

func TestMergingBooksKeepsEachPersonsNotesAndReadFinishes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	_, emma1, emma2 := a.historyBooks(t, lib)
	a.call(editor, http.MethodPut, "/books/"+emma1.String()+"/note", map[string]any{"note": "First."})
	a.call(editor, http.MethodPut, "/books/"+emma2.String()+"/note", map[string]any{"note": "Second."})
	a.call(editor, http.MethodPut, "/books/"+emma2.String()+"/reading", map[string]any{"status": "completed", "finishedOn": "2020-02-02"})
	if _, err := a.pool.Exec(context.Background(),
		`INSERT INTO reading_finishes (user_id, book_id, medium, finished_at) SELECT id, $1, 'ebook', '2020-02-02 12:00+00' FROM users WHERE username = 'editor'`, emma2); err != nil {
		t.Fatal(err)
	}
	if status, body, _ := a.call(editor, http.MethodPost, "/books/"+emma1.String()+"/merge", map[string]any{"from": emma2.String()}); status != 200 {
		t.Fatalf("merge: %d %v", status, body)
	}
	if r := standing(t, a, editor, emma1.String()); r["note"] != "First.\n\nSecond." || r["status"] != "completed" {
		t.Errorf("after the merge: %v", r)
	}
	var finishes int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM reading_finishes WHERE book_id = $1`, emma1).Scan(&finishes); err != nil || finishes != 1 {
		t.Errorf("finishes: %d %v", finishes, err)
	}
}

func TestBooksAnImportMadeAreQueuedForALookup(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	m, _ := a.matcher()
	a.server.Imports.Lookup = m.EnqueueTx
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	_, _, emma2 := a.historyBooks(t, lib)

	_, preview := a.importFile(reader, "goodreads.csv", nil)
	id := preview["id"].(string)
	a.call(reader, http.MethodPut, "/me/reading-imports/"+id+"/rows/3", map[string]any{"book": emma2.String()})
	a.call(reader, http.MethodPost, "/me/reading-imports/"+id+"/start", map[string]any{})
	a.runImport(id)

	var lookups, onHeld int
	if err := a.pool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE b.placeholder),
		       count(*) FILTER (WHERE NOT b.placeholder)
		FROM river_job j JOIN books b ON b.id = (j.args->>'bookId')::uuid
		WHERE j.kind = 'enrich.match_book'`).Scan(&lookups, &onHeld); err != nil {
		t.Fatal(err)
	}
	if lookups != 3 || onHeld != 0 {
		t.Errorf("lookups queued: %d for new books, %d for held ones; want 3 and 0", lookups, onHeld)
	}
}
