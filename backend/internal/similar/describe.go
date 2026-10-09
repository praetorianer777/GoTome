package similar

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MaxDescribed is the most books a page of Describe holds.
const MaxDescribed = 100

// queries holds the model a description is embedded with, opened on the
// first and kept: opening it takes longer than a search may.
type queries struct {
	mu    sync.Mutex
	model embed.Model
	e     embed.Embedder
}

// Describe returns the books whose content and metadata are nearest a
// description of what someone wants to read ("a detective story set in
// Venice"), among those the scope sees, of one library when named: the
// best first, of limit after skipping offset less the copies of a book
// before them, and whether more follow.
// A model that cannot run here is ErrUnavailable.
func (s *Service) Describe(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, text string, offset, limit int) ([]uuid.UUID, bool, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return nil, false, err
	}
	vec, err := s.embedQuery(ctx, spec, strings.Join(strings.Fields(text), " "))
	if err != nil {
		return nil, false, err
	}
	limit = min(max(limit, 1), MaxDescribed)
	rows, err := sqlc.New(s.pool).ListBooksNearQuery(ctx, sqlc.ListBooksNearQueryParams{
		Query: literal(vec), Model: spec.Model.Name, ModelVersion: Version(spec.Model),
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
		Max: int32(limit + 1), Skip: int32(max(offset, 0)),
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	rows = rows[:min(len(rows), limit)]
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.BookID
	}
	// A copy of a book listed before it on the page is left out.
	all, err := traitsOf(ctx, sqlc.New(s.pool), ids)
	if err != nil {
		return nil, false, err
	}
	var kept []traits
	for _, id := range ids {
		t := all[id]
		if !slices.ContainsFunc(kept, func(o traits) bool { return isCopy(o, t) }) {
			kept = append(kept, t)
		}
	}
	out := make([]uuid.UUID, len(kept))
	for i, t := range kept {
		out[i] = t.id
	}
	return out, more, nil
}

func (s *Service) embedQuery(ctx context.Context, spec embed.Spec, text string) ([]float32, error) {
	s.queries.mu.Lock()
	defer s.queries.mu.Unlock()
	if s.queries.e != nil && s.queries.model != spec.Model {
		_ = s.queries.e.Close()
		s.queries.e = nil
	}
	if s.queries.e == nil {
		if s.open == nil {
			return nil, ErrUnavailable
		}
		e, err := s.open(ctx, spec)
		if err != nil {
			return nil, err
		}
		s.queries.e, s.queries.model = e, spec.Model
	}
	vecs, err := s.queries.e.Embed(ctx, []string{spec.Prefix + text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}
