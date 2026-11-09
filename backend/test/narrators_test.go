//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

type recordingBook struct {
	Recordings []struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Narrators []string `json:"narrators"`
	} `json:"recordings"`
}

func TestAnotherNarratorIsLinkedNotMerged(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Audio", "shared")
	libID := uuid.MustParse(lib)
	books := catalog.NewService(a.pool)

	add := func(title, format, path string, narrators ...string) uuid.UUID {
		t.Helper()
		contributors := []catalog.NewContributor{{Name: "Jane Austen"}}
		for _, n := range narrators {
			contributors = append(contributors, catalog.NewContributor{Name: n, Role: catalog.RoleNarrator})
		}
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: libID, Title: title, Contributors: contributors})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := books.AddFile(ctx, id, libID, catalog.NewFile{
			RelPath: path, Format: format, Size: 10, ModifiedAt: time.Now(), SHA256: hash(path),
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stevenson := add("Emma", "m4b", "stevenson/Emma.m4b", "Juliet Stevenson")
	savage := add("Emma", "m4b", "savage/Emma.m4b", "Karen Savage")
	ebook := add("Emma", "epub", "text/Emma.epub")
	sameVoice := add("Persuasion", "m4b", "a/Persuasion.m4b", "Juliet Stevenson")
	again := add("Persuasion", "m4b", "b/Persuasion.m4b", "Juliet Stevenson")
	for _, id := range []uuid.UUID{stevenson, savage, ebook, sameVoice, again} {
		if err := a.server.Duplicates.Check(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	var pairs []struct {
		ID        string     `json:"id"`
		Narrators [][]string `json:"narrators"`
		Books     []struct {
			ID string `json:"id"`
		} `json:"books"`
	}
	status, body := a.get(admin, "/duplicates")
	if status != 200 {
		t.Fatalf("duplicates: %d %s", status, body)
	}
	var list struct {
		Pairs json.RawMessage `json:"pairs"`
	}
	if err := json.Unmarshal(body, &list); err != nil || json.Unmarshal(list.Pairs, &pairs) != nil {
		t.Fatalf("decode: %v %s", err, body)
	}
	var recording string
	for _, p := range pairs {
		ids := []string{p.Books[0].ID, p.Books[1].ID}
		switch {
		case slices.Contains(ids, stevenson.String()) && slices.Contains(ids, savage.String()):
			recording = p.ID
			want := [][]string{{"Juliet Stevenson"}, {"Karen Savage"}}
			if p.Books[0].ID == savage.String() {
				want = [][]string{{"Karen Savage"}, {"Juliet Stevenson"}}
			}
			if !slices.EqualFunc(p.Narrators, want, slices.Equal[[]string]) {
				t.Errorf("narrators = %v, want %v", p.Narrators, want)
			}
		case slices.Contains(ids, ebook.String()):
			if p.Narrators != nil {
				t.Errorf("an ebook and an audiobook are not recordings: %v", p.Narrators)
			}
		case slices.Contains(ids, sameVoice.String()):
			if p.Narrators != nil {
				t.Errorf("the same narrator is a copy, not a recording: %v", p.Narrators)
			}
		}
	}
	if recording == "" {
		t.Fatalf("no pair of the two recordings in %s", body)
	}

	if status, _, _ := a.call(admin, http.MethodPut, "/duplicates/"+recording+"/state",
		map[string]any{"state": "kept_both", "relation": "recording"}); status != 204 {
		t.Fatalf("link as recordings: %d", status)
	}
	for id, other := range map[uuid.UUID]string{stevenson: "Karen Savage", savage: "Juliet Stevenson"} {
		status, body := a.get(admin, "/books/"+id.String())
		var book recordingBook
		if status != 200 || json.Unmarshal(body, &book) != nil || len(book.Recordings) != 1 ||
			!slices.Equal(book.Recordings[0].Narrators, []string{other}) {
			t.Errorf("recordings of %s: %d %s", id, status, body)
		}
	}
	if status, body := a.get(admin, "/books/"+ebook.String()); status != 200 || !json.Valid(body) {
		t.Errorf("ebook: %d", status)
	} else {
		var book recordingBook
		_ = json.Unmarshal(body, &book)
		if len(book.Recordings) != 0 {
			t.Errorf("an ebook has recordings: %s", body)
		}
	}

	filter := `{"all":[{"field":"narrator","op":"in","values":["Karen Savage"]}]}`
	status, body = a.get(admin, "/books?filter="+url.QueryEscape(filter))
	var found struct {
		Books []struct {
			ID string `json:"id"`
		} `json:"books"`
	}
	if status != 200 || json.Unmarshal(body, &found) != nil || len(found.Books) != 1 || found.Books[0].ID != savage.String() {
		t.Errorf("filter by narrator: %d %s", status, body)
	}
	status, body = a.get(admin, "/books/facets")
	if status != 200 || !json.Valid(body) {
		t.Fatalf("facets: %d", status)
	}
	var facets struct {
		Facets []struct {
			Field  string `json:"field"`
			Values []struct {
				Value string `json:"value"`
				Label string `json:"label"`
				Count int    `json:"count"`
			} `json:"values"`
		} `json:"facets"`
	}
	if err := json.Unmarshal(body, &facets); err != nil {
		t.Fatal(err)
	}
	var narrators []string
	for _, f := range facets.Facets {
		if f.Field == "narrator" {
			for _, v := range f.Values {
				narrators = append(narrators, v.Label)
			}
		}
	}
	slices.Sort(narrators)
	if !slices.Equal(narrators, []string{"Juliet Stevenson", "Karen Savage"}) {
		t.Errorf("narrator facet = %v", narrators)
	}
}
