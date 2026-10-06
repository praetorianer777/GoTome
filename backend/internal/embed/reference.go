package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

// Reference is the model's own output for a set of texts: token IDs and
// vectors from sentence-transformers on PyTorch, as spikes/embed/reference.py
// writes them.
type Reference struct {
	Model        string          `json:"model"`
	Revision     string          `json:"revision"`
	MaxSeqLength int             `json:"maxSeqLength"`
	Texts        []ReferenceText `json:"texts"`
}

type ReferenceText struct {
	Text   string    `json:"text"`
	IDs    []int     `json:"ids"`
	Vector []float32 `json:"vector"`
}

// LoadReference reads a reference file.
func LoadReference(path string) (Reference, error) {
	var ref Reference
	b, err := os.ReadFile(path)
	if err != nil {
		return ref, err
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		return ref, fmt.Errorf("%s: %w", path, err)
	}
	return ref, nil
}

// MinCosine is how near every vector must be to the reference's. The
// reference is of the fp32 weights; the int8 ones came to 0.982 at worst on
// x86-64 in #15, and arm64 runs other kernels.
const MinCosine = 0.97

// CheckResult is how an embedder compares with a reference.
type CheckResult struct {
	Texts int
	// TokenMismatches are the texts whose token IDs differ, when the
	// embedder says its tokens.
	TokenMismatches []string
	MinCosine       float64
	// Worst is the text of the least similar vector.
	Worst string
}

// Passed reports whether every token matched and every vector was near enough.
func (r CheckResult) Passed() bool {
	return len(r.TokenMismatches) == 0 && r.MinCosine >= MinCosine
}

// Check embeds the reference's texts and compares tokens, where the embedder
// says them, and vectors with the reference's.
func Check(ctx context.Context, e Embedder, ref Reference) (CheckResult, error) {
	m := e.Model()
	if ref.Model != m.Name || ref.Revision != m.Revision {
		return CheckResult{}, fmt.Errorf("the reference is of %s at %s, the embedder runs %s at %s", ref.Model, ref.Revision, m.Name, m.Revision)
	}
	res := CheckResult{Texts: len(ref.Texts), MinCosine: 1}
	tokens, hasTokens := e.(interface{ Tokens(string) []int })
	const batch = 8
	for start := 0; start < len(ref.Texts); start += batch {
		texts := ref.Texts[start:min(start+batch, len(ref.Texts))]
		in := make([]string, len(texts))
		for i, t := range texts {
			in[i] = t.Text
		}
		vecs, err := e.Embed(ctx, in)
		if err != nil {
			return res, err
		}
		for i, t := range texts {
			if hasTokens && !slices.Equal(tokens.Tokens(t.Text), t.IDs) {
				res.TokenMismatches = append(res.TokenMismatches, t.Text)
			}
			if c := Cosine(vecs[i], t.Vector); c < res.MinCosine {
				res.MinCosine, res.Worst = c, t.Text
			}
		}
	}
	return res, nil
}
