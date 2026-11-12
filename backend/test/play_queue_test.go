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

func TestAPlayQueueHoldsOnlyWhatIsSeen(t *testing.T) {
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
		if _, err := books.AddFile(context.Background(), id, lib, catalog.NewFile{RelPath: title + ".mp3", Format: "mp3", Size: 1}); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	one, two, secret := book(shelf, "One"), book(shelf, "Two"), book(vault, "Secret")
	ebook, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: shelf, Title: "Text"})
	if err != nil {
		t.Fatal(err)
	}

	ids := func(out map[string]any) []string {
		var got []string
		for _, b := range out["books"].([]any) {
			got = append(got, b.(string))
		}
		return got
	}
	put := func(c *http.Client, books ...string) []string {
		t.Helper()
		status, out, _ := a.call(c, http.MethodPut, "/me/queue", map[string]any{"books": books})
		if status != 200 {
			t.Fatalf("put %v: %d %v", books, status, out)
		}
		return ids(out)
	}

	if got := put(admin, two, secret, one, two); !slices.Equal(got, []string{two, secret, one}) {
		t.Errorf("the administrator's queue: %v", got)
	}
	if got := put(reader, ebook.String(), one); !slices.Equal(got, []string{one}) {
		t.Errorf("a book with no audio is queued: %v", got)
	}
	if got := put(reader, two, secret, one); !slices.Equal(got, []string{two, one}) {
		t.Errorf("the reader is queued a book of a library they may not see: %v", got)
	}
	_, out, _ := a.call(admin, http.MethodGet, "/me/queue", nil)
	if got := ids(out); !slices.Equal(got, []string{two, secret, one}) {
		t.Errorf("the queue is each person's own: %v", got)
	}
	if got := put(admin, one); !slices.Equal(got, []string{one}) {
		t.Errorf("replacing the queue: %v", got)
	}
	_, out, _ = a.call(reader, http.MethodGet, "/me/queue", nil)
	if got := ids(out); !slices.Equal(got, []string{two, one}) {
		t.Errorf("another's change reached the reader's queue: %v", got)
	}
	if status, _, _ := a.call(admin, http.MethodPut, "/me/queue", map[string]any{"books": []string{"nope"}}); status != 400 && status != 422 {
		t.Errorf("a queue of nothing like books: %d", status)
	}
}
