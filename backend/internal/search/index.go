package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// engineVersionName is the row of index_versions that holds the pg_search
// version book_chunks_bm25 was last built with.
const engineVersionName = "pg_search"

// Index looks after book_chunks_bm25: it notices a new pg_search and builds
// the index again from the chunks.
type Index struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	Queue *jobs.Runner
}

// NewIndex returns the Index on the pool; Queue is set once the runner
// exists.
func NewIndex(pool *pgxpool.Pool, log *slog.Logger) *Index {
	return &Index{pool: pool, log: log}
}

// CheckEngine runs at start. When the image brings a newer pg_search than
// the database has, the extension is updated; when the version differs from
// the one the index was built with, a rebuild is queued, which records the
// version once it is done. A database without a version is taken to have
// been built with the one it has.
func (x *Index) CheckEngine(ctx context.Context) error {
	var installed, available string
	err := x.pool.QueryRow(ctx, `
		SELECT e.extversion, coalesce(a.default_version, e.extversion)
		FROM pg_extension e
		LEFT JOIN pg_available_extensions a ON a.name = e.extname
		WHERE e.extname = 'pg_search'`).Scan(&installed, &available)
	if err != nil {
		return fmt.Errorf("read the pg_search version: %w", err)
	}
	if available != installed {
		x.log.Info("updating pg_search", "from", installed, "to", available)
		if _, err := x.pool.Exec(ctx, "ALTER EXTENSION pg_search UPDATE"); err != nil {
			return fmt.Errorf("update pg_search from %s to %s: %w", installed, available, err)
		}
		installed = available
	}
	q := sqlc.New(x.pool)
	built, err := q.GetIndexVersion(ctx, engineVersionName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return q.SetIndexVersion(ctx, sqlc.SetIndexVersionParams{Name: engineVersionName, Version: installed})
	case err != nil:
		return err
	case built == installed:
		return nil
	}
	x.log.Info("pg_search changed; rebuilding the search index", "built with", built, "now", installed)
	_, err = x.RequestRebuild(ctx)
	return err
}

// RequestRebuild queues the rebuild of the index from the chunks, unless
// one is waiting or running.
func (x *Index) RequestRebuild(ctx context.Context) (bool, error) {
	got, err := x.Queue.Insert(ctx, RebuildIndexArgs{}, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: rebuildAttempts,
	})
	return err == nil && !got.Duplicate, err
}

// Rebuild builds book_chunks_bm25 again from the chunks, beside the old
// one, which searches use until the new one takes its place, and records
// the pg_search version it was built with.
func (x *Index) Rebuild(ctx context.Context) error {
	// A rebuild that was stopped leaves its half-built copy behind, which
	// the next one would trip over.
	if _, err := x.pool.Exec(ctx, "DROP INDEX IF EXISTS book_chunks_bm25_ccnew"); err != nil {
		return err
	}
	start := time.Now()
	if _, err := x.pool.Exec(ctx, "REINDEX INDEX CONCURRENTLY book_chunks_bm25"); err != nil {
		return fmt.Errorf("rebuild the search index: %w", err)
	}
	var version string
	if err := x.pool.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname = 'pg_search'").Scan(&version); err != nil {
		return err
	}
	x.log.Info("rebuilt the search index", "pg_search", version, "took", time.Since(start).Round(time.Second))
	return sqlc.New(x.pool).SetIndexVersion(ctx, sqlc.SetIndexVersionParams{Name: engineVersionName, Version: version})
}

// IndexStatus is how much of the visible books' text search knows.
type IndexStatus struct {
	// Files is how many books have a file whose text is read; Indexed how
	// many of those files are cut into chunks, which search finds.
	Files   int `json:"files"`
	Indexed int `json:"indexed"`
	// Rebuilding says the index is being built again; until it is done,
	// searches use the old one.
	Rebuilding bool `json:"rebuilding"`
	// Engine is the pg_search version.
	Engine string `json:"engine"`
}

// Status counts the visible books' text files, of a library when named.
func (x *Index) Status(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (IndexStatus, error) {
	var out IndexStatus
	err := db.InTx(ctx, x.pool, func(tx pgx.Tx) error {
		counts, err := sqlc.New(tx).CountChunked(ctx, sqlc.CountChunkedParams{
			Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
		})
		if err != nil {
			return err
		}
		out.Files, out.Indexed = int(counts.Files), int(counts.Chunked)
		return tx.QueryRow(ctx, `
			SELECT (SELECT extversion FROM pg_extension WHERE extname = 'pg_search'),
			       EXISTS (SELECT 1 FROM river_job WHERE kind = $1
			               AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled'))`,
			rebuildKind).Scan(&out.Engine, &out.Rebuilding)
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

// Timeout is none: building the index of a large library takes as long as
// it takes, and a rebuild cut short starts over.
func (w *RebuildIndexWorker) Timeout(*river.Job[RebuildIndexArgs]) time.Duration { return -1 }

func (w *RebuildIndexWorker) Work(ctx context.Context, _ *river.Job[RebuildIndexArgs]) error {
	return w.Index.Rebuild(ctx)
}
