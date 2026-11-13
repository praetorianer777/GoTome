// Package passages keeps a vector of every passage of a book's text, for
// finding books by a description of what happens in them
// (docs/decisions/passage-vectors.md, #174). It is off unless the setting
// embedding.passages is on. The vectors are files of a Store under the
// data directory, derived like the full-text index; passage_files says
// which files they are of, and whatever the store finds is checked
// against the chunks, which may be ahead of it.
package passages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// centreSample is how many passages the store's centre is the mean of:
// the first ones embedded, held back until there are this many or no
// more.
const centreSample = 2000

// batch is how many passages go to the model at once, as in #15.
const batch = 8

// page is how many files one look at the catalogue takes.
const page = 200

// compactAfter is how often the store is written again without the
// passages of chunks gone and under the libraries books are in now, at
// the latest; sooner once a fifth of it is gone.
const compactAfter = 24 * time.Hour

// Search keeps the best rescoreMin passages by their signs, or a fiftieth
// of the store if more, and reads those passages' int8 vectors; of those
// the best keep are checked against the database. At 3.19 million
// passages the signs' best 20,000 hold 92 % of the ten best books, and a
// fiftieth more (docs/decisions/passage-vectors.md).
const (
	rescoreMin = 10000
	keep       = 1000
)

// Service embeds passages and searches them.
type Service struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	config similar.Config
	on     func(ctx context.Context) (bool, error)
	open   similar.Opener
	store  *Store
	Queue  similar.Queue
	// Chunks reads every chunk of a file from it (ingest.Service.FileChunks).
	Chunks func(ctx context.Context, fileID uuid.UUID) (map[int64]string, error)
}

// NewService returns the Service over the store; on says whether passages
// are embedded and searched. Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, store *Store, config similar.Config, on func(ctx context.Context) (bool, error), open similar.Opener, log *slog.Logger) *Service {
	return &Service{pool: pool, store: store, config: config, on: on, open: open, log: log}
}

// Store is the service's store.
func (s *Service) Store() *Store { return s.store }

// enabled is the model and whether passages are embedded with it now:
// embedding and passages both on.
func (s *Service) enabled(ctx context.Context) (embed.Spec, bool, error) {
	spec, on, err := s.config(ctx)
	if err != nil || !on {
		return spec, false, err
	}
	passages, err := s.on(ctx)
	return spec, passages, err
}

// pending are files embedded before the store has its centre.
type pending struct {
	file    sqlc.ListPassageFilesToEmbedRow
	records []Record
}

// Embed works through the primary text files whose passages have no
// vectors of the chosen model, in ID order, until none is left, the
// deadline has passed, or passages or embedding are switched off. It
// reports whether it got through all of them; switched off counts as
// through.
func (s *Service) Embed(ctx context.Context, deadline time.Time) (complete bool, err error) {
	spec, on, err := s.enabled(ctx)
	if err != nil || !on {
		return true, err
	}
	q := sqlc.New(s.pool)
	model, version := spec.Model.Name, similar.Version(spec.Model)
	if m := s.store.Meta(); m == nil || m.Model != model || m.Version != version {
		// Whatever was recorded is not in the store: every file is
		// embedded again, and the store begun with the first of them.
		if err := q.DeletePassageFiles(ctx); err != nil {
			return false, err
		}
	}
	var e embed.Embedder
	defer func() {
		if e != nil {
			e.Close()
		}
	}()
	var held []pending
	heldPassages := 0
	// begin starts the store around the mean of what was held back, and
	// stores it.
	begin := func() error {
		var vecs [][]float32
		for _, p := range held {
			for _, r := range p.records {
				vecs = append(vecs, r.Vector)
			}
		}
		if len(vecs) == 0 {
			return nil
		}
		if err := s.store.Begin(model, version, centre(vecs, spec.Model.Dim)); err != nil {
			return err
		}
		for _, p := range held {
			if err := s.put(ctx, spec, p.file, p.records); err != nil {
				return err
			}
		}
		held = nil
		return nil
	}
	begun := func() bool {
		m := s.store.Meta()
		return m != nil && m.Model == model && m.Version == version
	}
	after := uuid.Nil
	for {
		rows, err := q.ListPassageFilesToEmbed(ctx, sqlc.ListPassageFilesToEmbedParams{
			After: after, Model: model, ModelVersion: version, Page: page,
		})
		if err != nil {
			return false, errors.Join(err, begin())
		}
		if len(rows) == 0 {
			if err := begin(); err != nil {
				return false, err
			}
			return true, s.compactIfDue(ctx, model, version)
		}
		for _, r := range rows {
			after = r.FileID
			if time.Now().After(deadline) {
				return false, begin()
			}
			// Asked for each file, so that switching off stops a pass
			// within a file's time, and another model starts the next.
			now, on, err := s.enabled(ctx)
			if err != nil || !on {
				return true, errors.Join(err, begin())
			}
			if now.Model != spec.Model {
				return false, begin()
			}
			if e == nil {
				if e, err = s.open(ctx, spec); err != nil {
					return false, errors.Join(err, begin())
				}
			}
			records, err := s.embedFile(ctx, e, spec, r)
			if err != nil {
				if errors.Is(err, embed.ErrServer) {
					err = fmt.Errorf("%w: %v", similar.ErrUnavailable, err)
				}
				return false, errors.Join(fmt.Errorf("embed the passages of file %s: %w", r.FileID, err), begin())
			}
			if begun() {
				if err := s.put(ctx, spec, r, records); err != nil {
					return false, err
				}
				continue
			}
			held = append(held, pending{r, records})
			heldPassages += len(records)
			if heldPassages >= centreSample {
				if err := begin(); err != nil {
					return false, err
				}
			}
		}
	}
}

// embedFile embeds every chunk of the file that can be read.
func (s *Service) embedFile(ctx context.Context, e embed.Embedder, spec embed.Spec, r sqlc.ListPassageFilesToEmbedRow) ([]Record, error) {
	if s.Chunks == nil {
		return nil, errors.New("embedding passages needs the chunks read")
	}
	texts, err := s.Chunks(ctx, r.FileID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(texts))
	for id := range texts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	records := make([]Record, 0, len(ids))
	for start := 0; start < len(ids); start += batch {
		part := ids[start:min(start+batch, len(ids))]
		in := make([]string, len(part))
		for i, id := range part {
			in[i] = spec.PassageText(texts[id])
		}
		vecs, err := e.Embed(ctx, in)
		if err != nil {
			return nil, err
		}
		for i, id := range part {
			records = append(records, Record{Chunk: id, Library: r.LibraryID, Vector: vecs[i]})
		}
	}
	return records, nil
}

// put stores a file's passages, then records that it has them.
func (s *Service) put(ctx context.Context, spec embed.Spec, r sqlc.ListPassageFilesToEmbedRow, records []Record) error {
	if len(records) > 0 {
		if err := s.store.Append(records); err != nil {
			return err
		}
	}
	return sqlc.New(s.pool).PutPassageFile(ctx, sqlc.PutPassageFileParams{
		FileID: r.FileID, Model: spec.Model.Name, ModelVersion: similar.Version(spec.Model),
		ChunkedAt: r.ChunkedAt, Passages: int32(len(records)),
	})
}

// centre is the mean of the vectors.
func centre(vecs [][]float32, dim int) []float32 {
	out := make([]float32, dim)
	for _, v := range vecs {
		for i, x := range v {
			out[i] += x / float32(len(vecs))
		}
	}
	return out
}

// compactIfDue writes the store again once a fifth of it is of chunks
// gone, or once a day.
func (s *Service) compactIfDue(ctx context.Context, model, version string) error {
	m := s.store.Meta()
	if m == nil {
		return nil
	}
	q := sqlc.New(s.pool)
	want, err := q.CountStoredPassages(ctx, sqlc.CountStoredPassagesParams{Model: model, ModelVersion: version})
	if err != nil {
		return err
	}
	if n := int64(s.store.Len()); n*4 <= want*5 && time.Since(m.CompactedAt) < compactAfter {
		return nil
	}
	rows, err := q.ListLivePassages(ctx, sqlc.ListLivePassagesParams{Model: model, ModelVersion: version})
	if err != nil {
		return err
	}
	before := s.store.Len()
	err = s.store.Compact(func(chunk int64) (uuid.UUID, bool) {
		i := sort.Search(len(rows), func(i int) bool { return rows[i].ID >= chunk })
		if i < len(rows) && rows[i].ID == chunk {
			return rows[i].LibraryID, true
		}
		return uuid.Nil, false
	})
	if err != nil {
		return err
	}
	s.log.Info("passage store compacted", "before", before, "after", s.store.Len())
	return nil
}

// Near returns the books the scope sees, of one library when named, whose
// passages are nearest the query vector, each with its best passage's
// cosine: none while passages are off or the store holds another model's.
func (s *Service) Near(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, spec embed.Spec, query []float32) (map[uuid.UUID]float64, error) {
	on, err := s.on(ctx)
	if err != nil || !on {
		return nil, err
	}
	q := sqlc.New(s.pool)
	visible, err := q.ListPassageLibraries(ctx, sqlc.ListPassageLibrariesParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil || len(visible) == 0 {
		return nil, err
	}
	model, version := spec.Model.Name, similar.Version(spec.Model)
	hits, err := s.store.Search(query, model, version, visible, max(rescoreMin, s.store.Len()/50), keep)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	ids := make([]int64, len(hits))
	score := make(map[int64]float64, len(hits))
	for i, h := range hits {
		ids[i] = h.Chunk
		score[h.Chunk] = h.Score
	}
	rows, err := q.ListPassageBooks(ctx, sqlc.ListPassageBooksParams{
		Ids: ids, Model: model, ModelVersion: version,
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]float64{}
	for _, r := range rows {
		if old, ok := out[r.BookID]; !ok || score[r.ChunkID] > old {
			out[r.BookID] = score[r.ChunkID]
		}
	}
	return out, nil
}

// Status counts the books the scope sees, of one library when named, with
// text, and those whose text has its passages' vectors of the chosen
// model; nil while passages are off.
func (s *Service) Status(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (*similar.PassageStatus, error) {
	spec, on, err := s.enabled(ctx)
	if err != nil || !on {
		return nil, err
	}
	c, err := sqlc.New(s.pool).CountPassages(ctx, sqlc.CountPassagesParams{
		Model: spec.Model.Name, ModelVersion: similar.Version(spec.Model),
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil {
		return nil, err
	}
	return &similar.PassageStatus{WithText: int(c.WithText), WithPassages: int(c.WithPassages), Bytes: s.store.Size()}, nil
}

// Bytes is what the store takes on disk while it holds the model's
// passages, else 0.
func (s *Service) Bytes(m embed.Model) int64 {
	if !s.holds(m) {
		return 0
	}
	return s.store.Size()
}

func (s *Service) holds(m embed.Model) bool {
	meta := s.store.Meta()
	return meta != nil && meta.Model == m.Name && meta.Version == similar.Version(m)
}

// Remove takes the model's passages away, the store while it holds them
// and the record of which files have them, and returns the bytes freed.
func (s *Service) Remove(ctx context.Context, m embed.Model) (int64, error) {
	var freed int64
	if s.holds(m) {
		freed = s.store.Size()
		if err := s.store.Clear(); err != nil {
			return 0, err
		}
	}
	return freed, sqlc.New(s.pool).DeletePassageFilesOfModel(ctx, sqlc.DeletePassageFilesOfModelParams{
		Model: m.Name, ModelVersion: similar.Version(m),
	})
}

// passBudget is how long one run embeds before it snoozes and the next run
// goes on.
const passBudget = 5 * time.Minute

// EmbedArgs is the job that embeds the passages of every file whose
// passages have no vectors yet.
type EmbedArgs struct{}

// embedKind names the job in the queue. It is stored with every job, so it
// stays.
const embedKind = "passages.embed"

func (EmbedArgs) Kind() string { return embedKind }

// On the embed queue, whose one worker runs it after or before the book
// vectors' pass, never beside it.
var embedOpts = jobs.InsertOpts{Queue: jobs.QueueEmbed, Unique: true, MaxAttempts: 5}

// Enqueue asks for a pass; one already waiting or running is enough.
func (s *Service) Enqueue(ctx context.Context) error {
	_, err := s.Queue.Insert(ctx, EmbedArgs{}, embedOpts)
	return err
}

// EnqueueTx asks for a pass once the transaction commits.
func (s *Service) EnqueueTx(ctx context.Context, tx pgx.Tx) error {
	_, err := s.Queue.InsertTx(ctx, tx, EmbedArgs{}, embedOpts)
	return err
}

// Periodic is the scheduled pass, also at start.
func Periodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, EmbedArgs{}, jobs.QueueEmbed)
}

// EmbedWorker runs EmbedArgs.
type EmbedWorker struct {
	river.WorkerDefaults[EmbedArgs]
	Service *Service
}

func (w *EmbedWorker) Timeout(*river.Job[EmbedArgs]) time.Duration { return passBudget + time.Minute }

func (w *EmbedWorker) Work(ctx context.Context, _ *river.Job[EmbedArgs]) error {
	complete, err := w.Service.Embed(ctx, time.Now().Add(passBudget))
	return similar.PassResult(w.Service.log, "passages are not embedded", complete, err)
}
