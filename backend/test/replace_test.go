//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestReplacingKeepsOneFileAndCarriesThePlaceOver(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("rita", "reader")
	lib := a.library(admin, "Novels", "shared")
	a.call(admin, http.MethodPut, "/libraries/"+lib+"/members/"+readerID, nil)
	upload := func(name string) (book, file string) {
		t.Helper()
		status, up := a.upload(admin, lib, name, []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload %s: %d %v", name, status, up)
		}
		return up["bookId"].(string), up["fileId"].(string)
	}
	keep, _ := upload("Emma.pdf")
	gone, goneFile := upload("Emma copy.pdf")
	third, _ := upload("Emma again.pdf")
	pair := func(x, y string) string {
		t.Helper()
		if y < x {
			x, y = y, x
		}
		var id string
		if err := a.pool.QueryRow(ctx, `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
			INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, 'title_author', 'emma / austen' FROM p RETURNING pair_id`, x, y).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	replacing := pair(keep, gone)
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+gone+"/progress/ebook", map[string]any{
		"fileId": goneFile, "locator": "page:8", "fraction": 0.8, "clientId": "tablet",
	}); status != 200 {
		t.Fatalf("progress: %d %v", status, out)
	}
	replace := func(c *http.Client, id, book string) int {
		status, _, _ := a.call(c, http.MethodPost, "/duplicates/"+id+"/replace", map[string]any{"keep": book})
		return status
	}

	if status := replace(reader, replacing, keep); status != 403 {
		t.Errorf("a reader replaces: %d", status)
	}
	if status := replace(admin, replacing, third); status != 422 {
		t.Errorf("keep a book of no pair: %d", status)
	}
	if status := replace(admin, uuid.NewString(), keep); status != 404 {
		t.Errorf("no such pair: %d", status)
	}
	if status := replace(admin, replacing, keep); status != 204 {
		t.Fatalf("replace: %d", status)
	}

	// One file left with the book kept; the other is in the trash, as the
	// kept book's, and comes back from there.
	_, book, _ := a.call(admin, http.MethodGet, "/books/"+gone, nil)
	if book["id"] != keep {
		t.Errorf("the replaced book leads to %v", book["id"])
	}
	if files, _ := book["files"].([]any); len(files) != 1 {
		t.Errorf("files after replacing: %v", book["files"])
	}
	trash := a.trash(admin)
	if len(trash.Files) != 1 || trash.Files[0].ID != goneFile || trash.Files[0].BookTitle != "Emma" {
		t.Errorf("trash: %+v", trash)
	}
	var root string
	if err := a.pool.QueryRow(ctx, "SELECT root_path FROM libraries WHERE id = $1", lib).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".trash", goneFile)); err != nil {
		t.Errorf("the file is not in the trash folder: %v", err)
	}

	// Rita's place carries over by how far she was.
	_, progress, _ := a.call(reader, http.MethodGet, "/books/"+keep+"/progress", nil)
	if e, _ := progress["ebook"].(map[string]any); e["locator"] != "fraction:0.8" || e["fraction"] != 0.8 {
		t.Errorf("Rita's place: %v", progress["ebook"])
	}
	var state string
	if err := a.pool.QueryRow(ctx, "SELECT state FROM duplicate_pairs WHERE id = $1", replacing).Scan(&state); err != nil || state != "replaced" {
		t.Errorf("the pair: %q %v", state, err)
	}
	// The replaced book is gone, and with it the pair.
	if status := replace(admin, replacing, keep); status != 404 {
		t.Errorf("replace again: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/files/"+goneFile+"/restore", nil); status != 204 {
		t.Fatalf("restore: %d", status)
	}
	if _, back, _ := a.call(admin, http.MethodGet, "/books/"+keep, nil); len(back["files"].([]any)) != 2 {
		t.Errorf("files after the restore: %v", back["files"])
	}

	// Kept both as another edition: the link stays, and so does the pair's
	// state when the books are checked again.
	keptBoth := pair(keep, third)
	setState := func(body map[string]any) int {
		status, _, _ := a.call(admin, http.MethodPut, "/duplicates/"+keptBoth+"/state", body)
		return status
	}
	if status := setState(map[string]any{"state": "kept_both", "relation": "sequel"}); status != 422 {
		t.Errorf("an unknown relation: %d", status)
	}
	if status := setState(map[string]any{"state": "open", "relation": "edition"}); status != 422 {
		t.Errorf("a relation without keeping both: %d", status)
	}
	if status := setState(map[string]any{"state": "kept_both", "relation": "edition"}); status != 204 {
		t.Fatalf("keep both: %d", status)
	}
	for _, id := range []string{keep, third} {
		if err := a.server.Duplicates.Check(ctx, uuid.MustParse(id)); err != nil {
			t.Fatal(err)
		}
	}
	var kind string
	if err := a.pool.QueryRow(ctx, `SELECT r.kind, p.state FROM book_relations r, duplicate_pairs p
		WHERE p.id = $1 AND r.book_a = p.book_a AND r.book_b = p.book_b`, keptBoth).Scan(&kind, &state); err != nil || kind != "edition" || state != "kept_both" {
		t.Errorf("after checking again: %q %q %v", kind, state, err)
	}
}
