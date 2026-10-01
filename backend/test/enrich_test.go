//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// matcher looks books up with a provider that knows Emma by ISBN and by
// title, and has every file read hand its book to it.
func (a *app) matcher() (*enrich.Service, *atomic.Int64) {
	a.t.Helper()
	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.png" {
			_, _ = w.Write(squarePNG(a.t))
			return
		}
		asked.Add(1)
		emma := metadata.Record{
			ID: "emma", Title: "Emma", Publisher: "John Murray", Description: "A comedy of manners.",
			Published: "1815-12", CoverURL: "http://" + r.Host + "/cover.png",
			Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
			Identifiers:  []catalog.Identifier{{Type: catalog.IDOpenLibrary, Value: "OL1M"}},
		}
		switch r.URL.Path {
		case "/isbn/9780141439587":
			emma.Identifiers = append(emma.Identifiers, catalog.Identifier{Type: catalog.IDISBN, Value: "9780141439587"})
			_ = json.NewEncoder(w).Encode([]metadata.Record{emma})
		case "/search":
			_ = json.NewEncoder(w).Encode([]metadata.Record{emma})
		default:
			http.NotFound(w, r)
		}
	}))
	a.t.Cleanup(srv.Close)
	meta := metadata.NewService(a.pool, []metadata.Provider{shelfProvider{base: srv.URL}}, metadata.Options{AllowPrivate: true})
	m := enrich.NewService(a.pool, meta, a.scans, a.covers, a.settings, slog.New(slog.DiscardHandler))
	m.Queue = a.scans.Queue
	a.scans.OnExtracted = m.EnqueueTx
	return m, &asked
}

func (a *app) matchJobs(book uuid.UUID) int {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'enrich.match_book' AND args->>'bookId' = $1`, book.String()).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func (a *app) matchStates(book uuid.UUID) []string {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), `SELECT state FROM metadata_matches WHERE book_id = $1 ORDER BY id`, book)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestABookWithAnISBNIsMatchedByItself(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	m, asked := a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	writeEPUB(t, filepath.Join(folder, "Emma.epub"),
		`<dc:title>Emma</dc:title><dc:identifier opf:scheme="ISBN">0-14-143958-0</dc:identifier>`, nil, "Emma Woodhouse.")
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", folder))
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	if n := a.matchJobs(file.BookID); n != 1 {
		t.Fatalf("%d lookups queued, want 1", n)
	}
	// A person locked the publisher empty.
	a.call(editor, http.MethodPatch, "/books/"+file.BookID.String(), map[string]any{"locks": map[string]bool{"publisher": true}})

	if err := m.Match(context.Background(), file.BookID); err != nil {
		t.Fatal(err)
	}
	book := a.book(file.BookID)
	if book.Description != "A comedy of manners." || book.Published() != "1815-12" || book.CoverKey == "" ||
		len(book.Contributors) != 1 || book.Publisher != "" {
		t.Errorf("after the match: %+v", book)
	}
	if book.Sources["description"] != "provider:shelf" || book.Sources["title"] == "provider:shelf" {
		t.Errorf("sources %v", book.Sources)
	}
	if got := a.matchStates(file.BookID); len(got) != 1 || got[0] != enrich.StateApplied {
		t.Errorf("matches %v", got)
	}

	// Again: the same answers, from the cache, and nothing more changes.
	before, n := book, asked.Load()
	if err := m.Match(context.Background(), file.BookID); err != nil {
		t.Fatal(err)
	}
	after := a.book(file.BookID)
	if asked.Load() != n || len(a.matchStates(file.BookID)) != 1 || after.Description != before.Description ||
		len(after.Identifiers) != len(before.Identifiers) || after.CoverKey != before.CoverKey {
		t.Errorf("a second match changed things: %+v, asked %d more times", after, asked.Load()-n)
	}
}

func TestALowConfidenceMatchWaitsForAPerson(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	m, _ := a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	id, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{
		LibraryID: lib, Title: "Emma", Contributors: []catalog.NewContributor{{Name: "Emma Donoghue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Match(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if book := a.book(id); book.Publisher != "" || book.Description != "" || book.CoverKey != "" {
		t.Errorf("a doubtful match changed the book: %+v", book)
	}
	if got := a.matchStates(id); len(got) != 1 || got[0] != enrich.StatePending {
		t.Fatalf("matches %v", got)
	}
	// Dismissed, it is not brought back.
	if _, err := a.pool.Exec(context.Background(), `UPDATE metadata_matches SET state = 'dismissed' WHERE book_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := m.Match(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := a.matchStates(id); len(got) != 1 || got[0] != enrich.StateDismissed {
		t.Errorf("after dismissing: %v", got)
	}
}

func TestNothingIsLookedUpWhenSwitchedOff(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	if status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.AutoMatch: "off"}}); status != 200 {
		t.Fatalf("switch off: %d %v", status, body)
	}
	folder := t.TempDir()
	writeEPUB(t, filepath.Join(folder, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", folder))
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	if n := a.matchJobs(file.BookID); n != 0 {
		t.Errorf("%d lookups queued while switched off", n)
	}
}
