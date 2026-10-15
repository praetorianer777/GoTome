package fulltext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

const (
	// Heap is the writer's buffer: books arrive one at a time, and the
	// rebuild commits every outboxPage of them, so a small one does; the
	// owner's server has 5 GB for everything.
	Heap = 64 << 20
	// outboxPage is how many outbox rows, each a book's chunks at most, one
	// commit of the index takes.
	outboxPage = 16
	// formatVersionName is the row of index_versions that holds the format
	// of the index on disk, as the engine's version names it.
	formatVersionName = "fulltext"
)

// Service keeps the full-text index of the chunks: it applies the outbox
// the chunks' writers fill, and opens the index again from scratch when the
// engine's format changes.
type Service struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	index *Index
	// One apply at a time: rows are applied in order, and two runs taking
	// the same rows could interleave a delete and an add of one chunk.
	apply sync.Mutex
	Queue *jobs.Runner
}

// Dir is where the index of the data folder lives.
func Dir(dataDir string) string { return filepath.Join(dataDir, "search", "tantivy") }

// OpenService opens the index in dir as its writer, with the library Load
// loaded. When the index there is new, or of a format other than the one
// recorded, it is made anew and every file is marked for its chunks to be
// handed to it again (ClearAllIndexed), which the caller then queues.
func OpenService(ctx context.Context, pool *pgxpool.Pool, dir, version string, log *slog.Logger) (*Service, bool, error) {
	format := formatOf(version)
	q := sqlc.New(pool)
	recorded, err := q.GetIndexVersion(ctx, formatVersionName)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	_, statErr := os.Stat(filepath.Join(dir, "meta.json"))
	exists := statErr == nil
	fresh := !exists || recorded != format
	if exists && recorded != format {
		log.Info("the full-text index has another format; making it anew", "was", recorded, "now", format)
		if err := os.RemoveAll(dir); err != nil {
			return nil, false, fmt.Errorf("remove the old full-text index: %w", err)
		}
	}
	ix, err := Open(dir, Heap)
	if err != nil {
		return nil, false, err
	}
	if fresh {
		if err := q.ClearAllIndexed(ctx); err != nil {
			ix.Close()
			return nil, false, err
		}
		if err := q.SetIndexVersion(ctx, sqlc.SetIndexVersionParams{Name: formatVersionName, Version: format}); err != nil {
			ix.Close()
			return nil, false, err
		}
	}
	return &Service{pool: pool, log: log, index: ix}, fresh, nil
}

// formatOf is the "format N" part of the engine's version.
func formatOf(version string) string {
	if i := strings.Index(version, "format "); i >= 0 {
		return version[i:]
	}
	return version
}

// Index is the index the service writes.
func (s *Service) Index() *Index { return s.index }

// Close closes the index.
func (s *Service) Close() error { return s.index.Close() }

// Drain applies the outbox to the index, oldest first, a page at a time:
// each page is committed to the index before its rows go, so a crash in
// between applies them again, which adds nothing twice. It stops at the
// deadline and says whether rows are left.
func (s *Service) Drain(ctx context.Context, deadline time.Time) (bool, error) {
	s.apply.Lock()
	defer s.apply.Unlock()
	q := sqlc.New(s.pool)
	for {
		rows, err := q.ListOutbox(ctx, outboxPage)
		if err != nil || len(rows) == 0 {
			return false, err
		}
		for _, r := range rows {
			b := Batch{DeleteChunks: r.Deletes}
			if err := json.Unmarshal(r.Docs, &b.Add); err != nil {
				return false, fmt.Errorf("outbox row %d: %w", r.ID, err)
			}
			for _, d := range b.Add {
				b.DeleteChunks = append(b.DeleteChunks, d.ID)
			}
			if err := s.index.Apply(b); err != nil {
				return false, err
			}
		}
		if err := s.index.Commit(); err != nil {
			return false, err
		}
		if err := q.DeleteOutbox(ctx, rows[len(rows)-1].ID); err != nil {
			return false, err
		}
		if time.Now().After(deadline) {
			return true, nil
		}
	}
}

// AddTx hands chunks to the index in the transaction that wrote them, and
// queues the outbox's job; the index has them shortly after the commit.
func AddTx(ctx context.Context, tx pgx.Tx, queue Inserter, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	data, err := json.Marshal(docs)
	if err != nil {
		return err
	}
	if err := sqlc.New(tx).AddOutboxDocs(ctx, data); err != nil {
		return err
	}
	return EnqueueTx(ctx, tx, queue)
}

// Inserter is the part of the job runner that queues in a transaction.
type Inserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// EnqueueTx queues the outbox's job, unless one waits already.
func EnqueueTx(ctx context.Context, tx pgx.Tx, queue Inserter) error {
	if queue == nil {
		return nil
	}
	_, err := queue.InsertTx(ctx, tx, ApplyArgs{}, applyOpts)
	return err
}

var applyOpts = jobs.InsertOpts{Queue: jobs.QueueIndex, Unique: true, MaxAttempts: 25}

// applyBudget is how long one run of the outbox's job applies before it
// hands over to the next.
const applyBudget = 2 * time.Minute

// ApplyArgs is the job that applies the outbox to the index.
type ApplyArgs struct{}

// applyKind names the job in the queue. It is stored with every job, so it
// stays.
const applyKind = "fulltext.apply_outbox"

func (ApplyArgs) Kind() string { return applyKind }

// Periodic queues the outbox's job every interval: deletes reach the
// outbox through a trigger, which queues nothing.
func Periodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, ApplyArgs{}, jobs.QueueIndex)
}

// ApplyWorker runs ApplyArgs.
type ApplyWorker struct {
	river.WorkerDefaults[ApplyArgs]
	Service *Service
}

func (w *ApplyWorker) Timeout(*river.Job[ApplyArgs]) time.Duration { return applyBudget + time.Minute }

func (w *ApplyWorker) Work(ctx context.Context, _ *river.Job[ApplyArgs]) error {
	more, err := w.Service.Drain(ctx, time.Now().Add(applyBudget))
	if err != nil {
		return err
	}
	if more {
		return river.JobSnooze(time.Second)
	}
	return nil
}
