package similar

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Weight is how much a book counts in what a person likes: by its status
// and the stars they gave it.
func (t Taste) Weight(status string, rating *int16) float64 { return t.weight(status, rating) }

// Profile returns what the scope's user likes, as Recommend works it out,
// and whether there is a profile: fewer than MinTaste finished or rated
// books give none. A model that cannot run here does not matter to it; the
// vectors it needs are stored.
func (s *Service) Profile(ctx context.Context, scope library.Scope) (Profile, bool, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return Profile{}, false, err
	}
	books, err := s.taste(ctx, scope, spec, DefaultTaste)
	if err != nil {
		return Profile{}, false, err
	}
	p, ok := buildProfile(books, DefaultTaste)
	return p, ok, nil
}

// Resemblance is how near a text is to a profile: the cosine to its nearest
// centre, and the books of that centre the text is nearest.
type Resemblance struct {
	Score   float64
	Because []Because
}

// Resemble says how near each text is to the profile, by the text embedded
// as a book's metadata is. A text that is empty gets nil. A model that
// cannot run here, or embedding switched off, is ErrUnavailable.
func (s *Service) Resemble(ctx context.Context, p Profile, texts []string) ([]*Resemblance, error) {
	out := make([]*Resemblance, len(texts))
	spec, enabled, err := s.config(ctx)
	if err != nil {
		return out, err
	}
	if !enabled {
		return out, ErrUnavailable
	}
	vecs, err := s.vectorsOf(ctx, spec, texts)
	if err != nil {
		return out, err
	}
	var titled []Because
	for i, v := range vecs {
		if v == nil {
			continue
		}
		r := &Resemblance{Score: -1}
		var best centre
		for _, c := range p.centres {
			key := c.metadata
			if key == nil {
				key = c.content
			}
			if key == nil {
				continue
			}
			if score := Cosine(v, key); score > r.Score {
				r.Score, best = score, c
			}
		}
		if r.Score < -0.5 {
			continue
		}
		type near struct {
			b     Because
			score float64
		}
		var ranked []near
		for _, m := range best.members {
			if m.weight <= 0 || m.metadata == nil {
				continue
			}
			ranked = append(ranked, near{Because{ID: m.id}, Cosine(v, m.metadata)})
		}
		slices.SortStableFunc(ranked, func(a, b near) int {
			switch {
			case a.score > b.score:
				return -1
			case a.score < b.score:
				return 1
			}
			return slices.Compare(a.b.ID[:], b.b.ID[:])
		})
		for _, n := range ranked[:min(because, len(ranked))] {
			r.Because = append(r.Because, n.b)
		}
		titled = append(titled, r.Because...)
		out[i] = r
	}
	if len(titled) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(titled))
	for i, b := range titled {
		ids[i] = b.ID
	}
	rows, err := sqlc.New(s.pool).ListBookTitles(ctx, ids)
	if err != nil {
		return out, err
	}
	byID := make(map[uuid.UUID]string, len(rows))
	for _, t := range rows {
		byID[t.ID] = t.Title
	}
	for _, r := range out {
		if r == nil {
			continue
		}
		for j := range r.Because {
			r.Because[j].Title = byID[r.Because[j].ID]
		}
	}
	return out, nil
}
