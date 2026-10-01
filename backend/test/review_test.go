//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
)

func TestDoubtfulMatchesAreReviewed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	m, _ := a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	shared := uuid.MustParse(a.library(admin, "Shared", "shared"))
	hidden := uuid.MustParse(a.library(admin, "Hidden", "private"))
	ctx := context.Background()
	// Same title, another author: each a doubtful match of Jane Austen's.
	doubtful := func(lib uuid.UUID, author string) uuid.UUID {
		id, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: "Emma", Contributors: []catalog.NewContributor{{Name: author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Match(ctx, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second, secret := doubtful(shared, "Emma Donoghue"), doubtful(shared, "Emma Tennant"), doubtful(hidden, "Emma Thompson")

	if status, _, _ := a.call(reader, http.MethodGet, "/matches", nil); status != 403 {
		t.Errorf("a reader reviews: %d", status)
	}
	queue := func(c *http.Client) ([]any, float64) {
		status, body, _ := a.call(c, http.MethodGet, "/matches", nil)
		if status != 200 {
			t.Fatalf("queue: %d %v", status, body)
		}
		return body["books"].([]any), body["total"].(float64)
	}
	books, total := queue(editor)
	if len(books) != 2 || total != 2 {
		t.Fatalf("the editor's queue: %d books, total %v", len(books), total)
	}
	if _, total := queue(admin); total != 3 {
		t.Errorf("the administrator's queue: total %v", total)
	}
	item := books[0].(map[string]any)
	if item["book"].(map[string]any)["id"] != first.String() {
		t.Errorf("the longest waiting first: %v", item["book"])
	}
	match := item["matches"].([]any)[0].(map[string]any)
	if match["provider"] != "shelf" || match["title"] != "Emma" || match["coverToken"] == nil {
		t.Errorf("match %v", match)
	}

	// Hidden: as if it were not there.
	var hiddenMatch uuid.UUID
	if err := a.pool.QueryRow(ctx, `SELECT id FROM metadata_matches WHERE book_id = $1`, secret).Scan(&hiddenMatch); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"accept", "reject"} {
		if status, _, _ := a.call(editor, http.MethodPost, "/matches/"+hiddenMatch.String()+"/"+action, map[string]any{}); status != 404 {
			t.Errorf("%s a hidden match: %d", action, status)
		}
	}

	status, body, _ := a.call(editor, http.MethodPost, "/matches/"+match["matchId"].(string)+"/accept", map[string]any{
		"publisher": "John Murray", "coverToken": match["coverToken"],
	})
	if status != 200 || body["publisher"] != "John Murray" || body["coverKey"] == nil || field(body, "publisher")["source"] != "provider" {
		t.Fatalf("accept: %d %v", status, body)
	}
	if got := a.matchStates(first); len(got) != 1 || got[0] != enrich.StateApplied {
		t.Errorf("the accepted match: %v", got)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/matches/"+match["matchId"].(string)+"/accept", map[string]any{}); status != 404 {
		t.Errorf("accept twice: %d", status)
	}

	books, _ = queue(editor)
	other := books[0].(map[string]any)["matches"].([]any)[0].(map[string]any)["matchId"].(string)
	if status, _, _ := a.call(editor, http.MethodPost, "/matches/"+other+"/reject", nil); status != 204 {
		t.Errorf("reject: %d", status)
	}
	// Looked up again, the rejected match is not proposed again.
	if err := m.Match(ctx, second); err != nil {
		t.Fatal(err)
	}
	if books, total := queue(editor); len(books) != 0 || total != 0 {
		t.Errorf("after accepting and rejecting: %v", books)
	}
	if book := a.book(second); book.Publisher != "" {
		t.Errorf("the rejected match changed the book: %+v", book)
	}
}
