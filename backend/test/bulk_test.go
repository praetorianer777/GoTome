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

// matchesOf looks books up with whatever matcher the app's server has when
// it is asked.
type matchesOf struct{ a *app }

func (m matchesOf) Fetch(ctx context.Context, id uuid.UUID) (string, error) {
	return m.a.server.Matches.Fetch(ctx, id)
}

// runBulk starts a bulk change, works it to the end and returns what it
// came to.
func (a *app) runBulk(c *http.Client, req map[string]any) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/books/bulk", req)
	if status != http.StatusAccepted {
		a.t.Fatalf("start %v: %d %v\n%s", req, status, body, a.logs.String())
	}
	id := uuid.MustParse(body["id"].(string))
	for {
		done, err := a.server.Bulk.Run(context.Background(), id)
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			break
		}
	}
	status, body, _ = a.call(c, http.MethodGet, "/bulk/"+id.String(), nil)
	if status != 200 || body["finishedAt"] == nil {
		a.t.Fatalf("status: %d %v", status, body)
	}
	return body
}

// outcomes are the bulk change's books by title, each with its outcome and
// the fields it skipped.
func outcomes(status map[string]any) map[string]string {
	out := map[string]string{}
	for _, b := range status["books"].([]any) {
		r := b.(map[string]any)
		outcome, _ := r["outcome"].(string)
		for _, s := range r["skipped"].([]any) {
			outcome += " " + s.(string)
		}
		out[r["title"].(string)] = outcome
	}
	return out
}

func TestABulkEditChangesTheFilteredBooksAndSkipsLockedFields(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	for i, title := range []string{"Mort", "Sourcery", "Eric", "Emma"} {
		in := catalog.NewBook{LibraryID: uuid.MustParse(lib), Title: title, Tags: []string{"Fantasy", "Paperback"},
			Contributors: []catalog.NewContributor{{Name: "Terry Pratchet"}, {Name: "Nigel Planer", Role: catalog.RoleNarrator}}}
		if title != "Emma" {
			index := float64(i + 4)
			in.Series, in.SeriesIndex = "Discworl", &index
		}
		id, err := books.CreateBook(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	// A person set Eric's series by hand; Sourcery's tags and authors too.
	a.call(editor, http.MethodPatch, "/books/"+ids["Eric"].String(), map[string]any{"locks": map[string]bool{"series": true}})
	a.call(editor, http.MethodPatch, "/books/"+ids["Sourcery"].String(), map[string]any{"locks": map[string]bool{"series": true, "tags": true, "contributors": true}})

	edit := map[string]any{
		"filter": `{"all":[{"field":"series","op":"in","values":["Discworl"]}]}`, "library": lib, "action": "edit",
		"change": map[string]any{
			"series": "Discworld", "authors": []string{"Terry Pratchett"},
			"addTags": []string{"Humour", "fantasy"}, "removeTags": []string{"Paperback"},
		},
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/books/bulk", edit); status != 403 {
		t.Errorf("a reader edits in bulk: %d", status)
	}
	status := a.runBulk(editor, edit)
	if status["total"].(float64) != 3 || status["done"].(float64) != 3 {
		t.Errorf("total and done: %v", status)
	}
	want := map[string]string{"Mort": "changed", "Eric": "changed series", "Sourcery": "locked contributors series tags"}
	if got := outcomes(status); !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}

	mort := a.book(ids["Mort"])
	if mort.Series != "Discworld" || mort.SeriesIndex == nil || *mort.SeriesIndex != 4 {
		t.Errorf("Mort's series: %q %v", mort.Series, mort.SeriesIndex)
	}
	if !slices.Equal(mort.Authors(), []string{"Terry Pratchett"}) || len(mort.Contributors) != 2 {
		t.Errorf("Mort's contributors: %+v", mort.Contributors)
	}
	if slices.Sort(mort.Tags); !slices.Equal(mort.Tags, []string{"Fantasy", "Humour"}) {
		t.Errorf("Mort's tags: %v", mort.Tags)
	}
	if !slices.Contains(mort.Locked, "series") || mort.Sources["tags"] != catalog.SourceManual {
		t.Errorf("Mort's locks %v and sources %v", mort.Locked, mort.Sources)
	}
	if eric := a.book(ids["Eric"]); eric.Series != "Discworl" || !slices.Equal(eric.Authors(), []string{"Terry Pratchett"}) {
		t.Errorf("Eric: %q %v", eric.Series, eric.Authors())
	}
	if emma := a.book(ids["Emma"]); emma.Series != "" || !slices.Equal(emma.Authors(), []string{"Terry Pratchet"}) || len(emma.Tags) != 2 {
		t.Errorf("Emma, not selected, was changed: %+v", emma)
	}

	// Once more, locked fields included: Sourcery follows; Mort is as the
	// change would leave it.
	edit["filter"] = `{"all":[{"field":"author","op":"in","values":["Terry Pratchett", "Terry Pratchet"]}]}`
	edit["change"].(map[string]any)["includeLocked"] = true
	delete(edit["change"].(map[string]any), "authors")
	status = a.runBulk(editor, edit)
	want = map[string]string{"Mort": "unchanged", "Eric": "changed", "Sourcery": "changed", "Emma": "changed"}
	if got := outcomes(status); !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}

	if status, _, _ := a.call(admin, http.MethodGet, "/bulk/"+status["id"].(string), nil); status != 404 {
		t.Errorf("somebody else's bulk change: %d", status)
	}
	for name, req := range map[string]map[string]any{
		"no action":        {"books": []uuid.UUID{ids["Mort"]}},
		"nothing to do":    {"books": []uuid.UUID{ids["Mort"]}, "action": "edit", "change": map[string]any{}},
		"a bad language":   {"books": []uuid.UUID{ids["Mort"]}, "action": "edit", "change": map[string]any{"language": "not a language at all"}},
		"a fetch's change": {"books": []uuid.UUID{ids["Mort"]}, "action": "fetch", "change": map[string]any{"series": "X"}},
		"no books":         {"books": []uuid.UUID{uuid.New()}, "action": "writeBack"},
		"a bad filter":     {"filter": `{"field":"colour","op":"in","values":["red"]}`, "action": "writeBack"},
	} {
		if status, body, _ := a.call(editor, http.MethodPost, "/books/bulk", req); status != 422 {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
}

func sameOutcomes(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestABulkChangeReportsEachBookAndGoesOnPastFailures(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	for _, in := range []catalog.NewBook{
		{Title: "Emma", Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780141439587"}}},
		{Title: "A Book Nobody Knows"},
		{Title: "Gone"},
	} {
		in.LibraryID = lib
		id, err := books.CreateBook(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		ids[in.Title] = id
	}
	selection := []uuid.UUID{ids["Gone"], ids["Emma"], ids["A Book Nobody Knows"]}

	status, body, _ := a.call(editor, http.MethodPost, "/books/bulk", map[string]any{"books": selection, "action": "fetch"})
	if status != http.StatusAccepted {
		t.Fatalf("start: %d %v", status, body)
	}
	// The book goes after it was chosen.
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET deleted_at = now() WHERE id = $1`, ids["Gone"]); err != nil {
		t.Fatal(err)
	}
	id := body["id"].(string)
	for done := false; !done; {
		var err error
		if done, err = a.server.Bulk.Run(context.Background(), uuid.MustParse(id)); err != nil {
			t.Fatal(err)
		}
	}
	_, body, _ = a.call(editor, http.MethodGet, "/bulk/"+id, nil)
	want := map[string]string{"Emma": "changed", "A Book Nobody Knows": "notFound", "Gone": "failed"}
	if got := outcomes(body); !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	for _, b := range body["books"].([]any) {
		if r := b.(map[string]any); r["outcome"] == "failed" && r["message"] == nil {
			t.Errorf("the failure says nothing: %v", r)
		}
	}
	if counts := body["counts"].(map[string]any); counts["failed"].(float64) != 1 || counts["changed"].(float64) != 1 {
		t.Errorf("counts %v", counts)
	}
	if emma := a.book(ids["Emma"]); emma.Publisher != "John Murray" || emma.Sources["publisher"] != "provider:shelf" {
		t.Errorf("Emma after the lookup: %q %v", emma.Publisher, emma.Sources)
	}

	// Nothing of these lies in a library GOtome writes into.
	body = a.runBulk(editor, map[string]any{"books": selection[1:], "action": "writeBack"})
	want = map[string]string{"Emma": "unchanged", "A Book Nobody Knows": "unchanged"}
	if got := outcomes(body); !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
}
