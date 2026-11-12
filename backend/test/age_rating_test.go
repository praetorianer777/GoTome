//go:build integration

package test

import (
	"net/http"
	"testing"
)

func TestAnAgeRatingFollowsTagsUntilAPersonSetsIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Shelf", "shared")
	status, up := a.upload(admin, lib, "tale.pdf", []byte("%PDF-1.4 tale"))
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	book := "/books/" + up["bookId"].(string)
	edit := func(body map[string]any) map[string]any {
		t.Helper()
		status, out, _ := a.call(admin, http.MethodPatch, book, body)
		if status != 200 {
			t.Fatalf("edit %v: %d %v", body, status, out)
		}
		return out
	}
	field := func(out map[string]any) map[string]any {
		f, _ := out["fields"].(map[string]any)["ageRating"].(map[string]any)
		return f
	}

	out := edit(map[string]any{"tags": []string{"Crime", "Young adult"}})
	if out["ageRating"] != nil {
		t.Fatalf("plain tags rate the book: %v", out["ageRating"])
	}
	out = edit(map[string]any{"tags": []string{"Crime", "Belletristik/Erotik"}})
	if out["ageRating"] != 18.0 || field(out)["source"] != "tags" {
		t.Fatalf("an erotica tag should rate the book 18 from tags: %v %v", out["ageRating"], field(out))
	}
	out = edit(map[string]any{"tags": []string{"Crime"}})
	if out["ageRating"] != nil {
		t.Fatalf("taking the tag off should take the rating: %v", out["ageRating"])
	}

	out = edit(map[string]any{"tags": []string{"fsk 16"}})
	if out["ageRating"] != 16.0 {
		t.Fatalf("fsk 16: %v", out["ageRating"])
	}
	out = edit(map[string]any{"ageRating": 6})
	if out["ageRating"] != 6.0 || field(out)["source"] != "manual" || field(out)["locked"] != true {
		t.Fatalf("a person's rating is manual and locked: %v %v", out["ageRating"], field(out))
	}
	out = edit(map[string]any{"tags": []string{"Erotica"}})
	if out["ageRating"] != 6.0 {
		t.Fatalf("tags changed a locked rating: %v", out["ageRating"])
	}
	out = edit(map[string]any{"ageRating": -1})
	if out["ageRating"] != nil || field(out)["locked"] != true {
		t.Fatalf("removing the rating keeps the tags off it: %v %v", out["ageRating"], field(out))
	}
	out = edit(map[string]any{"locks": map[string]bool{"ageRating": false}, "tags": []string{"Erotica"}})
	if out["ageRating"] != 18.0 {
		t.Fatalf("unlocked, the tags rate it again: %v", out["ageRating"])
	}

	if status, out, _ := a.call(admin, http.MethodPatch, book, map[string]any{"ageRating": 14}); status != 422 || fieldError(out, "ageRating") == "" {
		t.Fatalf("14 is no step: %d %v", status, out)
	}
}

func TestAnAccountSeesNoBookAboveItsMaximumRating(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	editor, editorID := a.signedIn("editor", "editor")
	lib := a.library(admin, "Shelf", "shared")

	ids := map[int]string{}
	for _, r := range []int{-1, 0, 12, 16, 18} {
		status, up := a.upload(admin, lib, "book.pdf", []byte("%PDF-1.4 rated "+string(rune('a'+r+1))))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		ids[r] = up["bookId"].(string)
		if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+ids[r], map[string]any{"ageRating": r}); status != 200 {
			t.Fatalf("rate: %d %v", status, out)
		}
	}
	count := func(c *http.Client) float64 {
		t.Helper()
		status, out, _ := a.call(c, http.MethodGet, "/books/count", nil)
		if status != 200 {
			t.Fatalf("count: %d %v", status, out)
		}
		return out["count"].(float64)
	}
	if got := count(reader); got != 5 {
		t.Fatalf("without a maximum the reader sees %v of 5 books", got)
	}

	set := func(id string, max any) (int, map[string]any) {
		status, out, _ := a.call(admin, http.MethodPut, "/users/"+id+"/max-age-rating", map[string]any{"maxAgeRating": max})
		return status, out
	}
	if status, out := set(readerID, 12); status != 200 || out["maxAgeRating"] != 12.0 {
		t.Fatalf("set: %d %v", status, out)
	}
	if got := count(reader); got != 3 {
		t.Fatalf("with a maximum of 12 the reader sees %v books, not the unrated, 0 and 12", got)
	}
	for r, want := range map[int]int{-1: 200, 0: 200, 12: 200, 16: 404, 18: 404} {
		if status, _, _ := a.call(reader, http.MethodGet, "/books/"+ids[r], nil); status != want {
			t.Errorf("book rated %d answers the reader %d, not %d", r, status, want)
		}
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/books/"+ids[18], nil); status != 200 {
		t.Errorf("the administrator is answered %d about an adult book", status)
	}

	if status, out := set(readerID, 0); status != 200 || count(reader) != 2 {
		t.Fatalf("a maximum of 0: %d %v, sees %v", status, out, count(reader))
	}
	if status, out := set(readerID, nil); status != 200 || count(reader) != 5 {
		t.Fatalf("no maximum: %d %v, sees %v", status, out, count(reader))
	}
	if status, _ := set(readerID, 14); status != 422 {
		t.Errorf("a maximum of 14 is answered %d", status)
	}
	if status, out := set(adminID, 12); status != 422 || fieldError(out, "maxAgeRating") == "" {
		t.Errorf("an administrator cannot be limited: %d %v", status, out)
	}

	if status, _ := set(editorID, 12); status != 200 {
		t.Fatalf("limiting the editor: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPut, "/users/"+editorID+"/max-age-rating", map[string]any{"maxAgeRating": nil}); status != 403 {
		t.Errorf("a limited account lifted its own limit: %d", status)
	}
	if status, out, _ := a.call(editor, http.MethodPatch, "/books/"+ids[0], map[string]any{"ageRating": 18}); status != 422 || fieldError(out, "ageRating") == "" {
		t.Errorf("an editor limited to 12 rated a book 18: %d %v", status, out)
	}
	if status, out, _ := a.call(editor, http.MethodPatch, "/books/"+ids[12], map[string]any{"ageRating": 6}); status != 200 {
		t.Errorf("an editor limited to 12 could not lower a rating: %d %v", status, out)
	}
	if status, _, _ := a.call(editor, http.MethodPatch, "/books/"+ids[18], map[string]any{"title": "Found"}); status != 404 {
		t.Errorf("a limited editor edited an adult book: %d", status)
	}
}

func TestMergingKeepsTheHigherRating(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Shelf", "shared")
	ids := []string{}
	for _, name := range []string{"one", "two"} {
		status, up := a.upload(admin, lib, name+".pdf", []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		ids = append(ids, up["bookId"].(string))
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+ids[1], map[string]any{"ageRating": 16}); status != 200 {
		t.Fatalf("rate: %d %v", status, out)
	}
	status, out, _ := a.call(admin, http.MethodPost, "/books/"+ids[0]+"/merge", map[string]any{"from": ids[1]})
	if status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	if _, book, _ := a.call(admin, http.MethodGet, "/books/"+ids[0], nil); book["ageRating"] != 16.0 {
		t.Fatalf("the merged book is rated %v, not 16", book["ageRating"])
	}
}
