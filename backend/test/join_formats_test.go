//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

type joinedBook struct {
	Files []struct {
		Kind string `json:"kind"`
	} `json:"files"`
	Contributors []struct {
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"contributors"`
	Forms []struct {
		ID    string   `json:"id"`
		Kinds []string `json:"kinds"`
	} `json:"forms"`
	Joins []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"joins"`
}

func TestAnEbookAndItsAudiobookAreJoinedWhereTheLibraryAsksAndSplitAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := catalog.NewService(a.pool)
	a.server.Duplicates.Checked = a.scans.QueueJoinTx

	add := func(lib, title, format, path string, narrator string) uuid.UUID {
		t.Helper()
		libID := uuid.MustParse(lib)
		contributors := []catalog.NewContributor{{Name: "Jane Austen"}}
		if narrator != "" {
			contributors = append(contributors, catalog.NewContributor{Name: narrator, Role: catalog.RoleNarrator})
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
	check := func(ids ...uuid.UUID) {
		t.Helper()
		for _, id := range ids {
			if err := a.server.Duplicates.Check(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	detail := func(c *http.Client, id uuid.UUID) (int, joinedBook) {
		t.Helper()
		status, body := a.get(c, "/books/"+id.String())
		var b joinedBook
		if status == 200 {
			if err := json.Unmarshal(body, &b); err != nil {
				t.Fatal(err)
			}
		}
		return status, b
	}
	kinds := func(b joinedBook) []string {
		var out []string
		for _, f := range b.Files {
			out = append(out, f.Kind)
		}
		slices.Sort(out)
		return out
	}

	asking := a.library(admin, "Asking", "shared")
	plain := a.library(admin, "Plain", "shared")
	if status, out, _ := a.call(admin, http.MethodPatch, "/libraries/"+asking, map[string]any{"joinFormats": true}); status != 200 || out["joinFormats"] != true {
		t.Fatalf("turn the option on: %d %v", status, out)
	}

	ebook := add(asking, "Emma", "epub", "text/Emma.epub", "")
	audio := add(asking, "Emma", "m4b", "audio/Emma.m4b", "Juliet Stevenson")
	plainEbook := add(plain, "Northanger Abbey", "epub", "text/Northanger.epub", "")
	plainAudio := add(plain, "Northanger Abbey", "m4b", "audio/Northanger.m4b", "Juliet Stevenson")
	check(ebook, audio, plainEbook, plainAudio)

	// Both pairs are suggested as an ebook and its audiobook.
	pairs := a.duplicates(admin, "")
	if len(pairs) != 2 {
		t.Fatalf("pairs: %v", described(pairs))
	}
	status, body := a.get(admin, "/duplicates")
	var list struct {
		Pairs []struct {
			Formats *struct {
				EbookID     string `json:"ebookId"`
				SameLibrary bool   `json:"sameLibrary"`
			} `json:"formats"`
			Books []struct {
				ID string `json:"id"`
			} `json:"books"`
		} `json:"pairs"`
	}
	if status != 200 || json.Unmarshal(body, &list) != nil {
		t.Fatalf("list: %d %s", status, body)
	}
	for _, p := range list.Pairs {
		if p.Formats == nil || !p.Formats.SameLibrary || (p.Formats.EbookID != ebook.String() && p.Formats.EbookID != plainEbook.String()) {
			t.Errorf("pair is not an ebook and its audiobook: %+v", p)
		}
	}

	// A library that did not ask keeps two books.
	if joined, err := a.scans.JoinFormats(ctx, plainEbook); err != nil || joined {
		t.Errorf("the library that did not ask: %v %v", joined, err)
	}

	// The one that did makes them one, the ebook's book surviving with the
	// narrator added.
	if joined, err := a.scans.JoinFormats(ctx, audio); err != nil || !joined {
		t.Fatalf("join: %v %v", joined, err)
	}
	if joined, err := a.scans.JoinFormats(ctx, ebook); err != nil || joined {
		t.Errorf("joined twice: %v %v", joined, err)
	}
	status, whole := detail(admin, ebook)
	if status != 200 || !slices.Equal(kinds(whole), []string{"audio", "ebook"}) || len(whole.Joins) != 1 {
		t.Fatalf("the joined book: %d %+v", status, whole)
	}
	var narrators []string
	for _, c := range whole.Contributors {
		if c.Role == "narrator" {
			narrators = append(narrators, c.Name)
		}
	}
	if !slices.Equal(narrators, []string{"Juliet Stevenson"}) {
		t.Errorf("narrators = %v", narrators)
	}
	if status, again := detail(admin, audio); status != 200 || len(again.Files) != 2 {
		t.Errorf("the old address of the audiobook: %d %+v", status, again)
	}

	// Only an editor splits it again, and the two are then kept as two.
	join := whole.Joins[0].ID
	if status, _, _ := a.call(reader, http.MethodPost, "/joins/"+join+"/undo", nil); status != 403 {
		t.Errorf("a reader splits: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/joins/"+uuid.NewString()+"/undo", nil); status != 404 {
		t.Errorf("no such join: %d", status)
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/joins/"+join+"/undo", nil); status != 204 {
		t.Fatalf("split: %d %v", status, out)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/joins/"+join+"/undo", nil); status != 404 {
		t.Errorf("split twice: %d", status)
	}
	_, ebookOnly := detail(admin, ebook)
	_, audioOnly := detail(admin, audio)
	if !slices.Equal(kinds(ebookOnly), []string{"ebook"}) || !slices.Equal(kinds(audioOnly), []string{"audio"}) || len(ebookOnly.Joins) != 0 {
		t.Errorf("after the split: %+v and %+v", ebookOnly, audioOnly)
	}
	if joined, err := a.scans.JoinFormats(ctx, ebook); err != nil || joined {
		t.Errorf("joined again after a split: %v %v", joined, err)
	}

	// Two audiobooks for one ebook are not guessed between.
	second := add(asking, "Persuasion", "m4b", "audio/Persuasion A.m4b", "Juliet Stevenson")
	third := add(asking, "Persuasion", "m4b", "audio/Persuasion B.m4b", "Karen Savage")
	text := add(asking, "Persuasion", "epub", "text/Persuasion.epub", "")
	check(second, third, text)
	if joined, err := a.scans.JoinFormats(ctx, text); err != nil || joined {
		t.Errorf("joined with one of two: %v %v", joined, err)
	}
}

func TestAnEbookAndAnAudiobookOfTwoLibrariesAreLinkedNotMerged(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := catalog.NewService(a.pool)
	open := a.library(admin, "Ebooks", "shared")
	vault := a.library(admin, "Audiobooks", "private")
	for _, lib := range []string{open, vault} {
		if status, _, _ := a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"joinFormats": true}); status != 200 {
			t.Fatalf("option: %d", status)
		}
	}
	add := func(lib, format, path string) uuid.UUID {
		t.Helper()
		libID := uuid.MustParse(lib)
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: libID, Title: "Emma",
			Contributors: []catalog.NewContributor{{Name: "Jane Austen"}}})
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
	ebook := add(open, "epub", "Emma.epub")
	audio := add(vault, "m4b", "Emma.m4b")
	for _, id := range []uuid.UUID{ebook, audio} {
		if err := a.server.Duplicates.Check(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if joined, err := a.scans.JoinFormats(ctx, ebook); err != nil || joined {
		t.Errorf("books of two libraries were joined: %v %v", joined, err)
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/books/"+ebook.String()+"/merge", map[string]any{"from": audio}); status != 422 {
		t.Errorf("merge across libraries: %d %v", status, out)
	}

	pairs := a.duplicates(admin, "")
	if len(pairs) != 1 {
		t.Fatalf("pairs: %v", described(pairs))
	}
	status, body := a.get(admin, "/duplicates")
	var list struct {
		Pairs []struct {
			Formats *struct {
				SameLibrary bool `json:"sameLibrary"`
			} `json:"formats"`
		} `json:"pairs"`
	}
	if status != 200 || json.Unmarshal(body, &list) != nil || list.Pairs[0].Formats == nil || list.Pairs[0].Formats.SameLibrary {
		t.Fatalf("the pair: %d %s", status, body)
	}
	// The reader sees no pair: one of its books is in a private library.
	if got := a.duplicates(reader, ""); len(got) != 0 {
		t.Errorf("a reader sees %v", described(got))
	}

	if status, _, _ := a.call(admin, http.MethodPut, "/duplicates/"+pairs[0].ID+"/state",
		map[string]any{"state": "kept_both", "relation": "format"}); status != 204 {
		t.Fatalf("link: %d", status)
	}
	form := func(c *http.Client, id uuid.UUID) joinedBook {
		t.Helper()
		_, b := func() (int, joinedBook) {
			status, body := a.get(c, "/books/"+id.String())
			var b joinedBook
			if status == 200 {
				_ = json.Unmarshal(body, &b)
			}
			return status, b
		}()
		return b
	}
	if forms := form(admin, ebook).Forms; len(forms) != 1 || forms[0].ID != audio.String() || !slices.Equal(forms[0].Kinds, []string{"audio"}) {
		t.Errorf("the ebook's other form: %+v", forms)
	}
	if forms := form(admin, audio).Forms; len(forms) != 1 || forms[0].ID != ebook.String() {
		t.Errorf("the audiobook's other form: %+v", forms)
	}
	// Who does not see the private library is not told of the link.
	if forms := form(reader, ebook).Forms; len(forms) != 0 {
		t.Errorf("a reader is told of %+v", forms)
	}
}
