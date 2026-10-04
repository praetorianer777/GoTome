//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestCollectionsShowEachViewerOnlyWhatTheyMaySee(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shelf := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	vault := uuid.MustParse(a.library(admin, "Vault", "private"))
	books := catalog.NewService(a.pool)
	book := func(lib uuid.UUID, title string) string {
		t.Helper()
		id, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: title})
		if err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	emma, persuasion, secret := book(shelf, "Emma"), book(shelf, "Persuasion"), book(vault, "Secret")

	create := func(c *http.Client, body map[string]any) map[string]any {
		t.Helper()
		status, out, _ := a.call(c, http.MethodPost, "/collections", body)
		if status != 201 {
			t.Fatalf("create %v: %d %v", body, status, out)
		}
		return out
	}
	titles := func(c map[string]any) []string {
		var out []string
		for _, b := range c["bookList"].([]any) {
			out = append(out, b.(map[string]any)["title"].(string))
		}
		return out
	}
	add := func(c *http.Client, id string, body map[string]any) int {
		t.Helper()
		status, out, _ := a.call(c, http.MethodPost, "/collections/"+id+"/books", body)
		if status != 200 {
			t.Fatalf("add %v: %d %v", body, status, out)
		}
		return int(out["added"].(float64))
	}

	shared := create(admin, map[string]any{"name": "  Favourites ", "visibility": "shared"})
	if shared["name"] != "Favourites" || shared["visibility"] != "shared" || shared["mine"] != true {
		t.Errorf("the new collection: %v", shared)
	}
	sharedID := shared["id"].(string)
	if n := add(admin, sharedID, map[string]any{"books": []string{emma, secret, persuasion, emma}}); n != 3 {
		t.Errorf("added %d, want 3", n)
	}
	private := create(admin, map[string]any{"name": "Notes"})
	if private["visibility"] != "private" {
		t.Errorf("a collection is private unless said otherwise: %v", private)
	}
	privateID := private["id"].(string)
	add(admin, privateID, map[string]any{"books": []string{emma}})

	// The owner sees everything; the reader the shared collection, without
	// the book of a library they may not see.
	_, got, _ := a.call(admin, http.MethodGet, "/collections/"+sharedID, nil)
	if !slices.Equal(titles(got), []string{"Emma", "Secret", "Persuasion"}) {
		t.Errorf("the owner sees %v", titles(got))
	}
	_, got, _ = a.call(reader, http.MethodGet, "/collections/"+sharedID, nil)
	if !slices.Equal(titles(got), []string{"Emma", "Persuasion"}) || got["books"].(float64) != 2 || got["mine"] != false {
		t.Errorf("the reader sees %v", got)
	}
	_, list, _ := a.call(reader, http.MethodGet, "/collections", nil)
	cs := list["collections"].([]any)
	if len(cs) != 1 || cs[0].(map[string]any)["id"] != sharedID || cs[0].(map[string]any)["books"].(float64) != 2 ||
		cs[0].(map[string]any)["ownerName"] != "admin" {
		t.Errorf("the reader's list: %v", cs)
	}

	// A private collection is not there for anyone else, even to change.
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/collections/" + privateID, nil},
		{http.MethodPut, "/collections/" + privateID, map[string]any{"name": "Mine now"}},
		{http.MethodDelete, "/collections/" + privateID, nil},
		{http.MethodPost, "/collections/" + privateID + "/books", map[string]any{"books": []string{persuasion}}},
		{http.MethodGet, "/collections/" + uuid.NewString(), nil},
	} {
		if status, out, _ := a.call(reader, call.method, call.path, call.body); status != 404 {
			t.Errorf("%s %s: %d %v", call.method, call.path, status, out)
		}
	}
	// A shared one may be looked at, not changed.
	if status, _, _ := a.call(reader, http.MethodPut, "/collections/"+sharedID+"/order", map[string]any{"books": []string{persuasion, emma}}); status != 403 {
		t.Errorf("the reader reordered the admin's collection: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+sharedID+"/books/"+emma, nil); status != 403 {
		t.Errorf("the reader took a book out of the admin's collection: %d", status)
	}

	// Reordering persists, and what the owner does not name stays put.
	status, got, _ := a.call(admin, http.MethodPut, "/collections/"+sharedID+"/order", map[string]any{"books": []string{persuasion, emma}})
	if status != 200 || !slices.Equal(titles(got), []string{"Persuasion", "Secret", "Emma"}) {
		t.Errorf("after the reorder: %d %v", status, titles(got))
	}
	_, got, _ = a.call(reader, http.MethodGet, "/collections/"+sharedID, nil)
	if !slices.Equal(titles(got), []string{"Persuasion", "Emma"}) {
		t.Errorf("the reader sees the new order as %v", titles(got))
	}

	// The reader's own collection, filled from a list: the book they may
	// not see is not added, named or not.
	mine := create(reader, map[string]any{"name": "To read", "description": "Next up"})["id"].(string)
	if n := add(reader, mine, map[string]any{"library": shelf.String()}); n != 2 {
		t.Errorf("added %d from the library, want 2", n)
	}
	if n := add(reader, mine, map[string]any{"books": []string{secret, emma}}); n != 0 {
		t.Errorf("added %d more, want 0", n)
	}
	for name, order := range map[string][]string{
		"twice":           {emma, emma},
		"not in it":       {uuid.NewString()},
		"a book unseen":   {secret},
		"a list of names": {persuasion, emma},
	} {
		status, out, _ := a.call(reader, http.MethodPut, "/collections/"+mine+"/order", map[string]any{"books": order})
		want := 422
		if name == "a list of names" {
			want = 200
		}
		if status != want {
			t.Errorf("reorder %s: %d %v", name, status, out)
		}
	}
	_, list, _ = a.call(reader, http.MethodGet, "/collections?book="+emma, nil)
	has := map[string]bool{}
	for _, c := range list["collections"].([]any) {
		c := c.(map[string]any)
		has[c["name"].(string)] = c["hasBook"].(bool)
	}
	if !has["To read"] || !has["Favourites"] || len(has) != 2 {
		t.Errorf("which hold Emma: %v", has)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+mine+"/books/"+emma, nil); status != 204 {
		t.Errorf("taking a book out: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+mine+"/books/"+emma, nil); status != 404 {
		t.Errorf("taking it out again: %d", status)
	}

	// Made private, the admin's collection is gone for the reader.
	if status, out, _ := a.call(admin, http.MethodPut, "/collections/"+sharedID, map[string]any{"name": "Favourites", "visibility": "private"}); status != 200 {
		t.Fatalf("making it private: %d %v", status, out)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/collections/"+sharedID, nil); status != 404 {
		t.Errorf("a collection made private: %d", status)
	}
	for name, body := range map[string]map[string]any{
		"no name":            {"name": " "},
		"another visibility": {"name": "x", "visibility": "public"},
	} {
		if status, out, _ := a.call(reader, http.MethodPost, "/collections", body); status != 422 {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+mine, nil); status != 204 {
		t.Errorf("deleting: %d", status)
	}
	if _, got, _ := a.call(reader, http.MethodGet, "/books/"+persuasion, nil); got["title"] != "Persuasion" {
		t.Errorf("the book went with the collection: %v", got)
	}
}
