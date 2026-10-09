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

// Spec is a model and the files it runs from: the weights and tokenizer.json,
// or the name it has on an embedding server.
type Spec struct {
	Model Model
	// Dir is the folder under the model directory its files are kept in.
	Dir       string
	Weights   File
	Tokenizer File
	// Server is the model's name on an embedding server (Ollama), for a
	// model GOtome does not run itself.
	Server string
	// Prefix goes before every text the model embeds, as its authors ask.
	Prefix string
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
	// A book is compared with books, which the model's authors call a
	// symmetric task and give the query prefix.
	Prefix: "query: ",
}

// E5SmallFP32 is the same model with its full-precision weights: four times
// the download and a third more memory, for no better suggestions in #15,
// but faster on a CPU without int8 dot-product instructions.
var E5SmallFP32 = Spec{
	Model: Model{
		Name:      E5Small.Model.Name,
		Revision:  E5Small.Model.Revision,
		Weights:   "fp32",
		Dim:       384,
		MaxTokens: 512,
	},
	Dir: E5Small.Dir,
	Weights: File{
		Name:   "model.onnx",
		Size:   470268510,
		SHA256: "ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665",
	},
	Tokenizer: E5Small.Tokenizer,
	Prefix:    E5Small.Prefix,
}

// BGEM3 is BAAI's bge-m3 as an Ollama server runs it, on a GPU where there
// is one (#173): multilingual, 1,024 dimensions, five times e5-small's
// size. It reads 8,192 tokens, but is asked for as many as e5-small, the
// first 512 of a passage, which a whole chunk would take four times as
// long for. It takes no prefix.
var BGEM3 = Spec{
	Model: Model{
		Name:      "BAAI/bge-m3",
		Revision:  "ollama",
		Weights:   "bge-m3",
		Dim:       1024,
		MaxTokens: 512,
	},
	Server: "bge-m3",
}

// Specs are the models an administrator chooses between, by the names the
// setting embedding.model takes.
var Specs = map[string]Spec{
	"multilingual-e5-small":      E5Small,
	"multilingual-e5-small-fp32": E5SmallFP32,
	"bge-m3":                     BGEM3,
}

// DefaultSpec is the name in Specs used unless another is chosen.
const DefaultSpec = "multilingual-e5-small"

// Mean is the weighted mean of the vectors, normalised to length 1.
func Mean(vecs [][]float32, weights []float64) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	sum := make([]float64, len(vecs[0]))
	for i, v := range vecs {
		for k, x := range v {
			sum[k] += weights[i] * float64(x)
		}
	}
	var norm float64
	for _, x := range sum {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float32, len(sum))
	if norm == 0 {
		return out
	}
	for k, x := range sum {
		out[k] = float32(x / norm)
	}
	return out
}

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
