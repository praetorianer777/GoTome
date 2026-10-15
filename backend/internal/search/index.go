package search

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Index looks after the full-text index in the app: it has it built again
// from the files when asked. A new engine format is noticed at start by
// fulltext.OpenService.
type Index struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	// engine is the engine's version, as its library says it.
	engine string
	Queue  *jobs.Runner
}

// NewIndex returns the Index on the pool; Queue is set once the runner
// exists.
func NewIndex(pool *pgxpool.Pool, log *slog.Logger, engine string) *Index {
	return &Index{pool: pool, log: log, engine: engine}
}

// RequestRebuild queues the rebuild of the index, unless one is waiting
// or running.
func (x *Index) RequestRebuild(ctx context.Context) (bool, error) {
	got, err := x.Queue.Insert(ctx, RebuildIndexArgs{}, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: rebuildAttempts,
	})
	return err == nil && !got.Duplicate, err
}

// Rebuild marks every file as one the index is still to get and queues
// ingest.index_files, which reads them again and hands their chunks over
// under the IDs they have: each replaces itself in the index, and searches
// find what is there meanwhile.
func (x *Index) Rebuild(ctx context.Context) error {
	if err := sqlc.New(x.pool).ClearAllIndexed(ctx); err != nil {
		return err
	}
	x.log.Info("building the full-text index again from the files")
	_, err := x.Queue.Insert(ctx, ingest.IndexFilesArgs{}, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: rebuildAttempts,
	})
	return err
}

// IndexStatus is how much of the visible books' text search knows.
type IndexStatus struct {
	// Files is how many books have a file whose text is read; Indexed how
	// many of those the full-text index has been given.
	Files   int `json:"files"`
	Indexed int `json:"indexed"`
	// Rebuilding says files are being read again for the index; until each
	// is, searches find what the index has of it.
	Rebuilding bool `json:"rebuilding"`
	// Engine is the full-text engine's version.
	Engine string `json:"engine"`
}

// Status counts the visible books' text files, of a library when named.
func (x *Index) Status(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (IndexStatus, error) {
	out := IndexStatus{Engine: x.engine}
	err := db.InTx(ctx, x.pool, func(tx pgx.Tx) error {
		counts, err := sqlc.New(tx).CountChunked(ctx, sqlc.CountChunkedParams{
			Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
		})
		if err != nil {
			return err
		}
		out.Files, out.Indexed = int(counts.Files), int(counts.InApp)
		return tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM river_job WHERE kind = ANY($1)
			               AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled'))`,
			[]string{rebuildKind, ingest.IndexFilesArgs{}.Kind()}).Scan(&out.Rebuilding)
	})
	return out, err
}

// rebuildAttempts is how often a failed rebuild is tried before it is left
// on the jobs page.
const rebuildAttempts = 5

// RebuildIndexArgs is the job that builds the search index again.
type RebuildIndexArgs struct{}

// rebuildKind names the job in the queue. It is stored with every job, so
// it stays.
const rebuildKind = "search.rebuild_index"

func (RebuildIndexArgs) Kind() string { return rebuildKind }

// RebuildIndexWorker runs RebuildIndexArgs.
type RebuildIndexWorker struct {
	river.WorkerDefaults[RebuildIndexArgs]
	Index *Index
}

func (w *RebuildIndexWorker) Work(ctx context.Context, _ *river.Job[RebuildIndexArgs]) error {
	return w.Index.Rebuild(ctx)
}
