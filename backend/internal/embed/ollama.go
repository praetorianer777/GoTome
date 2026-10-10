package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// ErrServer is an embedding server that did not answer, or not as asked:
// the next pass tries again.
var ErrServer = errors.New("the embedding server did not answer")

// ErrNotTheModel is a server whose model does not give the vectors the spec
// names: it is not used.
var ErrNotTheModel = errors.New("the embedding server's model is not the one chosen")

// ollamaParallel is how many requests the Ollama embedder sends at once:
// Ollama runs four side by side by default, and on the owner's GPU four
// books at once embed a quarter faster than one.
const ollamaParallel = 4

// Ollama embeds through an Ollama server's /api/embed, where a model too
// large for the CPU runs on a GPU. Each text is cut at the spec's
// MaxTokens by the server; the vectors come back normalised.
type Ollama struct {
	url    string
	spec   Spec
	client *http.Client
}

// probes are three sentences a model that embeds by meaning places so: the
// first two, which say the same, nearer each other than the third.
var probes = []string{
	"Ein Kommissar ermittelt in einem Mordfall in Venedig.",
	"A police inspector investigates a murder in Venice.",
	"Das Rezept braucht zwei Eier und etwas Mehl.",
}

// OpenOllama connects to the Ollama server at url for the spec's Server
// model and checks that it gives that model's vectors: of its length, and
// placing sentences by their meaning. A server that cannot be reached is
// ErrServer; one with another model, ErrNotTheModel.
func OpenOllama(ctx context.Context, url string, spec Spec) (*Ollama, error) {
	o := &Ollama{
		url:  strings.TrimRight(url, "/") + "/api/embed",
		spec: spec,
		// The first request loads the model into the GPU's memory.
		client: &http.Client{Timeout: 5 * time.Minute},
	}
	vecs, err := o.Embed(ctx, probes)
	if err != nil {
		return nil, err
	}
	if len(vecs[0]) != spec.Model.Dim {
		return nil, fmt.Errorf("%w: %s gives vectors of %d numbers, %s has %d",
			ErrNotTheModel, spec.Server, len(vecs[0]), spec.Model.Name, spec.Model.Dim)
	}
	same, other := cosine(vecs[0], vecs[1]), cosine(vecs[0], vecs[2])
	if same <= other {
		return nil, fmt.Errorf("%w: %s does not place sentences by their meaning (%.2f for the same, %.2f for another)",
			ErrNotTheModel, spec.Server, same, other)
	}
	return o, nil
}

func (o *Ollama) Model() Model { return o.spec.Model }

// Parallel is how many books the server embeds at once.
func (o *Ollama) Parallel() int { return ollamaParallel }

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": o.spec.Server, "input": texts, "truncate": true,
		// The same for every request, or the server loads the model again.
		"options": map[string]any{"num_ctx": o.spec.Model.MaxTokens},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrServer, err)
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrServer, resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s: %s", ErrServer, resp.Status, out.Error)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("%w: %d vectors for %d texts", ErrServer, len(out.Embeddings), len(texts))
	}
	for i, v := range out.Embeddings {
		out.Embeddings[i] = normalised(v)
	}
	return out.Embeddings, nil
}

func (o *Ollama) Close() error { return nil }

func normalised(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	n := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= n
	}
	return v
}

func cosine(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}
