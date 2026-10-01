//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// shelfProvider asks a test server, with an API key in the query.
type shelfProvider struct{ base string }

func (shelfProvider) Name() string            { return "shelf" }
func (shelfProvider) Limits() metadata.Limits { return metadata.Limits{} }

func (p shelfProvider) Lookup(ctx context.Context, web metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	return p.get(ctx, web, p.base+"/isbn/"+id.Value)
}

func (p shelfProvider) Search(ctx context.Context, web metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	return p.get(ctx, web, p.base+"/search?"+url.Values{"title": {q.Title}}.Encode())
}

func (p shelfProvider) get(ctx context.Context, web metadata.Web, u string) ([]metadata.Record, error) {
	body, err := web.Get(ctx, metadata.Request{URL: u, SecretQuery: url.Values{"key": {"sekrit"}}})
	if err != nil {
		return nil, err
	}
	var out []metadata.Record
	return out, json.Unmarshal(body, &out)
}

func TestProvidersAreAskedOnceAndRemembered(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))

	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.png" {
			_, _ = w.Write(squarePNG(t))
			return
		}
		asked.Add(1)
		if r.URL.Query().Get("key") != "sekrit" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/isbn/9780141439587":
			_ = json.NewEncoder(w).Encode([]metadata.Record{{
				ID: "emma", Title: "Emma", Publisher: "Penguin", CoverURL: "http://" + r.Host + "/cover.png",
				Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
				Identifiers:  []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780141439587"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s := metadata.NewService(a.pool, []metadata.Provider{shelfProvider{base: srv.URL}}, metadata.Options{AllowPrivate: true})

	books := catalog.NewService(a.pool)
	ctx := context.Background()
	emma, err := books.CreateBook(ctx, catalog.NewBook{
		LibraryID: lib, Title: "Emma", Contributors: []catalog.NewContributor{{Name: "Jane Austen"}},
		Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780141439587"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: "Nobody Knows This"})
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		got, err := s.Candidates(ctx, a.book(emma))
		if err != nil || len(got) != 1 || got[0].Score != 1 || got[0].Publisher != "Penguin" {
			t.Fatalf("candidates for Emma: %+v, %v", got, err)
		}
		if got, err := s.Candidates(ctx, a.book(unknown)); err != nil || len(got) != 0 {
			t.Fatalf("candidates for the unknown book: %+v, %v", got, err)
		}
	}
	// The lookup and the search that found nothing, each once.
	if n := asked.Load(); n != 2 {
		t.Errorf("the provider was asked %d times, want 2", n)
	}
	var stored []string
	rows, err := a.pool.Query(ctx, `SELECT url || ' ' || status FROM provider_records WHERE provider = 'shelf' ORDER BY url`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		stored = append(stored, s)
	}
	rows.Close()
	if len(stored) != 2 || strings.Contains(strings.Join(stored, " "), "sekrit") {
		t.Errorf("stored %v", stored)
	}

	// The chosen record's cover goes into the cover store.
	got, _ := s.Candidates(ctx, a.book(emma))
	image, err := s.Cover(ctx, "shelf", got[0].CoverURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.covers.Put(image); err != nil {
		t.Errorf("the cover: %v", err)
	}
}
