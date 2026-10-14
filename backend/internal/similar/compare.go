package similar

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
)

// MinStoredCosine is how near a vector made again must be to the stored one
// for the two to count as one model's (#228): the same weights run another
// way differ only in their rounding.
const MinStoredCosine = 0.99

// Comparison is how vectors made again compare with those stored.
type Comparison struct {
	Books     int
	MinCosine float64
	Mean      float64
	Worst     uuid.UUID
}

// Passed says whether every vector made again is near enough its stored one.
func (c Comparison) Passed() bool { return c.Books > 0 && c.MinCosine >= MinStoredCosine }

// CompareStored makes the metadata vectors of a sample of books again with
// the embedder and compares each with the one stored for its model.
func CompareStored(ctx context.Context, q *sqlc.Queries, spec embed.Spec, e embed.Embedder, max int) (Comparison, error) {
	m := e.Model()
	rows, err := q.ListStoredMetadataVectors(ctx, sqlc.ListStoredMetadataVectorsParams{
		Model: m.Name, ModelVersion: Version(m), Max: int32(max),
	})
	if err != nil {
		return Comparison{}, err
	}
	c := Comparison{MinCosine: 1}
	const batch = 8
	for start := 0; start < len(rows); start += batch {
		part := rows[start:min(start+batch, len(rows))]
		texts := make([]string, len(part))
		for i, r := range part {
			texts[i] = spec.Prefix + r.Text
		}
		vecs, err := e.Embed(ctx, texts)
		if err != nil {
			return Comparison{}, err
		}
		for i, r := range part {
			stored, err := parseLiteral(r.Embedding)
			if err != nil {
				return Comparison{}, fmt.Errorf("book %s: %w", r.BookID, err)
			}
			cos := cosine(vecs[i], stored)
			c.Books++
			c.Mean += cos
			if cos < c.MinCosine {
				c.MinCosine, c.Worst = cos, r.BookID
			}
		}
	}
	if c.Books > 0 {
		c.Mean /= float64(c.Books)
	}
	return c, nil
}

// parseLiteral reads a vector as pgvector writes it.
func parseLiteral(s string) ([]float32, error) {
	parts := strings.Split(strings.Trim(s, "[]"), ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, err
		}
		out[i] = float32(f)
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / math.Sqrt(na*nb)
}
