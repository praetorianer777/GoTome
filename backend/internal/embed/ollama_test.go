package embed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ollama answers /api/embed as an Ollama server does, with the fake
// model's vectors of dim numbers; same names texts it gives one vector.
func ollama(t *testing.T, dim int, same map[string]string, asked *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(body)
		_ = json.Unmarshal(raw, &req)
		if asked != nil {
			*asked = body
		}
		if req.Model != "bge-m3" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": `model "` + req.Model + `" not found, try pulling it first`})
			return
		}
		texts := make([]string, len(req.Input))
		for i, in := range req.Input {
			texts[i] = in
			if s, ok := same[in]; ok {
				texts[i] = s
			}
		}
		vecs, _ := (&Fake{Dim: dim}).Embed(r.Context(), texts)
		for _, v := range vecs {
			v[0] *= 3 // not normalised, to be normalised by the client
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": vecs})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOllamaEmbedsWithTheModelItIsAskedFor(t *testing.T) {
	ctx := context.Background()
	var asked map[string]any
	srv := ollama(t, 1024, map[string]string{probes[1]: probes[0]}, &asked)
	o, err := OpenOllama(ctx, srv.URL+"/", BGEM3)
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := o.Embed(ctx, []string{"eins", "zwei"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 1024 || cosine(vecs[0], vecs[0]) < 0.999 || cosine(vecs[0], vecs[0]) > 1.001 {
		t.Errorf("vectors: %d of %d, length² %v", len(vecs), len(vecs[0]), cosine(vecs[0], vecs[0]))
	}
	if asked["truncate"] != true || asked["options"].(map[string]any)["num_ctx"] != 512.0 {
		t.Errorf("asked %v", asked)
	}
	if o.Parallel() != ollamaParallel || o.Model() != BGEM3.Model {
		t.Errorf("parallel %d, model %v", o.Parallel(), o.Model())
	}
}

func TestOllamaWithAnotherModelIsRefused(t *testing.T) {
	ctx := context.Background()
	short := ollama(t, 384, map[string]string{probes[1]: probes[0]}, nil)
	if _, err := OpenOllama(ctx, short.URL, BGEM3); !errors.Is(err, ErrNotTheModel) {
		t.Errorf("vectors of another length: %v", err)
	}
	// Two sentences of one meaning as far apart as two of another.
	senseless := ollama(t, 1024, map[string]string{probes[1]: probes[2]}, nil)
	if _, err := OpenOllama(ctx, senseless.URL, BGEM3); !errors.Is(err, ErrNotTheModel) {
		t.Errorf("vectors without meaning: %v", err)
	}
	other := BGEM3
	other.Server = "nomic-embed-text"
	if _, err := OpenOllama(ctx, short.URL, other); !errors.Is(err, ErrServer) {
		t.Errorf("a model the server has not: %v", err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	if _, err := OpenOllama(ctx, gone.URL, BGEM3); !errors.Is(err, ErrServer) {
		t.Errorf("no server: %v", err)
	}
}
