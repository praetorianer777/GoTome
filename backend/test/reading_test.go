//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestEachPersonHasTheirOwnStateOfABook(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	private := uuid.MustParse(a.library(admin, "Mine", "private"))
	emma, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Emma"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{LibraryID: private, Title: "Diary"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/books/" + emma.String()

	status, body, _ := a.call(reader, http.MethodPut, path+"/reading", map[string]any{"status": "reading"})
	today := time.Now().UTC().Format(time.DateOnly)
	if status != 200 || body["status"] != "reading" || body["startedOn"] != today || body["rating"] != nil {
		t.Fatalf("reading: %d %v", status, body)
	}
	a.call(editor, http.MethodPut, path+"/reading", map[string]any{"status": "completed", "rating": 4})

	_, asReader, _ := a.call(reader, http.MethodGet, path, nil)
	if r := asReader["reading"].(map[string]any); r["status"] != "reading" || r["rating"] != nil || r["finishedOn"] != nil {
		t.Errorf("the reader's state: %v", r)
	}
	_, asEditor, _ := a.call(editor, http.MethodGet, path, nil)
	if r := asEditor["reading"].(map[string]any); r["status"] != "completed" || r["rating"].(float64) != 4 || r["finishedOn"] != today {
		t.Errorf("the editor's state: %v", r)
	}
	_, asAdmin, _ := a.call(admin, http.MethodGet, path, nil)
	if r := asAdmin["reading"].(map[string]any); r["status"] != "unread" {
		t.Errorf("the administrator's state: %v", r)
	}

	// The rating goes again; the rest stays.
	_, body, _ = a.call(editor, http.MethodPut, path+"/reading", map[string]any{"rating": 0, "startedOn": "2026-01-02"})
	if body["status"] != "completed" || body["rating"] != nil || body["startedOn"] != "2026-01-02" {
		t.Errorf("after taking the rating away: %v", body)
	}

	for name, req := range map[string]map[string]any{
		"an unknown status": {"status": "skimmed"},
		"six stars":         {"rating": 6},
		"a bad date":        {"finishedOn": "yesterday"},
		"nothing":           {},
	} {
		if status, body, _ := a.call(reader, http.MethodPut, path+"/reading", req); status != 422 {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	if status, _, _ := a.call(reader, http.MethodPut, "/books/"+hidden.String()+"/reading", map[string]any{"status": "reading"}); status != 404 {
		t.Errorf("a book the reader may not see: %d", status)
	}
	_, body, _ = a.call(reader, http.MethodPost, "/books/reading", map[string]any{"books": []uuid.UUID{emma, hidden}, "status": "abandoned"})
	if body["changed"].(float64) != 1 {
		t.Errorf("a bulk change over a hidden book: %v", body)
	}
}

func TestBooksAreFilteredByTheViewersStatusAndRating(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	ids := map[string]uuid.UUID{}
	for _, b := range []struct {
		title string
		tags  []string
	}{{"Emma", []string{"Fiction"}}, {"Persuasion", []string{"Fiction"}}, {"Walden", nil}, {"Ulysses", []string{"Fiction"}}} {
		id, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: b.title, Tags: b.tags})
		if err != nil {
			t.Fatal(err)
		}
		ids[b.title] = id
	}
	set := func(c *http.Client, title string, change map[string]any) {
		t.Helper()
		if status, body, _ := a.call(c, http.MethodPut, "/books/"+ids[title].String()+"/reading", change); status != 200 {
			t.Fatalf("%s: %d %v", title, status, body)
		}
	}
	set(reader, "Emma", map[string]any{"status": "completed", "rating": 5})
	set(reader, "Persuasion", map[string]any{"status": "completed", "rating": 3})
	set(reader, "Walden", map[string]any{"status": "completed", "rating": 5})
	// Another person's state does not count for the reader.
	set(admin, "Ulysses", map[string]any{"status": "completed", "rating": 5})

	list := func(c *http.Client, tree string) []string {
		t.Helper()
		titles, _ := a.listAll(c, url.Values{"filter": {tree}})
		slices.Sort(titles)
		return titles
	}
	if got := list(reader, `{"all":[{"field":"status","op":"in","values":["completed"]},{"field":"tag","op":"in","values":["fiction"]},{"field":"rating","op":"between","values":["4",""]}]}`); !slices.Equal(got, []string{"Emma"}) {
		t.Errorf("completed fiction of four stars or more: %v", got)
	}
	if got := list(reader, `{"all":[{"field":"status","op":"in","values":["unread"]}]}`); !slices.Equal(got, []string{"Ulysses"}) {
		t.Errorf("unread: %v", got)
	}
	if got := list(reader, `{"all":[{"field":"rating","op":"empty"}]}`); !slices.Equal(got, []string{"Ulysses"}) {
		t.Errorf("not rated: %v", got)
	}
	if got := list(admin, `{"all":[{"field":"rating","op":"in","values":["5"]}]}`); !slices.Equal(got, []string{"Ulysses"}) {
		t.Errorf("the administrator's five stars: %v", got)
	}
	for _, tree := range []string{
		`{"all":[{"field":"status","op":"in","values":["skimmed"]}]}`,
		`{"all":[{"field":"rating","op":"in","values":["6"]}]}`,
	} {
		if status, _, _ := a.call(reader, http.MethodGet, "/books?"+url.Values{"filter": {tree}}.Encode(), nil); status != 422 {
			t.Errorf("%s: %d", tree, status)
		}
	}

	_, body, _ := a.call(reader, http.MethodGet, "/books?"+url.Values{"filter": {`{"all":[{"field":"rating","op":"in","values":["3"]}]}`}}.Encode(), nil)
	if b := body["books"].([]any)[0].(map[string]any); b["status"] != "completed" || b["rating"].(float64) != 3 {
		t.Errorf("a summary: %v", b)
	}

	_, body, _ = a.call(reader, http.MethodGet, "/books/facets?"+url.Values{"filter": {`{"all":[{"field":"tag","op":"in","values":["fiction"]}]}`}}.Encode(), nil)
	counts, _ := facetCounts(t, body)
	if c := counts["status"]; c["completed"] != 2 || c["unread"] != 1 {
		t.Errorf("status counts: %v", c)
	}
	if c := counts["rating"]; c["5"] != 1 || c["3"] != 1 || len(c) != 2 {
		t.Errorf("rating counts: %v", c)
	}

	_, body, _ = a.call(reader, http.MethodPost, "/books/reading", map[string]any{
		"filter": `{"all":[{"field":"status","op":"in","values":["completed"]}]}`, "status": "wishlist",
	})
	if body["changed"].(float64) != 3 {
		t.Errorf("bulk: %v", body)
	}
	if got := list(reader, `{"all":[{"field":"status","op":"in","values":["wishlist"]}]}`); len(got) != 3 {
		t.Errorf("after the bulk change: %v", got)
	}
}
