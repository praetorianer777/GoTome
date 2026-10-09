// Package similar keeps what each book is about as vectors, for suggesting
// similar books: a content vector from passages of its text and a metadata
// vector from its title, authors, series, tags and description
// (docs/decisions/embedding-runtime.md). Nothing about which books are
// done is kept but the vectors themselves: a book whose vectors are
// missing, of another model, or made from what has changed since, is
// embedded on the next pass, so a pass may stop anywhere and the next one
// goes on.
package similar

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"golang.org/x/sync/errgroup"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Kinds of vector, as book_vectors.kind stores them.
const (
	KindContent  = "content"
	KindMetadata = "metadata"
)

// Samples is how many passages of a book's text its content vector is made
// from, spread evenly over it.
const Samples = 16

// recipe is how a content vector is made, part of its source's hash: a
// change to it has every book's made again.
var recipe = "samples=" + strconv.Itoa(Samples)

// batch is how many passages go to the model at once, as in #15.
const batch = 8

// page is how many books one look at the catalogue takes.
const page = 500

// ErrUnavailable is the error of an Opener that cannot run the model here:
// no runtime, or no model and no permission to download it. Embedding then
// waits for the next pass rather than being retried.
var ErrUnavailable = errors.New("embedding is not available")

// Opener opens the model of a spec.
type Opener func(ctx context.Context, spec embed.Spec) (embed.Embedder, error)

// OnnxOpener opens a spec on ONNX Runtime with the options' runtime and
// folders. No runtime, or a model missing where it may not be downloaded,
// is ErrUnavailable.
func OnnxOpener(opts embed.OnnxOptions) Opener {
	return func(ctx context.Context, spec embed.Spec) (embed.Embedder, error) {
		if spec.Server != "" {
			return nil, fmt.Errorf("%w: %s runs only on an embedding server", ErrUnavailable, spec.Model.Name)
		}
		if _, err := embed.LoadRuntime(opts.Runtime); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		o, err := embed.OpenOnnx(ctx, spec, opts)
		if errors.Is(err, embed.ErrModelMissing) {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if err != nil {
			return nil, err
		}
		return o, nil
	}
}

// ServerOpener opens a model of an embedding server (a spec with a Server)
// on the server the address names, and any other one with local. No
// server named, one that does not answer, or one with another model is
// ErrUnavailable, with why.
func ServerOpener(local Opener, address func(ctx context.Context) (string, error)) Opener {
	return func(ctx context.Context, spec embed.Spec) (embed.Embedder, error) {
		if spec.Server == "" {
			return local(ctx, spec)
		}
		url, err := address(ctx)
		if err != nil {
			return nil, err
		}
		if url == "" {
			return nil, fmt.Errorf("%w: %s runs on an embedding server, and embedding.server names none", ErrUnavailable, spec.Model.Name)
		}
		o, err := embed.OpenOllama(ctx, url, spec)
		if errors.Is(err, embed.ErrServer) || errors.Is(err, embed.ErrNotTheModel) {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if err != nil {
			return nil, err
		}
		return o, nil
	}
}

// Config says which model books are embedded with, and whether they are
// embedded at all.
type Config func(ctx context.Context) (spec embed.Spec, enabled bool, err error)

// Queue enqueues the embedding job.
type Queue interface {
	Insert(ctx context.Context, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service embeds books.
type Service struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	config Config
	open   Opener
	Queue  Queue
	// queries embeds the descriptions Describe is asked about.
	queries queries
}

// NewService returns the Service; Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, config Config, open Opener, log *slog.Logger) *Service {
	return &Service{pool: pool, config: config, open: open, log: log}
}

// Version is how a model's revision and weights are stored.
func Version(m embed.Model) string { return m.Revision + "/" + m.Weights }

// Embed works through the books whose vectors of the chosen model are
// missing or stale, in ID order, until none is left, the deadline has
// passed, at most max books were embedded (0 is no limit), or embedding is
// switched off. It reports whether it got through all of them; switched
// off counts as through. A model on a server embeds as many books at once
// as the server takes; one that stops answering ends the pass as
// ErrUnavailable.
func (s *Service) Embed(ctx context.Context, deadline time.Time, max int) (complete bool, err error) {
	spec, enabled, err := s.config(ctx)
	if err != nil || !enabled {
		return true, err
	}
	q := sqlc.New(s.pool)
	if _, err := q.DeleteVectorsOfGoneBooks(ctx); err != nil {
		return false, err
	}
	model, version := spec.Model.Name, Version(spec.Model)
	var e embed.Embedder
	defer func() {
		if e != nil {
			e.Close()
		}
	}()
	done := 0
	after := uuid.Nil
	var group []sqlc.ListBooksToEmbedRow
	// flush embeds the books gathered, at once where the model allows.
	flush := func() error {
		g, gctx := errgroup.WithContext(ctx)
		for _, r := range group {
			g.Go(func() error {
				if err := s.embedBook(gctx, e, spec, r); err != nil {
					return fmt.Errorf("embed book %s: %w", r.BookID, err)
				}
				return nil
			})
		}
		err := g.Wait()
		done += len(group)
		group = group[:0]
		if errors.Is(err, embed.ErrServer) {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return err
	}
	for {
		rows, err := q.ListBooksToEmbed(ctx, sqlc.ListBooksToEmbedParams{
			Model: model, ModelVersion: version, Recipe: recipe, After: after, Page: page,
		})
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return true, flush()
		}
		for _, r := range rows {
			after = r.BookID
			if r.ContentGone {
				if err := q.DeleteBookVector(ctx, sqlc.DeleteBookVectorParams{BookID: r.BookID, Kind: KindContent}); err != nil {
					return false, err
				}
			}
			if !r.MetadataStale && !r.ContentStale {
				continue
			}
			if (max > 0 && done+len(group) >= max) || time.Now().After(deadline) {
				return false, flush()
			}
			// Asked for each book, so that switching it off stops a
			// pass within a book's time, and another model starts the
			// next one.
			now, on, err := s.config(ctx)
			if err != nil || !on {
				return true, errors.Join(err, flush())
			}
			if now.Model != spec.Model {
				return false, flush()
			}
			if e == nil {
				if e, err = s.open(ctx, spec); err != nil {
					return false, err
				}
			}
			group = append(group, r)
			if len(group) >= parallel(e) {
				if err := flush(); err != nil {
					return false, err
				}
			}
		}
	}
}

// parallel is how many books an embedder takes at once: one, unless it says
// otherwise, as a server that runs several requests side by side does.
func parallel(e embed.Embedder) int {
	if p, ok := e.(interface{ Parallel() int }); ok {
		return max(p.Parallel(), 1)
	}
	return 1
}

// embedBook makes the book's stale vectors and stores them together.
func (s *Service) embedBook(ctx context.Context, e embed.Embedder, spec embed.Spec, r sqlc.ListBooksToEmbedRow) error {
	q := sqlc.New(s.pool)
	type made struct {
		kind string
		hash []byte
		vec  []float32
	}
	var out []made
	if r.MetadataStale {
		text, err := q.GetBookMetadataText(ctx, r.BookID)
		if err != nil {
			return err
		}
		vecs, err := e.Embed(ctx, []string{spec.Prefix + text})
		if err != nil {
			return err
		}
		// The hash of the text embedded, which is what the query hashes:
		// a change in between is found on the next pass.
		sum := sha256.Sum256([]byte(text))
		out = append(out, made{KindMetadata, sum[:], vecs[0]})
	}
	if r.ContentStale && r.TextFileID != nil {
		vec, err := s.contentVector(ctx, e, spec.Prefix, *r.TextFileID)
		if err != nil {
			return err
		}
		if vec == nil {
			if err := q.DeleteBookVector(ctx, sqlc.DeleteBookVectorParams{BookID: r.BookID, Kind: KindContent}); err != nil {
				return err
			}
		} else {
			out = append(out, made{KindContent, r.ContentHash, vec})
		}
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		for _, m := range out {
			if err := q.PutBookVector(ctx, sqlc.PutBookVectorParams{
				BookID: r.BookID, Kind: m.kind, Model: spec.Model.Name, ModelVersion: Version(spec.Model),
				SourceHash: m.hash, Embedding: literal(m.vec),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// contentVector embeds Samples passages spread evenly over the file's
// chunks and takes their mean, each weighted by how much of it the model
// read; nil for a file without chunks.
func (s *Service) contentVector(ctx context.Context, e embed.Embedder, prefix string, fileID uuid.UUID) ([]float32, error) {
	q := sqlc.New(s.pool)
	positions, err := q.ListChunkPositions(ctx, fileID)
	if err != nil || len(positions) == 0 {
		return nil, err
	}
	bodies, err := q.ListChunkBodies(ctx, sqlc.ListChunkBodiesParams{FileID: fileID, Positions: spread(positions, Samples)})
	if err != nil || len(bodies) == 0 {
		return nil, err
	}
	tokens, counts := e.(interface{ Tokens(string) []int })
	var vecs [][]float32
	var weights []float64
	for start := 0; start < len(bodies); start += batch {
		texts := make([]string, 0, batch)
		for _, b := range bodies[start:min(start+batch, len(bodies))] {
			texts = append(texts, prefix+b)
		}
		got, err := e.Embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		vecs = append(vecs, got...)
		for _, t := range texts {
			if counts {
				weights = append(weights, float64(len(tokens.Tokens(t))))
			} else {
				weights = append(weights, float64(utf8.RuneCountInString(t)))
			}
		}
	}
	return embed.Mean(vecs, weights), nil
}

// spread picks n of the positions, evenly from the middles of n equal
// parts; all of them when there are no more than n.
func spread(positions []int32, n int) []int32 {
	if len(positions) <= n {
		return positions
	}
	out := make([]int32, n)
	for i := range n {
		out[i] = positions[(2*i+1)*len(positions)/(2*n)]
	}
	return out
}

// literal is a vector as pgvector reads it: [0.1,-0.2,…].
func literal(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// MaxSimilar is the most books Similar returns.
const MaxSimilar = 50

// A list of similar books takes at most perAuthor books by one author and
// perSeries from one series, the nearest of each: the book page links to
// an author's and a series' other books already, and without the caps
// half the list is by the book's own author (docs/similarity.md). It looks
// through candidates times as many of the nearest books as it shows.
const (
	perAuthor  = 2
	perSeries  = 2
	candidates = 20
)

// Similar returns the books nearest the given one among those the scope
// sees, the nearest first, at most limit of them (MaxSimilar at most):
// none that is a copy of the book or of one taken before, and no more than
// perAuthor by one author and perSeries from one series. A book without
// vectors of the chosen model has none yet; whether the caller may see the
// book itself is the caller's to check.
func (s *Service) Similar(ctx context.Context, scope library.Scope, bookID uuid.UUID, limit int) ([]uuid.UUID, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), MaxSimilar)
	q := sqlc.New(s.pool)
	rows, err := q.ListSimilarBooks(ctx, sqlc.ListSimilarBooksParams{
		BookID: bookID, Model: spec.Model.Name, ModelVersion: Version(spec.Model),
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Max: int32(limit * candidates),
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.BookID
	}
	all, err := traitsOf(ctx, q, append([]uuid.UUID{bookID}, ids...))
	if err != nil {
		return nil, err
	}
	near := make([]traits, len(ids))
	for i, id := range ids {
		near[i] = all[id]
	}
	return pick(all[bookID], near, limit), nil
}

// traits are what a list of similar books is chosen by.
type traits struct {
	id     uuid.UUID
	title  string
	series *uuid.UUID
	// people are the authors' person_keys, the first author first.
	people []string
}

func traitsOf(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) (map[uuid.UUID]traits, error) {
	rows, err := q.ListBookTraits(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]traits, len(rows))
	for _, r := range rows {
		out[r.ID] = traits{id: r.ID, title: r.Title, series: r.SeriesID, people: r.People}
	}
	return out, nil
}

// pick takes from the nearest books, in order, up to limit that are no copy
// of the book or of one taken, within the caps per author and series.
func pick(book traits, near []traits, limit int) []uuid.UUID {
	var taken []traits
	byAuthor := map[string]int{}
	bySeries := map[uuid.UUID]int{}
	for _, t := range near {
		if len(taken) == limit {
			break
		}
		if isCopy(book, t) || slices.ContainsFunc(taken, func(o traits) bool { return isCopy(o, t) }) {
			continue
		}
		if len(t.people) > 0 && byAuthor[t.people[0]] >= perAuthor {
			continue
		}
		if t.series != nil && bySeries[*t.series] >= perSeries {
			continue
		}
		taken = append(taken, t)
		if len(t.people) > 0 {
			byAuthor[t.people[0]]++
		}
		if t.series != nil {
			bySeries[*t.series]++
		}
	}
	ids := make([]uuid.UUID, len(taken))
	for i, t := range taken {
		ids[i] = t.id
	}
	return ids
}

// isCopy reports whether two books are one with an author in common: the
// same title once a shop's additions are off, or one the other's with a
// subtitle after a colon ("Rauklands Sohn" and "Rauklands Sohn: Raukland
// Trilogie"). Two titles that both have a subtitle of their own after the
// same words are two books, as the volumes of a series often are.
func isCopy(a, b traits) bool {
	if !slices.ContainsFunc(a.people, func(p string) bool { return slices.Contains(b.people, p) }) {
		return false
	}
	ka, kb := titleKey(a.title), titleKey(b.title)
	return ka == kb || ka == mainTitleKey(b.title) || mainTitleKey(a.title) == kb
}

// titleKey is a title as copies of a book share it.
func titleKey(title string) string { return catalog.Key(cleanup.CleanTitle(title)) }

// mainTitleKey is titleKey of what comes before a subtitle.
func mainTitleKey(title string) string {
	main, _, _ := strings.Cut(cleanup.CleanTitle(title), ":")
	return catalog.Key(main)
}
