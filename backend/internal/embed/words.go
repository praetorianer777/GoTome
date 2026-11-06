package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
	"unicode"
)

// wordSlots is how many places of a vector each word takes.
const wordSlots = 8

// Words makes vectors from the words of a text instead of a model: each
// word, in lower case and counted once, adds to a few places its hash
// picks, so texts that share words are near and texts that share none
// nearly orthogonal. Similar books and genre suggestions then have
// something to go by where no model can run: the browser tests' stack
// embeds with it (GOTOME_TEST_EMBEDDINGS). What it finds near means
// nothing about what books are about.
type Words struct {
	// As is the model it passes for, whose length its vectors have.
	As Model
}

func (w *Words) Model() Model { return w.As }

func (w *Words) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v := make([]float64, w.As.Dim)
		seen := map[string]bool{}
		for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if seen[word] {
				continue
			}
			seen[word] = true
			sum := sha256.Sum256([]byte(word))
			for k := range wordSlots {
				n := binary.LittleEndian.Uint32(sum[k*4:])
				sign := 1.0
				if n&1 == 1 {
					sign = -1
				}
				v[int(n>>1)%len(v)] += sign
			}
		}
		var norm float64
		for _, x := range v {
			norm += x * x
		}
		out := make([]float32, len(v))
		if norm == 0 {
			// A text without words still needs a vector of unit length.
			out[0] = 1
		} else {
			norm = math.Sqrt(norm)
			for k, x := range v {
				out[k] = float32(x / norm)
			}
		}
		vecs[i] = out
	}
	return vecs, nil
}

func (w *Words) Close() error { return nil }
