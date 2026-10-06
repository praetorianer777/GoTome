package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// Fake makes vectors from the text's hash: the same text the same vector,
// different texts nearly orthogonal ones. Tests use it where the model
// is not downloaded.
type Fake struct {
	Dim int
}

func (f *Fake) Model() Model {
	return Model{Name: "fake", Revision: "1", Weights: "none", Dim: f.dim(), MaxTokens: 512}
}

func (f *Fake) dim() int {
	if f.Dim > 0 {
		return f.Dim
	}
	return E5Small.Model.Dim
}

func (f *Fake) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v := make([]float32, f.dim())
		seed := sha256.Sum256([]byte(t))
		var norm float64
		for k := range v {
			if k%8 == 0 && k > 0 {
				seed = sha256.Sum256(seed[:])
			}
			x := float64(int32(binary.LittleEndian.Uint32(seed[k%8*4:]))) / math.MaxInt32
			v[k] = float32(x)
			norm += x * x
		}
		norm = math.Sqrt(norm)
		for k := range v {
			v[k] = float32(float64(v[k]) / norm)
		}
		vecs[i] = v
	}
	return vecs, nil
}

func (f *Fake) Close() error { return nil }
