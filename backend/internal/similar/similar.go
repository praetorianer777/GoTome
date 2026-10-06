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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
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
}

// NewService returns the Service; Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, config Config, open Opener, log *slog.Logger) *Service {
	return &Service{pool: pool, config: config, open: open, log: log}
}

// Version is how a model's revision and weights are stored.
func Version(m embed.Model) string { return m.Revision + "/" + m.Weights }

// Embed works through the books whose vectors are missing or stale, in ID
// order, until none is left, the deadline has passed, at most max books
// were embedded (0 is no limit), or embedding is switched off. It reports
// whether it got through all of them; switched off counts as through.
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
	for {
		rows, err := q.ListBooksToEmbed(ctx, sqlc.ListBooksToEmbedParams{
			Model: model, ModelVersion: version, Recipe: recipe, After: after, Page: page,
		})
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return true, nil
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
			if (max > 0 && done >= max) || time.Now().After(deadline) {
				return false, nil
			}
			// Asked for each book, so that switching it off stops a
			// pass within a book's time, and another model starts the
			// next one.
			now, on, err := s.config(ctx)
			if err != nil || !on {
				return true, err
			}
			if now.Model != spec.Model {
				return false, nil
			}
			if e == nil {
				if e, err = s.open(ctx, spec); err != nil {
					return false, err
				}
			}
			if err := s.embedBook(ctx, e, model, version, r); err != nil {
				return false, fmt.Errorf("embed book %s: %w", r.BookID, err)
			}
			done++
		}
	}
}

// embedBook makes the book's stale vectors and stores them together.
func (s *Service) embedBook(ctx context.Context, e embed.Embedder, model, version string, r sqlc.ListBooksToEmbedRow) error {
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
		vecs, err := e.Embed(ctx, []string{embed.Prefix + text})
		if err != nil {
			return err
		}
		// The hash of the text embedded, which is what the query hashes:
		// a change in between is found on the next pass.
		sum := sha256.Sum256([]byte(text))
		out = append(out, made{KindMetadata, sum[:], vecs[0]})
	}
	if r.ContentStale && r.TextFileID != nil {
		vec, err := s.contentVector(ctx, e, *r.TextFileID)
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
				BookID: r.BookID, Kind: m.kind, Model: model, ModelVersion: version,
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
func (s *Service) contentVector(ctx context.Context, e embed.Embedder, fileID uuid.UUID) ([]float32, error) {
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
			texts = append(texts, embed.Prefix+b)
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
