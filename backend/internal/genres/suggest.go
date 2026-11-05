package genres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// Off is the setting genres.suggest while nothing is written.
const Off = "off"

// Modes are the values of the setting genres.suggest: Off, the tags alone,
// or the tags and one way of suggesting.
var Modes = []string{Off, MethodTags, MethodVector, MethodNeighbours, MethodCombined}

// page is how many books one look at the catalogue takes.
const page = 500

// Queue enqueues the job.
type Queue interface {
	Insert(ctx context.Context, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service gives books genres.
type Service struct {
	pool    *pgxpool.Pool
	log     *slog.Logger
	vectors *similar.Service
	mode    func(ctx context.Context) (string, error)
	Queue   Queue

	mu     sync.Mutex
	model  embed.Model
	genres [][]float32
}

// NewService returns the Service; mode reads the setting genres.suggest.
// Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, vectors *similar.Service, mode func(ctx context.Context) (string, error), log *slog.Logger) *Service {
	return &Service{pool: pool, vectors: vectors, mode: mode, log: log}
}

// genreVectors are the genres' vectors in List's order, each the mean of
// its descriptions embedded as queries, made once per model.
func (s *Service) genreVectors(ctx context.Context, spec embed.Spec) ([][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.genres != nil && s.model == spec.Model {
		return s.genres, nil
	}
	vecs, err := GenreVectors(ctx, spec, s.vectors)
	if err != nil {
		return nil, err
	}
	s.model, s.genres = spec.Model, vecs
	return vecs, nil
}

// GenreVectors embeds the genres' descriptions with the model of the spec.
func GenreVectors(ctx context.Context, spec embed.Spec, vectors *similar.Service) ([][]float32, error) {
	out := make([][]float32, len(List))
	const batch = 8
	var texts []string
	for _, g := range List {
		texts = append(texts, g.texts()...)
	}
	var all [][]float32
	for start := 0; start < len(texts); start += batch {
		vecs, err := vectors.EmbedQueries(ctx, spec, texts[start:min(start+batch, len(texts))])
		if err != nil {
			return nil, err
		}
		all = append(all, vecs...)
	}
	per := len(texts) / len(List)
	for i := range List {
		mine := all[i*per : (i+1)*per]
		out[i] = embed.Mean(mine, slices.Repeat([]float64{1}, len(mine)))
	}
	return out, nil
}

// pass is what one pass needs to know.
type pass struct {
	mode   string
	recipe string
	spec   embed.Spec
	genres [][]float32
}

// Suggest works through the books a pass of the setting's way has not
// looked at, or that changed since, in ID order, until none is left or the
// deadline has passed, and reports whether it got through them. Off does
// nothing. A model that cannot run gives the tags' genres alone, and the
// next pass that can run it looks at every book again.
func (s *Service) Suggest(ctx context.Context, deadline time.Time) (complete bool, err error) {
	mode, err := s.mode(ctx)
	if err != nil || mode == Off {
		return true, err
	}
	spec, err := s.vectors.Spec(ctx)
	if err != nil {
		return false, err
	}
	p := pass{mode: mode, spec: spec}
	if mode != MethodTags {
		p.genres, err = s.genreVectors(ctx, spec)
		if errors.Is(err, similar.ErrUnavailable) {
			s.log.Warn("genres are suggested from tags alone", "error", err)
			p.mode = MethodTags
		} else if err != nil {
			return false, err
		}
	}
	p.recipe = recipe(p.mode, spec)
	q := sqlc.New(s.pool)
	after := uuid.Nil
	for {
		ids, err := q.ListBooksForGenres(ctx, sqlc.ListBooksForGenresParams{
			After: after, Recipe: p.recipe, Model: spec.Model.Name, Page: page,
		})
		if err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return true, nil
		}
		for _, id := range ids {
			if time.Now().After(deadline) {
				return false, nil
			}
			if err := s.suggestBook(ctx, p, id); err != nil {
				return false, fmt.Errorf("genres of book %s: %w", id, err)
			}
			after = id
		}
	}
}

// recipe says how a book's genres were found: a change has every book
// looked at again.
func recipe(mode string, spec embed.Spec) string {
	r := mode + "/" + Version()
	if mode != MethodTags {
		r += "/" + spec.Model.Name + "/" + similar.Version(spec.Model) +
			"/n" + strconv.Itoa(Neighbours) + "-" + strconv.FormatFloat(NeighbourShare, 'g', -1, 64)
	}
	return r
}

func (s *Service) suggestBook(ctx context.Context, p pass, id uuid.UUID) error {
	q := sqlc.New(s.pool)
	evidence, err := q.ListGenreEvidence(ctx, []uuid.UUID{id})
	if err != nil {
		return err
	}
	if len(evidence) == 0 {
		return nil
	}
	e := evidence[0]
	fromTags := FromTags(e.Tags)
	have := unique(append(slices.Clone(fromTags), e.Accepted...))
	skip := append(slices.Clone(have), e.Rejected...)
	var found []Scored
	var method string
	if p.mode != MethodTags && len(have) < MaxGenres {
		book, library, err := s.bookVectors(ctx, p.spec, id)
		if err != nil {
			return err
		}
		var shares map[string]float64
		if (p.mode == MethodNeighbours || p.mode == MethodCombined) && library != uuid.Nil {
			near, err := s.neighbours(ctx, p.spec, id, library, book)
			if err != nil {
				return err
			}
			shares = Shares(near)
			found = pickShares(shares, NeighbourShare, skip, MaxGenres-len(have))
			method = MethodNeighbours
		}
		// Combined asks the vectors only where the neighbours cannot tell:
		// against the descriptions they separate genres no better than
		// guessing the commonest (docs/genres.md), so where both answer
		// they could only take the neighbours' place with worse.
		if p.mode == MethodVector || (p.mode == MethodCombined && shares == nil) {
			found = pickMargins(Margins(book, p.genres), Genre.threshold, skip, MaxGenres-len(have))
			method = MethodVector
		}
	}
	keep := slices.Clone(fromTags)
	for _, f := range found {
		keep = append(keep, f.Genre)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.DropStaleGenres(ctx, sqlc.DropStaleGenresParams{BookID: id, Keep: keep}); err != nil {
			return err
		}
		for _, g := range fromTags {
			if err := q.PutTagGenre(ctx, sqlc.PutTagGenreParams{BookID: id, Genre: g}); err != nil {
				return err
			}
		}
		for _, f := range found {
			if err := q.PutAutomaticGenre(ctx, sqlc.PutAutomaticGenreParams{
				BookID: id, Genre: f.Genre, Method: method, Score: float32(f.Score),
			}); err != nil {
				return err
			}
		}
		return q.MarkGenresChecked(ctx, sqlc.MarkGenresCheckedParams{BookID: id, Recipe: p.recipe})
	})
}

// bookVectors reads a book's vectors of the spec's model, and its library.
func (s *Service) bookVectors(ctx context.Context, spec embed.Spec, id uuid.UUID) (Vectors, uuid.UUID, error) {
	rows, err := sqlc.New(s.pool).GetGenreVectors(ctx, sqlc.GetGenreVectorsParams{
		BookID: id, Model: spec.Model.Name, ModelVersion: similar.Version(spec.Model),
	})
	if err != nil {
		return Vectors{}, uuid.Nil, err
	}
	var v Vectors
	var library uuid.UUID
	for _, r := range rows {
		vec, err := similar.ParseLiteral(r.Embedding)
		if err != nil {
			return Vectors{}, uuid.Nil, err
		}
		library = r.LibraryID
		switch r.Kind {
		case similar.KindContent:
			v.Content = vec
		case similar.KindMetadata:
			v.Metadata = vec
		}
	}
	return v, library, nil
}

// neighbours are the nearest books of the library to the vectors, with
// the genres each is known to have.
func (s *Service) neighbours(ctx context.Context, spec embed.Spec, id, library uuid.UUID, book Vectors) ([]Near, error) {
	if book.Content == nil && book.Metadata == nil {
		return nil, nil
	}
	q := sqlc.New(s.pool)
	params := sqlc.ListGenreNeighboursParams{
		BookID: id, LibraryID: library, Model: spec.Model.Name, ModelVersion: similar.Version(spec.Model),
		Max: Candidates,
	}
	if book.Content != nil {
		c := similar.Literal(book.Content)
		params.Content = &c
	}
	if book.Metadata != nil {
		m := similar.Literal(book.Metadata)
		params.Metadata = &m
	}
	rows, err := q.ListGenreNeighbours(ctx, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.BookID
	}
	known, err := s.known(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Near, len(rows))
	for i, r := range rows {
		out[i] = Near{Score: r.Score, Genres: known[r.BookID]}
	}
	return out, nil
}

// known are the genres each of the books is known to have.
func (s *Service) known(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]string, error) {
	rows, err := sqlc.New(s.pool).ListGenreEvidence(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]string, len(rows))
	for _, r := range rows {
		out[r.ID] = knownGenres(r.Tags, r.Accepted, r.Rejected)
	}
	return out, nil
}
