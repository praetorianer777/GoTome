//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// search asks the quick search and returns the titles found and what each
// matched by.
func (a *app) search(c *http.Client, words string) ([]string, []string) {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/books/search?"+url.Values{"q": {words}}.Encode(), nil)
	if status != 200 {
		a.t.Fatalf("search %q: %d %v", words, status, body)
	}
	var titles, matches []string
	for _, b := range body["books"].([]any) {
		hit := b.(map[string]any)
		titles = append(titles, hit["title"].(string))
		matches = append(matches, hit["match"].(string))
	}
	return titles, matches
}

func TestQuickSearch(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := uuid.MustParse(a.library(admin, "Shared", "shared"))
	private := uuid.MustParse(a.library(admin, "Private", "private"))
	books := catalog.NewService(a.pool)
	add := func(lib uuid.UUID, title, author, series string) {
		t.Helper()
		_, err := books.CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: title, Series: series,
			Contributors: []catalog.NewContributor{{Name: author, Role: catalog.RoleAuthor}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add(shared, "The Way of Kings", "Brandon Sanderson", "The Stormlight Archive")
	add(shared, "Mistborn", "Brandon Sanderson", "")
	add(shared, "Der Schatten des Windes", "Carlos Ruiz Zafón", "")
	add(shared, "Pride and Prejudice", "Jane Austen", "")
	add(private, "Elantris", "Brandon Sanderson", "")

	cases := []struct {
		words   string
		titles  []string
		matches []string
	}{
		// One wrong letter in the author's name.
		{"Sandersen", []string{"Mistborn", "The Way of Kings"}, []string{"author", "author"}},
		// Part of a title, and a misspelt part.
		{"way of king", []string{"The Way of Kings"}, []string{"title"}},
		{"Pride and Prejudise", []string{"Pride and Prejudice"}, []string{"title"}},
		// Accents and case do not matter.
		{"zafon", []string{"Der Schatten des Windes"}, []string{"author"}},
		{"SCHATTEN", []string{"Der Schatten des Windes"}, []string{"title"}},
		{"stormlight", []string{"The Way of Kings"}, []string{"series"}},
		{"ab", nil, nil},
		{"xyzzy plugh", nil, nil},
	}
	for _, c := range cases {
		titles, matches := a.search(reader, c.words)
		if !equalStrings(titles, c.titles) || !equalStrings(matches, c.matches) {
			t.Errorf("%q: %v by %v, want %v by %v", c.words, titles, matches, c.titles, c.matches)
		}
	}

	// What the reader may not see is not found; the administrator sees all.
	if titles, _ := a.search(admin, "sanderson"); len(titles) != 3 {
		t.Errorf("the administrator finds %v", titles)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestQuickSearchIsQuickOnALargeLibrary(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Large", "shared")
	// 50,000 books by 5,000 authors, with titles and names made of
	// syllables, so that trigrams repeat the way they do in real ones.
	_, err := a.pool.Exec(ctx, `
		WITH syllables AS (SELECT ARRAY['ka','lo','mir','san','der','son','tha','vel','ri','on','bel','ast','or','ine','qu','zu'] AS s),
		authors_made AS (
			INSERT INTO authors (name, sort_name, name_key)
			SELECT n, n, n FROM (
				SELECT DISTINCT s[1 + i % 16] || s[1 + (i / 16) % 16] || s[1 + (i / 256) % 16] || ' ' ||
				       s[1 + (i * 7) % 16] || s[1 + (i * 11 / 16) % 16] || ' ' || i AS n
				FROM syllables, generate_series(1, 5000) i) names
			RETURNING id, name_key),
		numbered AS (SELECT id, row_number() OVER () AS rn FROM authors_made),
		books_made AS (
			INSERT INTO books (library_id, title, sort_title, title_key)
			SELECT $1, t, t, t FROM (
				SELECT s[1 + i % 16] || s[1 + (i / 3) % 16] || ' ' || s[1 + (i / 7) % 16] || s[1 + (i / 49) % 16] ||
				       s[1 + (i / 343) % 16] || ' ' || i AS t
				FROM syllables, generate_series(1, 50000) i) titles
			RETURNING id),
		books_numbered AS (SELECT id, row_number() OVER () AS rn FROM books_made)
		INSERT INTO book_contributors (book_id, author_id, role, position)
		SELECT b.id, n.id, 'author', 0 FROM books_numbered b JOIN numbered n ON n.rn = 1 + b.rn % 5000`, lib)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, "ANALYZE books; ANALYZE authors; ANALYZE book_contributors"); err != nil {
		t.Fatal(err)
	}

	books := catalog.NewService(a.pool)
	scope := library.Scope{SeesAll: true}
	for _, words := range []string{"sandersen", "mirkalo", "velri onbel"} {
		// The best of three, so that a busy machine does not decide.
		best := time.Hour
		var found int
		for range 3 {
			started := time.Now()
			hits, err := books.Search(ctx, scope, words, 10)
			if err != nil {
				t.Fatal(err)
			}
			found = len(hits)
			best = min(best, time.Since(started))
		}
		t.Logf("%q: %d hits in %s", words, found, best)
		if found == 0 {
			t.Errorf("%q found nothing among 50,000 books", words)
		}
		if best > 300*time.Millisecond {
			t.Errorf("%q took %s, want an answer while the person types", words, best)
		}
	}
}
