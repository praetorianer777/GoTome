//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

func (a *app) described(c *http.Client, query url.Values) (int, []string, map[string]any) {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/search/similar?"+query.Encode(), nil)
	var titles []string
	if books, ok := out["books"].([]any); ok {
		for _, b := range books {
			titles = append(titles, b.(map[string]any)["title"].(string))
		}
	}
	return status, titles, out
}

func TestADescriptionFindsTheNearestBooksTheViewerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	open := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")

	const wanted = "a detective story set in Venice"
	fake := &embed.Fake{}
	vecs, err := fake.Embed(context.Background(), []string{embed.Prefix + wanted})
	if err != nil {
		t.Fatal(err)
	}
	exact := vecs[0]
	// Nearer the description the more of it a book's vectors hold.
	near := func(share float32) []float32 {
		v := make([]float32, len(exact))
		for i := range v {
			v[i] = share * exact[i]
		}
		v[0] += 1 - share
		return embed.Mean([][]float32{v}, []float64{1})
	}
	books := catalog.NewService(a.pool)
	for _, b := range []struct {
		lib, title, author string
		content            []float32
		metadata           []float32
	}{
		{open, "Death at La Fenice", "Donna Leon", near(1), near(1)},
		{open, "Brunetti's Cookbook", "Roberta Pianaro", near(0.6), nil},
		{open, "A Gardening Year", "Gertrude Jekyll", near(0.1), near(0.1)},
		{vault, "The Secret of the Lagoon", "Donna Leon", near(1), near(1)},
		// A copy, which a page leaves out after the book.
		{open, "A Gardening Year (German Edition)", "Jekyll, Gertrude", near(0.1), near(0.1)},
	} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: uuid.MustParse(b.lib), Title: b.title, Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		a.putVector(id, similar.KindContent, b.content)
		if b.metadata != nil {
			a.putVector(id, similar.KindMetadata, b.metadata)
		}
	}
	a.bookIn(open, "Not Embedded Yet")

	status, got, out := a.described(reader, url.Values{"q": {"  a detective   story set in Venice "}})
	if status != 200 || !slices.Equal(got, []string{"Death at La Fenice", "Brunetti's Cookbook", "A Gardening Year"}) {
		t.Errorf("the reader's books: %d %v %v", status, got, out)
	}
	if _, got, _ := a.described(admin, url.Values{"q": {wanted}, "limit": {"2"}}); len(got) != 2 || !slices.Contains(got, "The Secret of the Lagoon") {
		t.Errorf("the administrator, who sees the vault: %v", got)
	}
	_, first, page := a.described(admin, url.Values{"q": {wanted}, "limit": {"2"}})
	if page["nextOffset"] != 2.0 {
		t.Errorf("first page: %v", page)
	}
	_, second, last := a.described(admin, url.Values{"q": {wanted}, "limit": {"2"}, "offset": {"2"}})
	if len(second) != 2 || slices.ContainsFunc(second, func(s string) bool { return slices.Contains(first, s) }) || last["nextOffset"] != 4.0 {
		t.Errorf("second page: %v after %v, %v", second, first, last)
	}
	if _, got, _ := a.described(admin, url.Values{"q": {wanted}, "library": {open}}); slices.Contains(got, "The Secret of the Lagoon") {
		t.Errorf("one library's books: %v", got)
	}
	if status, _, _ := a.described(reader, url.Values{"q": {"  "}}); status != 422 {
		t.Errorf("no description: %d", status)
	}

	// Without a model that can run, it says so.
	none := similar.NewService(a.pool, a.settings.Embedding, nil, slog.New(slog.DiscardHandler))
	if _, _, err := none.Describe(context.Background(), library.Scope{SeesAll: true}, nil, wanted, 0, 10); err != similar.ErrUnavailable {
		t.Errorf("no model: %v", err)
	}
}
