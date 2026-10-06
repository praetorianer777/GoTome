//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// direction is a vector of length 1 that leans on the given axes by the
// given amounts: two of them are as near as their weights on shared axes.
func direction(weights map[int]float32) []float32 {
	v := make([]float32, embed.E5Small.Model.Dim)
	for axis, w := range weights {
		v[axis] = w
	}
	return embed.Mean([][]float32{v}, []float64{1})
}

func (a *app) bookIn(libraryID, title string) uuid.UUID {
	a.t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(context.Background(), `
		INSERT INTO books (library_id, title, sort_title, title_key) VALUES ($1, $2, $2, lower($2))
		RETURNING id`, libraryID, title).Scan(&id); err != nil {
		a.t.Fatal(err)
	}
	return id
}

// putVector stores a vector as the embedding job does, under the model the
// settings name by default.
func (a *app) putVector(book uuid.UUID, kind string, v []float32) {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(), `
		INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
		VALUES ($1, $2, $3, $4, sha256('test'), $5::text::vector)`,
		book, kind, embed.E5Small.Model.Name, similar.Version(embed.E5Small.Model), vectorText(v)); err != nil {
		a.t.Fatal(err)
	}
}

func (a *app) similarTitles(c *http.Client, book uuid.UUID) []string {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/books/"+book.String()+"/similar", nil)
	if status != 200 {
		a.t.Fatalf("similar: %d %v", status, out)
	}
	var titles []string
	for _, b := range out["books"].([]any) {
		titles = append(titles, b.(map[string]any)["title"].(string))
	}
	return titles
}

func TestSimilarBooksAreTheNearestOnesTheViewerSeesButNoCopies(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	open := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")

	emma := a.bookIn(open, "Emma")
	near := a.bookIn(open, "Persuasion")
	far := a.bookIn(open, "Dracula")
	titled := a.bookIn(open, "Sanditon")
	copyOf := a.bookIn(open, "Emma, again")
	edition := a.bookIn(open, "Emma, annotated")
	hidden := a.bookIn(vault, "Lady Susan")
	wish := a.bookIn(open, "The Watsons")
	gone := a.bookIn(open, "Love and Freindship")
	ctx := context.Background()
	a.pool.Exec(ctx, `UPDATE books SET placeholder = true WHERE id = $1`, wish)
	a.pool.Exec(ctx, `UPDATE books SET deleted_at = now() WHERE id = $1`, gone)
	pairA, pairB := emma, copyOf
	if pairB.String() < pairA.String() {
		pairA, pairB = pairB, pairA
	}
	a.pool.Exec(ctx, `INSERT INTO duplicate_pairs (book_a, book_b, state) VALUES ($1, $2, 'kept_both')`, pairA, pairB)
	relA, relB := emma, edition
	if relB.String() < relA.String() {
		relA, relB = relB, relA
	}
	a.pool.Exec(ctx, `INSERT INTO book_relations (book_a, book_b, kind) VALUES ($1, $2, 'edition')`, relA, relB)

	both := func(book uuid.UUID, v []float32) {
		a.putVector(book, similar.KindContent, v)
		a.putVector(book, similar.KindMetadata, v)
	}
	both(emma, direction(map[int]float32{0: 1}))
	both(near, direction(map[int]float32{0: 0.9, 1: 0.1}))
	both(far, direction(map[int]float32{1: 1}))
	// Only a metadata vector, as a book without text has: compared on that.
	a.putVector(titled, similar.KindMetadata, direction(map[int]float32{0: 0.7, 1: 0.3}))
	for _, b := range []uuid.UUID{copyOf, edition, wish, gone} {
		both(b, direction(map[int]float32{0: 1}))
	}
	both(hidden, direction(map[int]float32{0: 0.95, 2: 0.05}))

	if got, want := a.similarTitles(reader, emma), []string{"Persuasion", "Sanditon", "Dracula"}; !slices.Equal(got, want) {
		t.Errorf("a reader of the open library is shown %v, want %v", got, want)
	}
	if got, want := a.similarTitles(admin, emma), []string{"Lady Susan", "Persuasion", "Sanditon", "Dracula"}; !slices.Equal(got, want) {
		t.Errorf("the administrator is shown %v, want %v", got, want)
	}
	status, out, _ := a.call(reader, http.MethodGet, "/books/"+emma.String()+"/similar?limit=1", nil)
	if status != 200 || len(out["books"].([]any)) != 1 {
		t.Errorf("limit 1: %d %v", status, out)
	}

	// Vectors of another model are not compared with these.
	a.pool.Exec(ctx, `UPDATE book_vectors SET model_version = 'other' WHERE book_id = $1`, near)
	if got := a.similarTitles(reader, emma); slices.Contains(got, "Persuasion") {
		t.Errorf("a book embedded by another model is shown: %v", got)
	}
	// A book not embedded yet has nothing similar, rather than an error.
	if got := a.similarTitles(reader, a.bookIn(open, "Northanger Abbey")); len(got) != 0 {
		t.Errorf("a book without vectors is shown %v", got)
	}
}

// Not parallel: the fixture is heavy enough to slow the timings of tests
// beside it, and theirs would slow this one's.
func TestSimilarBooksAreQuickOnALargeLibrary(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Large", "shared")
	// 50,000 books with both vectors, pointing anywhere: taken in turn
	// from a thousand random ones, as the scan's cost is the same.
	_, err := a.pool.Exec(ctx, `
		WITH made AS (
			INSERT INTO books (library_id, title, sort_title, title_key)
			SELECT $1, 'Book ' || i, 'Book ' || i, 'book ' || i FROM generate_series(1, 50000) i
			RETURNING id),
		numbered AS (SELECT id, row_number() OVER () AS rn FROM made),
		pool AS (
			SELECT n, array_agg(random() - 0.5)::vector AS v
			FROM generate_series(0, 999) n, generate_series(1, 384) d
			GROUP BY n)
		INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
		SELECT b.id, k.kind, $2, $3, sha256('test'), p.v
		FROM numbered b
		CROSS JOIN (VALUES ('content', 0), ('metadata', 500)) k(kind, shift)
		JOIN pool p ON p.n = (b.rn + k.shift) % 1000`,
		lib, embed.E5Small.Model.Name, similar.Version(embed.E5Small.Model))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, "ANALYZE books; ANALYZE book_vectors"); err != nil {
		t.Fatal(err)
	}
	var book uuid.UUID
	a.pool.QueryRow(ctx, `SELECT id FROM books WHERE title = 'Book 25000'`).Scan(&book)

	for name, scope := range map[string]library.Scope{"administrator": {SeesAll: true}, "reader": {Viewer: uuid.New()}} {
		// The best of three, so that a busy machine does not decide.
		best := time.Hour
		var found int
		for range 3 {
			started := time.Now()
			ids, err := a.server.Similar.Similar(ctx, scope, book, 12)
			if err != nil {
				t.Fatal(err)
			}
			found = len(ids)
			best = min(best, time.Since(started))
		}
		t.Logf("%s: %d similar books in %s", name, found, best)
		if name == "administrator" && found != 12 {
			t.Errorf("%d similar books among 50,000", found)
		}
		if best > 500*time.Millisecond {
			t.Errorf("%s: %s, want an answer while the page loads", name, best)
		}
	}
}
