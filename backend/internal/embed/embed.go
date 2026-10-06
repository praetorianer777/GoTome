// Package embed turns text into vectors with multilingual-e5-small on ONNX
// Runtime, as docs/decisions/embedding-runtime.md decided: the runtime's
// library is in the image and loaded at run time, the model is downloaded
// on first use into the data directory and checked against pinned hashes,
// and the tokenizer is this package's own.
package embed

import (
	"context"
	"math"
)

// Embedder makes one vector per text.
type Embedder interface {
	// Embed returns an L2-normalised vector for each text, each text cut to
	// the model's MaxTokens tokens.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model names what made the vectors, so that vectors of another model,
	// revision or weights are known to need making again.
	Model() Model
	Close() error
}

// Model identifies a model and its weights.
type Model struct {
	Name      string
	Revision  string
	Weights   string
	Dim       int
	MaxTokens int
}

// File is one file of a model, at the pinned revision.
type File struct {
	Name   string
	Size   int64
	SHA256 string
}

// Spec is a model and the files it runs from: the weights and tokenizer.json.
type Spec struct {
	Model Model
	// Dir is the folder under the model directory its files are kept in.
	Dir       string
	Weights   File
	Tokenizer File
}

// Files are the spec's files, the weights first.
func (s Spec) Files() []File { return []File{s.Weights, s.Tokenizer} }

// E5Small is multilingual-e5-small with its int8 weights.
var E5Small = Spec{
	Model: Model{
		Name:      "intfloat/multilingual-e5-small",
		Revision:  "614241f622f53c4eeff9890bdc4f31cfecc418b3",
		Weights:   "int8",
		Dim:       384,
		MaxTokens: 512,
	},
	Dir: "multilingual-e5-small",
	Weights: File{
		Name:   "model_qint8_avx512_vnni.onnx",
		Size:   118346824,
		SHA256: "dd476dd0c2514e9b9be83aeb3853fac0763e0bdf4a71645407587d77c48a2d88",
	},
	Tokenizer: File{
		Name:   "tokenizer.json",
		Size:   17082730,
		SHA256: "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39",
	},
}

// Prefix goes before every passage of a book. A book is compared with
// books, which the model's authors call a symmetric task and give the query
// prefix.
const Prefix = "query: "

// Cosine is the cosine of two vectors of the same length.
func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
