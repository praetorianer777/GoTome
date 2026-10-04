//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestASmartShelfHoldsWhatMatchesNowForWhoeverLooks(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shelf := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	vault := uuid.MustParse(a.library(admin, "Vault", "private"))
	books := catalog.NewService(a.pool)
	book := func(lib uuid.UUID, title, author string) string {
		t.Helper()
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: lib, Title: title,
			Contributors: []catalog.NewContributor{{Name: author, Role: catalog.RoleAuthor}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	mistborn := book(shelf, "Mistborn", "Brandon Sanderson")
	elantris := book(shelf, "Elantris", "Brandon Sanderson")
	warbreaker := book(shelf, "Warbreaker", "Brandon Sanderson")
	emma := book(shelf, "Emma", "Jane Austen")
	book(vault, "The Way of Kings", "Brandon Sanderson")
	set := func(c *http.Client, id string, change map[string]any) {
		t.Helper()
		if status, out, _ := a.call(c, http.MethodPut, "/books/"+id+"/reading", change); status != 200 {
			t.Fatalf("reading %v: %d %v", change, status, out)
		}
	}
	set(reader, mistborn, map[string]any{"rating": 5})
	set(reader, elantris, map[string]any{"rating": 4, "status": "completed"})
	set(reader, warbreaker, map[string]any{"rating": 3})
	set(reader, emma, map[string]any{"rating": 5})

	// Author is Brandon Sanderson AND rating >= 4 AND status is unread.
	rules, _ := json.Marshal(map[string]any{"all": []map[string]any{
		{"field": "author", "op": "in", "values": []string{"Brandon Sanderson"}},
		{"field": "rating", "op": "between", "values": []string{"4", ""}},
		{"field": "status", "op": "in", "values": []string{"unread"}},
	}})
	status, created, _ := a.call(reader, http.MethodPost, "/smart-shelves", map[string]any{"name": "Next Sanderson", "filter": string(rules)})
	if status != 201 || created["books"].(float64) != 1 || created["visibility"] != "private" || created["mine"] != true {
		t.Fatalf("create: %d %v", status, created)
	}
	id := created["id"].(string)
	on := func(c *http.Client, id string) []string {
		t.Helper()
		status, out, _ := a.call(c, http.MethodGet, "/smart-shelves/"+id+"/books", nil)
		if status != 200 {
			t.Fatalf("books: %d %v", status, out)
		}
		var titles []string
		for _, b := range out["books"].([]any) {
			titles = append(titles, b.(map[string]any)["title"].(string))
		}
		return titles
	}
	if got := on(reader, id); !slices.Equal(got, []string{"Mistborn"}) {
		t.Errorf("on the shelf: %v", got)
	}

	// A change of the reader's own state moves books on and off at once.
	set(reader, warbreaker, map[string]any{"rating": 4})
	set(reader, mistborn, map[string]any{"status": "completed"})
	if got := on(reader, id); !slices.Equal(got, []string{"Warbreaker"}) {
		t.Errorf("after rating and finishing: %v", got)
	}
	// So does a change of a book's metadata.
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+emma, map[string]any{
		"contributors": []map[string]string{{"name": "Brandon Sanderson", "role": "author"}},
	}); status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	if got := on(reader, id); !slices.Equal(got, []string{"Emma", "Warbreaker"}) {
		t.Errorf("after the edit: %v", got)
	}
	_, count, _ := a.call(reader, http.MethodGet, "/books/count?"+url.Values{"filter": {string(rules)}}.Encode(), nil)
	if count["count"].(float64) != 2 {
		t.Errorf("the count: %v", count)
	}

	// A shared shelf of the admin's holds, for the reader, the reader's own
	// matches, and never a book of a library the reader may not see.
	unrated, _ := json.Marshal(map[string]any{"all": []map[string]any{
		{"field": "author", "op": "in", "values": []string{"Brandon Sanderson"}},
		{"not": map[string]any{"field": "rating", "op": "empty"}},
	}})
	_, shared, _ := a.call(admin, http.MethodPost, "/smart-shelves", map[string]any{"name": "Rated", "filter": string(unrated), "visibility": "shared"})
	sharedID := shared["id"].(string)
	if shared["books"].(float64) != 0 {
		t.Errorf("the admin rated nothing, yet their shelf holds %v", shared["books"])
	}
	if got := on(reader, sharedID); !slices.Equal(got, []string{"Elantris", "Emma", "Mistborn", "Warbreaker"}) {
		t.Errorf("the reader's view of the shared shelf: %v", got)
	}
	_, list, _ := a.call(reader, http.MethodGet, "/smart-shelves", nil)
	if shelves := list["shelves"].([]any); len(shelves) != 2 || shelves[0].(map[string]any)["id"] != id ||
		shelves[1].(map[string]any)["books"].(float64) != 4 || shelves[1].(map[string]any)["mine"] != false {
		t.Errorf("the reader's list: %v", shelves)
	}
	if status, _, _ := a.call(reader, http.MethodPut, "/smart-shelves/"+sharedID, map[string]any{"name": "Mine", "filter": string(rules)}); status != 403 {
		t.Errorf("the reader changed the admin's shelf: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/smart-shelves/"+id, nil); status != 404 {
		t.Errorf("the admin sees the reader's private shelf: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/smart-shelves/"+id+"/books", nil); status != 404 {
		t.Errorf("the admin lists the reader's private shelf: %d", status)
	}

	// Rules the library's filter refuses are refused on save.
	for name, filter := range map[string]string{
		"an unknown field": `{"field":"colour","op":"in","values":["red"]}`,
		"an unknown op":    `{"field":"author","op":"like","values":["B"]}`,
		"a bad value":      `{"field":"rating","op":"in","values":["6"]}`,
		"an unknown key":   `{"every":[]}`,
		"not JSON":         `author is Sanderson`,
		"no rules":         ``,
	} {
		status, out, _ := a.call(reader, http.MethodPost, "/smart-shelves", map[string]any{"name": "Broken", "filter": filter})
		if fields, _ := out["error"].(map[string]any)["fields"].(map[string]any); status != 422 || fields["filter"] == nil {
			t.Errorf("%s: %d %v", name, status, out)
		}
		if status, _, _ := a.call(reader, http.MethodPut, "/smart-shelves/"+id, map[string]any{"name": "Broken", "filter": filter}); status != 422 {
			t.Errorf("%s on update: %d", name, status)
		}
	}
	if status, out, _ := a.call(reader, http.MethodPost, "/smart-shelves", map[string]any{"name": " ", "filter": "{}"}); status != 422 {
		t.Errorf("no name: %d %v", status, out)
	}

	status, updated, _ := a.call(reader, http.MethodPut, "/smart-shelves/"+id, map[string]any{"name": "Austen", "filter": `{"field":"author","op":"in","values":["jane austen"]}`})
	if status != 200 || updated["name"] != "Austen" || updated["books"].(float64) != 0 {
		t.Errorf("update: %d %v", status, updated)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/smart-shelves/"+id, nil); status != 204 {
		t.Errorf("delete: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/smart-shelves/"+id, nil); status != 404 {
		t.Errorf("after the delete: %d", status)
	}
}
