//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

func TestABookWithAnISBNFromItsTextIsLookedUpByItOnce(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	m, _ := a.matcher()
	admin, _ := a.signedIn("admin", "admin")
	ctx := context.Background()
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	newBook := func(isbn string) uuid.UUID {
		id, _ := catalog.NormalizeIdentifier(catalog.IDISBN, isbn)
		b, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: "Effi Briest " + isbn, Identifiers: []catalog.Identifier{id},
			Contributors: []catalog.NewContributor{{Name: "Theodor Fontane"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	fromText := newBook("9780141439587")
	fromFile := newBook("9783596294312")
	if _, err := a.pool.Exec(ctx, `UPDATE books SET field_sources = field_sources || '{"identifiers": "text"}' WHERE id = $1`, fromText); err != nil {
		t.Fatal(err)
	}
	patch := func(v string) {
		if status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.AutoMatch: v}}); status != 200 {
			t.Fatalf("autoMatch %s: %d %v", v, status, body)
		}
	}

	patch("off")
	if n, err := m.QueueTextISBNLookups(ctx); err != nil || n != 0 || a.matchJobs(fromText) != 0 {
		t.Fatalf("with lookups off: queued %d, %d jobs, %v", n, a.matchJobs(fromText), err)
	}
	patch("on")
	if n, err := m.QueueTextISBNLookups(ctx); err != nil || n != 1 || a.matchJobs(fromText) != 1 || a.matchJobs(fromFile) != 0 {
		t.Fatalf("switched on: queued %d (text %d, file %d jobs), %v", n, a.matchJobs(fromText), a.matchJobs(fromFile), err)
	}
	if n, err := m.QueueTextISBNLookups(ctx); err != nil || n != 0 {
		t.Errorf("a second sweep queued %d, %v", n, err)
	}

	// Another reading of the text that gives another ISBN is looked up again.
	if _, err := a.pool.Exec(ctx, `UPDATE book_identifiers SET value = '9780141199085' WHERE book_id = $1 AND type = 'isbn'`, fromText); err != nil {
		t.Fatal(err)
	}
	if n, err := m.QueueTextISBNLookups(ctx); err != nil || n != 1 {
		t.Errorf("a new ISBN from the text queued %d, %v", n, err)
	}
}
