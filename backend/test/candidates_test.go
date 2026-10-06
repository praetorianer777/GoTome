//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// withProvider gives the app a provider that knows Emma, by search.
func (a *app) withProvider() {
	a.t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cover.png":
			_, _ = w.Write(squarePNG(a.t))
		case "/search":
			_ = json.NewEncoder(w).Encode([]metadata.Record{{
				ID: "emma-1815", Title: "Emma", Description: "A comedy of manners.", Publisher: "John Murray",
				Published: "1815-12", Language: "en", CoverURL: "http://" + r.Host + "/cover.png",
				Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
				Tags:         []string{"Classics"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	a.t.Cleanup(srv.Close)
	a.server.Metadata = metadata.NewService(a.pool, []metadata.Provider{shelfProvider{base: srv.URL}}, metadata.Options{AllowPrivate: true})
}

func TestCandidatesAreFoundAndChosenFieldsTaken(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withProvider()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Wishes", "shared"))
	// A book without a file, as a wish is.
	id, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{
		LibraryID: lib, Title: "Emma", Contributors: []catalog.NewContributor{{Name: "Jane Austen"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/books/" + id.String()
	// A person's subtitle and title, locked, which no provider changes.
	a.call(editor, http.MethodPatch, path, map[string]any{"title": "Emma (my copy)"})

	if status, _, _ := a.call(reader, http.MethodGet, path+"/candidates", nil); status != 403 {
		t.Errorf("a reader asks for candidates: %d", status)
	}
	status, body, _ := a.call(editor, http.MethodGet, path+"/candidates", nil)
	candidates, _ := body["candidates"].([]any)
	if status != 200 || len(candidates) != 1 {
		t.Fatalf("candidates: %d %v", status, body)
	}
	c := candidates[0].(map[string]any)
	if c["provider"] != "shelf" || c["score"].(float64) < 0.5 || c["coverToken"] == nil {
		t.Errorf("candidate %v", c)
	}
	token := c["coverToken"].(string)

	resp, err := editor.Get(a.url + httpapi.APIPrefix + "/metadata/covers/" + token)
	if err != nil {
		t.Fatal(err)
	}
	image, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || len(image) == 0 {
		t.Errorf("the cover: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// One character of the signature changed to another: a fixed
	// replacement is the token itself once in a few thousand runs, and the
	// last character holds padding bits a decoder may ignore.
	at := len(token) - 5
	swap := byte('A')
	if token[at] == swap {
		swap = 'B'
	}
	forged := token[:at] + string(swap) + token[at+1:]
	if status, _, _ := a.call(editor, http.MethodGet, "/metadata/covers/"+forged, nil); status != 404 {
		t.Errorf("a forged token: %d", status)
	}

	status, body, _ = a.call(editor, http.MethodPost, path+"/candidates/apply", map[string]any{
		"provider": "shelf", "title": "Emma", "description": "A comedy of manners.", "publisher": "John Murray",
		"coverToken": token,
	})
	if status != 200 {
		t.Fatalf("apply: %d %v", status, body)
	}
	if body["title"] != "Emma (my copy)" || body["description"] != "A comedy of manners." || body["publisher"] != "John Murray" || body["coverKey"] == nil {
		t.Errorf("after applying: %v", body)
	}
	if f := field(body, "description"); f["source"] != "provider" || f["detail"] != "shelf" || f["locked"] != false {
		t.Errorf("the description's source: %v", f)
	}
	if f := field(body, "title"); f["source"] != "manual" || f["locked"] != true {
		t.Errorf("the locked title's source: %v", f)
	}
	if f := field(body, "published"); f != nil {
		t.Errorf("a field not chosen has a source: %v", f)
	}

	for name, req := range map[string]map[string]any{
		"an unknown provider": {"provider": "nobody", "title": "X"},
		"locks":               {"provider": "shelf", "locks": map[string]bool{"title": false}},
		"a forged cover":      {"provider": "shelf", "coverToken": "abc.def"},
	} {
		if status, body, _ := a.call(editor, http.MethodPost, path+"/candidates/apply", req); status != 422 {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	if status, _, _ := a.call(reader, http.MethodPost, path+"/candidates/apply", map[string]any{"provider": "shelf"}); status != 403 {
		t.Errorf("a reader applies: %d", status)
	}
}
