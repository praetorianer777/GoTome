//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// fakeOllama answers /api/embed as an Ollama server with bge-m3 does, with
// the fake model's vectors of bge-m3's length. The opening check's two
// sentences about a murder in Venice get one vector, as a model of meaning
// would place them.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "bge-m3" {
			http.Error(w, `{"error":"no such model"}`, http.StatusNotFound)
			return
		}
		texts := slices.Clone(req.Input)
		for i, in := range texts {
			if strings.Contains(in, "Venedig") || strings.Contains(in, "Venice") {
				texts[i] = "venice"
			}
		}
		vecs, _ := (&embed.Fake{Dim: embed.BGEM3.Model.Dim}).Embed(r.Context(), texts)
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (a *vectorApp) vectorsOf(model string) map[string]int {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), `
		SELECT kind, vector_dims(embedding) FROM book_vectors WHERE book_id = $1 AND model = $2`, a.text, model)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var dims int
		if err := rows.Scan(&kind, &dims); err != nil {
			a.t.Fatal(err)
		}
		out[kind] = dims
	}
	return out
}

func TestBooksAreEmbeddedOnAServerBesideTheVectorsOfTheModelBefore(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	a.vectors = similar.NewService(a.pool, a.settings.Embedding, similar.ServerOpener(
		func(context.Context, embed.Spec) (embed.Embedder, error) {
			a.opened.Add(1)
			return &embed.Fake{}, nil
		}, a.settings.EmbeddingServer), slog.New(slog.DiscardHandler))
	a.embedAll()
	e5 := embed.E5Small.Model.Name
	if got := a.vectorsOf(e5); got["content"] != 384 || got["metadata"] != 384 {
		t.Fatalf("e5-small's vectors: %v", got)
	}

	a.set(settings.EmbeddingModel, "bge-m3")
	_, err := a.vectors.Embed(context.Background(), time.Now().Add(time.Minute), 0)
	if !errors.Is(err, similar.ErrUnavailable) || !strings.Contains(err.Error(), "embedding.server") {
		t.Errorf("bge-m3 without a server: %v", err)
	}
	if err := a.settings.Update(context.Background(), a.adminID, map[string]*string{settings.EmbeddingServer: ptr("ollama:11434")}); err == nil {
		t.Error("an address without its scheme is taken")
	}

	srv := fakeOllama(t)
	a.set(settings.EmbeddingServer, srv.URL+"/")
	a.embedAll()
	bge := embed.BGEM3.Model.Name
	if got := a.vectorsOf(bge); got["content"] != 1024 || got["metadata"] != 1024 {
		t.Errorf("bge-m3's vectors: %v", got)
	}
	if got := a.vectorsOf(e5); len(got) != 2 {
		t.Errorf("e5-small's vectors are gone: %v", got)
	}
	near, err := a.vectors.Similar(context.Background(), library.Scope{SeesAll: true}, a.text, 5)
	if err != nil || !slices.Equal(near, []uuid.UUID{a.bare}) {
		t.Errorf("similar by bge-m3: %v %v", near, err)
	}

	// Back to e5-small: its vectors are there, and nothing is embedded.
	opened := a.opened.Load()
	a.set(settings.EmbeddingModel, embed.DefaultSpec)
	a.embedAll()
	if a.opened.Load() != opened {
		t.Error("going back to the model before embedded the books again")
	}

	// A server gone ends a pass without an error to retry.
	a.set(settings.EmbeddingModel, "bge-m3")
	srv.Close()
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET description = 'New.' WHERE id = $1`, a.bare); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vectors.Embed(context.Background(), time.Now().Add(time.Minute), 0); !errors.Is(err, similar.ErrUnavailable) {
		t.Errorf("no server: %v", err)
	}
}
