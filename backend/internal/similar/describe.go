package similar

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

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
	// loading is closed when the model being opened is, with loadErr set
	// when it failed.
	loading chan struct{}
	loadErr error
}

// ErrLoading is a model that is being opened and is not ready yet: opening
// takes longer than a request may wait, so the request is answered at once
// and the model goes on loading for the next.
var ErrLoading = errors.New("the embedding model is loading")

// loadWait is how long a request waits for a model that is opening before it
// answers ErrLoading. It stays under the 10 s perf-check allows a view.
var loadWait = 5 * time.Second

// loadLimit bounds opening a model, which downloads it when it may.
const loadLimit = 30 * time.Minute

// Warm starts opening the model for descriptions in the background when it
// is here to run without a download, so that the first search does not wait
// for it. A probe of a server can take a while: call it in a goroutine.
func (s *Service) Warm(ctx context.Context) {
	spec, _, err := s.config(ctx)
	if err != nil || s.open == nil || s.Probe == nil || s.Probe(ctx, spec).State != ReadyHere {
		return
	}
	s.queries.mu.Lock()
	defer s.queries.mu.Unlock()
	if s.queries.e == nil && s.queries.loading == nil {
		s.startOpen(spec)
	}
}

// startOpen opens the model in the background; the caller holds the lock.
func (s *Service) startOpen(spec embed.Spec) chan struct{} {
	done := make(chan struct{})
	s.queries.loading, s.queries.loadErr = done, nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), loadLimit)
		defer cancel()
		e, err := s.open(ctx, spec)
		s.queries.mu.Lock()
		if err == nil {
			s.queries.e, s.queries.model = e, spec.Model
		} else {
			s.queries.loadErr = err
		}
		s.queries.loading = nil
		s.queries.mu.Unlock()
		close(done)
	}()
	return done
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
	if s.NearPassages != nil {
		near, err := s.NearPassages(ctx, scope, libraryID, spec, vec)
		if err != nil {
			return nil, false, err
		}
		if len(near) > 0 {
			return s.describeBlended(ctx, scope, libraryID, spec, vec, near, offset, limit)
		}
	}
	rows, err := sqlc.New(s.pool).ListBooksNearQuery(ctx, sqlc.ListBooksNearQueryParams{
		Query: Literal(vec), Model: spec.Model.Name, ModelVersion: Version(spec.Model),
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
	out, err := s.withoutCopies(ctx, ids)
	return out, more, err
}

// blended is how many of the books nearest by their book vectors a search
// that also has passages ranks: the books its passages found are ranked
// with them, and pages go no further.
const blended = 500

// describeBlended ranks the books found by their passages with those found
// by their book vectors: a book with passages near the query by its best
// passage in place of its content vector, blended with its metadata vector
// as the content vector is (docs/decisions/passage-vectors.md).
func (s *Service) describeBlended(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, spec embed.Spec, vec []float32, near map[uuid.UUID]float64, offset, limit int) ([]uuid.UUID, bool, error) {
	q := sqlc.New(s.pool)
	model, version := spec.Model.Name, Version(spec.Model)
	rows, err := q.ListBooksNearQuery(ctx, sqlc.ListBooksNearQueryParams{
		Query: Literal(vec), Model: model, ModelVersion: version,
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID, Max: blended, Skip: 0,
	})
	if err != nil {
		return nil, false, err
	}
	score := make(map[uuid.UUID]float64, len(rows)+len(near))
	for _, r := range rows {
		score[r.BookID] = r.Score
	}
	ids := make([]uuid.UUID, 0, len(near))
	for id := range near {
		ids = append(ids, id)
	}
	vecs, err := q.ScoreMetadataNearQuery(ctx, sqlc.ScoreMetadataNearQueryParams{Query: Literal(vec), Ids: ids, Model: model, ModelVersion: version})
	if err != nil {
		return nil, false, err
	}
	metadata := make(map[uuid.UUID]float64, len(vecs))
	for _, v := range vecs {
		metadata[v.BookID] = v.Score
	}
	for id, p := range near {
		if m, ok := metadata[id]; ok {
			score[id] = 0.6*p + 0.4*m
		} else {
			score[id] = p
		}
	}
	ranked := make([]uuid.UUID, 0, len(score))
	for id := range score {
		ranked = append(ranked, id)
	}
	slices.SortFunc(ranked, func(a, b uuid.UUID) int {
		if score[a] != score[b] {
			if score[a] > score[b] {
				return -1
			}
			return 1
		}
		return strings.Compare(a.String(), b.String())
	})
	offset = max(offset, 0)
	if offset >= len(ranked) {
		return nil, false, nil
	}
	page := ranked[offset:min(offset+limit, len(ranked))]
	out, err := s.withoutCopies(ctx, page)
	return out, offset+limit < len(ranked), err
}

// withoutCopies leaves out a copy of a book listed before it.
func (s *Service) withoutCopies(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	all, err := traitsOf(ctx, sqlc.New(s.pool), ids)
	if err != nil {
		return nil, err
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
	return out, nil
}

func (s *Service) embedQuery(ctx context.Context, spec embed.Spec, text string) ([]float32, error) {
	vecs, err := s.EmbedQueries(ctx, spec, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// Spec is the model books are embedded with now.
func (s *Service) Spec(ctx context.Context) (embed.Spec, error) {
	spec, _, err := s.config(ctx)
	return spec, err
}

// EmbedQueries embeds texts as queries are, with the spec's prefix, by the
// model Describe keeps open. A model that cannot run here is
// ErrUnavailable.
func (s *Service) EmbedQueries(ctx context.Context, spec embed.Spec, texts []string) ([][]float32, error) {
	for {
		s.queries.mu.Lock()
		if s.queries.e != nil && s.queries.model != spec.Model {
			_ = s.queries.e.Close()
			s.queries.e = nil
		}
		if s.queries.e != nil {
			defer s.queries.mu.Unlock()
			prefixed := make([]string, len(texts))
			for i, t := range texts {
				prefixed[i] = spec.Prefix + t
			}
			return s.queries.e.Embed(ctx, prefixed)
		}
		if s.open == nil {
			s.queries.mu.Unlock()
			return nil, ErrUnavailable
		}
		if err := s.queries.loadErr; err != nil {
			s.queries.loadErr = nil
			s.queries.mu.Unlock()
			return nil, err
		}
		done := s.queries.loading
		if done == nil {
			done = s.startOpen(spec)
		}
		s.queries.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(loadWait):
			return nil, ErrLoading
		}
	}
}
