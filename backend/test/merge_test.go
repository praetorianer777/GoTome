//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestBooksAreMergedWithEveryonesReading(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("rita", "reader")
	other, otherID := a.signedIn("otto", "reader")
	lib := a.library(admin, "Novels", "shared")
	for _, id := range []string{readerID, otherID} {
		a.call(admin, http.MethodPut, "/libraries/"+lib+"/members/"+id, nil)
	}
	upload := func(name string) (book, file string) {
		t.Helper()
		status, up := a.upload(admin, lib, name, []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload %s: %d %v", name, status, up)
		}
		return up["bookId"].(string), up["fileId"].(string)
	}
	keep, _ := upload("Emma.pdf")
	gone, goneFile := upload("Emma Novel.pdf")
	third, _ := upload("Persuasion.pdf")
	edit := func(book string, body map[string]any) {
		t.Helper()
		if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+book, body); status != 200 {
			t.Fatalf("edit: %d %v", status, out)
		}
	}
	edit(keep, map[string]any{"title": "Emma", "contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}}})
	edit(gone, map[string]any{
		"title": "Emma: A Novel", "description": "A heroine whom no one but myself will much like.",
		"identifiers": []map[string]string{{"type": "isbn", "value": "9780141439587"}},
	})
	put := func(c *http.Client, path string, body map[string]any) {
		t.Helper()
		if status, out, _ := a.call(c, http.MethodPut, path, body); status != 200 {
			t.Fatalf("PUT %s: %d %v", path, status, out)
		}
	}
	// Rita read one copy a little and finished the other; Otto only the other.
	put(reader, "/books/"+keep+"/reading", map[string]any{"status": "reading"})
	put(reader, "/books/"+keep+"/progress/ebook", map[string]any{"locator": "page:3", "fraction": 0.3, "clientId": "phone"})
	put(reader, "/books/"+gone+"/progress/ebook", map[string]any{"locator": "page:8", "fraction": 0.8, "clientId": "tablet"})
	put(reader, "/books/"+gone+"/reading", map[string]any{"status": "completed", "rating": 5})
	put(other, "/books/"+gone+"/progress/ebook", map[string]any{"locator": "page:5", "fraction": 0.5, "clientId": "laptop"})
	shelf := func(c *http.Client, name string, books ...string) string {
		t.Helper()
		_, col, _ := a.call(c, http.MethodPost, "/collections", map[string]any{"name": name, "visibility": "private"})
		id := col["id"].(string)
		a.call(c, http.MethodPost, "/collections/"+id+"/books", map[string]any{"books": books})
		return id
	}
	ritaShelf := shelf(reader, "To keep", gone)
	ottoShelf := shelf(other, "Both", keep, gone)
	if _, err := a.pool.Exec(ctx, `INSERT INTO notifications (user_id, kind, data, link, book_id)
		VALUES ($1, 'bulk.finished', '{}', '/books/' || $2, $2::uuid)`, readerID, gone); err != nil {
		t.Fatal(err)
	}
	pairOf := func(x, y string) (string, string) {
		if y < x {
			return y, x
		}
		return x, y
	}
	for _, p := range [][2]string{{keep, gone}, {gone, third}} {
		x, y := pairOf(p[0], p[1])
		if _, err := a.pool.Exec(ctx, `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
			INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, 'title_author', 'emma' FROM p`, x, y); err != nil {
			t.Fatal(err)
		}
	}
	merge := func(c *http.Client, into, from string, take ...string) (int, map[string]any) {
		status, out, _ := a.call(c, http.MethodPost, "/books/"+into+"/merge", map[string]any{"from": from, "take": take})
		return status, out
	}
	snapshot := func() string {
		t.Helper()
		var s string
		if err := a.pool.QueryRow(ctx, `SELECT concat_ws('|',
			(SELECT string_agg(book_id::text, ',' ORDER BY id) FROM book_files WHERE library_id = $1),
			(SELECT string_agg(book_id || status || coalesce(rating::text, ''), ',' ORDER BY user_id, book_id) FROM user_books),
			(SELECT string_agg(book_id || fraction::text, ',' ORDER BY user_id, book_id) FROM reading_progress),
			(SELECT count(*) FROM books WHERE deleted_at IS NOT NULL))`, lib).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if status, _ := merge(reader, keep, gone); status != 403 {
		t.Errorf("a reader merges: %d", status)
	}
	if status, _ := merge(admin, keep, keep); status != 422 {
		t.Errorf("merge into itself: %d", status)
	}
	if status, _ := merge(admin, keep, gone, "files"); status != 422 {
		t.Errorf("take an unknown field: %d", status)
	}
	elsewhere := a.library(admin, "Elsewhere", "shared")
	_, far := a.upload(admin, elsewhere, "Far.pdf", []byte("%PDF-1.4 far"))
	if status, _ := merge(admin, keep, far["bookId"].(string)); status != 422 {
		t.Errorf("merge across libraries: %d", status)
	}

	// A failure at the last step leaves everything as it was.
	before := snapshot()
	if _, err := a.pool.Exec(ctx, `
		CREATE FUNCTION refuse_merge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$;
		CREATE TRIGGER refuse_merge BEFORE UPDATE ON books FOR EACH ROW
			WHEN (NEW.merged_into_id IS NOT NULL AND OLD.merged_into_id IS NULL) EXECUTE FUNCTION refuse_merge()`); err != nil {
		t.Fatal(err)
	}
	if status, _ := merge(admin, keep, gone); status != 500 {
		t.Errorf("a merge that fails: %d", status)
	}
	if after := snapshot(); after != before {
		t.Errorf("a failed merge changed things:\n%s\n%s", before, after)
	}
	if _, err := a.pool.Exec(ctx, "DROP TRIGGER refuse_merge ON books; DROP FUNCTION refuse_merge()"); err != nil {
		t.Fatal(err)
	}

	status, merged := merge(admin, keep, gone, "title")
	if status != 200 || merged["id"] != keep || merged["title"] != "Emma: A Novel" ||
		merged["description"] != "A heroine whom no one but myself will much like." {
		t.Fatalf("merge: %d %v", status, merged)
	}
	if files, _ := merged["files"].([]any); len(files) != 2 {
		t.Errorf("files after the merge: %v", merged["files"])
	}
	if ids, _ := merged["identifiers"].([]any); len(ids) != 1 {
		t.Errorf("identifiers after the merge: %v", merged["identifiers"])
	}
	if authors, _ := merged["contributors"].([]any); len(authors) != 1 {
		t.Errorf("the surviving book's authors: %v", merged["contributors"])
	}

	// Links to the merged book lead to the surviving one.
	if status, old, _ := a.call(admin, http.MethodGet, "/books/"+gone, nil); status != 200 || old["id"] != keep {
		t.Errorf("the old link: %d %v", status, old["id"])
	}

	// No one lost their standing, place or shelves.
	_, rita, _ := a.call(reader, http.MethodGet, "/books/"+keep, nil)
	if r, _ := rita["reading"].(map[string]any); r["status"] != "completed" || r["rating"] != 5.0 {
		t.Errorf("Rita's standing: %v", rita["reading"])
	}
	progress := func(c *http.Client) float64 {
		_, p, _ := a.call(c, http.MethodGet, "/books/"+keep+"/progress", nil)
		e, _ := p["ebook"].(map[string]any)
		f, _ := e["fraction"].(float64)
		return f
	}
	if f := progress(reader); f != 0.8 {
		t.Errorf("Rita's place: %v", f)
	}
	if f := progress(other); f != 0.5 {
		t.Errorf("Otto's place: %v", f)
	}
	onShelf := func(c *http.Client, id string) []string {
		_, col, _ := a.call(c, http.MethodGet, "/collections/"+id, nil)
		var out []string
		books, _ := col["bookList"].([]any)
		for _, b := range books {
			out = append(out, b.(map[string]any)["id"].(string))
		}
		return out
	}
	if got := onShelf(reader, ritaShelf); !slices.Equal(got, []string{keep}) {
		t.Errorf("Rita's shelf: %v", got)
	}
	if got := onShelf(other, ottoShelf); !slices.Equal(got, []string{keep}) {
		t.Errorf("Otto's shelf: %v", got)
	}
	var noteBook, pairState string
	var otherPairs int
	if err := a.pool.QueryRow(ctx, "SELECT book_id::text FROM notifications WHERE user_id = $1", readerID).Scan(&noteBook); err != nil || noteBook != keep {
		t.Errorf("the notification's book: %q %v", noteBook, err)
	}
	x, y := pairOf(keep, gone)
	if err := a.pool.QueryRow(ctx, `SELECT state, (SELECT count(*) FROM duplicate_pairs WHERE $3 IN (book_a, book_b) AND state = 'open')
		FROM duplicate_pairs WHERE book_a = $1 AND book_b = $2`, x, y, gone).Scan(&pairState, &otherPairs); err != nil || pairState != "merged" || otherPairs != 0 {
		t.Errorf("pairs: %q, %d open of the merged book, %v", pairState, otherPairs, err)
	}
	var fileBook uuid.UUID
	if err := a.pool.QueryRow(ctx, "SELECT book_id FROM book_files WHERE id = $1", goneFile).Scan(&fileBook); err != nil || fileBook.String() != keep {
		t.Errorf("the merged book's file belongs to %v, %v", fileBook, err)
	}
}
