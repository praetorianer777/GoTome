package similar

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
)

// maxCompared is how many texts' vectors are kept: a candidate's
// description is looked at again by every lookup of the book.
const maxCompared = 4000

// comparedTexts remembers the vectors of texts embedded as queries by the
// hash of the model and the text, so that a description that a source
// returns again is not embedded again. It is emptied when full: the
// descriptions looked at next are the ones to keep.
type comparedTexts struct {
	mu sync.Mutex
	m  map[[sha256.Size]byte][]float32
}

func (c *comparedTexts) get(key [sha256.Size]byte) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *comparedTexts) put(key [sha256.Size]byte, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= maxCompared {
		c.m = map[[sha256.Size]byte][]float32{}
	}
	c.m[key] = v
}

// CompareToBook says how near each text is to the book's text: the cosine of
// the text embedded as a query, with the model's prefix, to the book's
// content vector of the chosen model. A text that cannot be compared (the
// book has no content vector, or the text is empty) gets nil. A model that
// cannot run here, or embedding switched off, is ErrUnavailable; a book
// without a content vector embeds nothing.
func (s *Service) CompareToBook(ctx context.Context, bookID uuid.UUID, texts []string) ([]*float64, error) {
	out := make([]*float64, len(texts))
	spec, enabled, err := s.config(ctx)
	if err != nil {
		return out, err
	}
	if !enabled {
		return out, ErrUnavailable
	}
	q := sqlc.New(s.pool)
	model, version := spec.Model.Name, Version(spec.Model)
	has, err := q.HasBookContentVector(ctx, sqlc.HasBookContentVectorParams{BookID: bookID, Model: model, ModelVersion: version})
	if err != nil || !has {
		return out, err
	}
	vecs, err := s.vectorsOf(ctx, spec, texts)
	if err != nil {
		return out, err
	}
	for i, v := range vecs {
		if v == nil {
			continue
		}
		cosine, err := q.CosineToBookContent(ctx, sqlc.CosineToBookContentParams{
			Query: Literal(v), BookID: bookID, Model: model, ModelVersion: version,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return make([]*float64, len(texts)), nil
		}
		if err != nil {
			return out, err
		}
		out[i] = &cosine
	}
	return out, nil
}

// vectorsOf embeds the texts that are not remembered, in one call.
func (s *Service) vectorsOf(ctx context.Context, spec embed.Spec, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	keys := make([][sha256.Size]byte, len(texts))
	var missing []int
	var asked []string
	for i, text := range texts {
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		keys[i] = sha256.Sum256([]byte(spec.Model.Name + "\x00" + Version(spec.Model) + "\x00" + text))
		if v, ok := s.compared.get(keys[i]); ok {
			out[i] = v
			continue
		}
		missing = append(missing, i)
		asked = append(asked, text)
	}
	if len(asked) == 0 {
		return out, nil
	}
	var vecs [][]float32
	var err error
	for {
		vecs, err = s.EmbedQueries(ctx, spec, asked)
		if !errors.Is(err, ErrLoading) || !s.WaitForModel {
			break
		}
	}
	if err != nil {
		return out, err
	}
	for j, i := range missing {
		out[i] = vecs[j]
		s.compared.put(keys[i], vecs[j])
	}
	return out, nil
}
