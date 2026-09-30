// Package jobs runs background work: scans, extraction, metadata matching,
// embedding, notifications. The queue is River, which keeps its jobs in the
// same Postgres as everything else, so there is no further service to run and
// a job can be enqueued in the transaction that made it necessary.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

// The queues. Each has its own number of workers, so a long embedding run
// cannot hold up a scan, and sending a notification never waits behind either.
const (
	// QueueDefault is for housekeeping that belongs to no feature.
	QueueDefault  = river.QueueDefault
	QueueScan     = "scan"
	QueueExtract  = "extract"
	QueueMetadata = "metadata"
	QueueEmbed    = "embed"
	QueueNotify   = "notify"
)

// queues is how many jobs of each queue run at once. The numbers are for
// home hardware: a scan walks a disk and embedding fills every core, so one
// of each; extraction and metadata lookups mostly wait on files and networks.
var queues = map[string]river.QueueConfig{
	QueueDefault:  {MaxWorkers: 2},
	QueueScan:     {MaxWorkers: 1},
	QueueExtract:  {MaxWorkers: 2},
	QueueMetadata: {MaxWorkers: 2},
	QueueEmbed:    {MaxWorkers: 1},
	QueueNotify:   {MaxWorkers: 4},
}

// Queues lists the names of the queues.
func Queues() []string {
	return []string{QueueDefault, QueueScan, QueueExtract, QueueMetadata, QueueEmbed, QueueNotify}
}

// shutdownGrace is how long Stop lets running jobs finish before it cancels
// them. A cancelled job is not lost: it is picked up again on the next start.
const shutdownGrace = 20 * time.Second

// Workers is the set of job handlers. A feature registers its worker with
// river.AddWorker before the Runner is built.
type Workers = river.Workers

// NewWorkers returns an empty set of workers.
func NewWorkers() *Workers { return river.NewWorkers() }

// Config is what a Runner is built from.
type Config struct {
	Logger  *slog.Logger
	Workers *Workers
	// Periodic jobs are enqueued on a schedule for as long as the Runner runs.
	Periodic []*river.PeriodicJob
	// RetryPolicy replaces River's exponential backoff. Tests use it to retry
	// at once; nothing else should.
	RetryPolicy river.ClientRetryPolicy
	// InsertOnly builds a Runner that enqueues and lists jobs but works none,
	// for a process that must not start background work.
	InsertOnly bool
}

// Runner enqueues jobs and works them.
type Runner struct {
	client *river.Client[pgx.Tx]
	log    *slog.Logger
	works  bool
}

// Migrate brings River's own tables up to date. They live beside the
// application's, in the same database.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("job queue migrations: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate the job queue: %w", err)
	}
	return nil
}

// SchemaVersion is the newest version of River's tables this build knows,
// which is what Migrate brings a database to.
func SchemaVersion() int {
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		return 0
	}
	newest := 0
	for _, m := range migrator.AllVersions() {
		newest = max(newest, m.Version)
	}
	return newest
}

// New builds a Runner. It does not start working jobs; Start does.
func New(pool *pgxpool.Pool, cfg Config) (*Runner, error) {
	riverCfg := &river.Config{
		Logger:       slog.New(quiet{cfg.Logger.Handler()}),
		RetryPolicy:  cfg.RetryPolicy,
		Workers:      cfg.Workers,
		PeriodicJobs: cfg.Periodic,
	}
	if !cfg.InsertOnly {
		riverCfg.Queues = queues
	}
	client, err := river.NewClient(riverpgxv5.New(pool), riverCfg)
	if err != nil {
		return nil, fmt.Errorf("job queue: %w", err)
	}
	return &Runner{client: client, log: cfg.Logger, works: !cfg.InsertOnly}, nil
}

// Start begins working jobs. Jobs left unfinished by an earlier process are
// picked up again.
func (r *Runner) Start(ctx context.Context) error {
	if !r.works {
		return nil
	}
	// The client's lifetime is its own: it is stopped through Stop, which
	// lets running jobs finish, not by the caller's context ending.
	if err := r.client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start the job queue: %w", err)
	}
	return nil
}

// Stop takes no new jobs and waits for the running ones to finish. Those that
// do not finish in time are cancelled; River runs them again after the next
// start, which is why a job must be safe to run twice.
func (r *Runner) Stop(ctx context.Context) error {
	if !r.works {
		return nil
	}
	soft, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	err := r.client.Stop(soft)
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return err
	}
	r.log.Warn("jobs still running after the grace period are cancelled and will run again")
	return r.client.StopAndCancel(ctx)
}

// InsertOpts says how a job is enqueued.
type InsertOpts struct {
	// Queue is one of the Queue constants; empty is QueueDefault.
	Queue string
	// Unique skips the insert when a job of the same kind with the same
	// arguments is already waiting or running. Asking twice for a library to
	// be scanned is one scan.
	Unique bool
	// ScheduledAt delays the job; the zero value runs it as soon as a worker
	// is free.
	ScheduledAt time.Time
	// MaxAttempts overrides River's default of 25.
	MaxAttempts int
}

func (o InsertOpts) river() *river.InsertOpts {
	opts := &river.InsertOpts{Queue: o.Queue, ScheduledAt: o.ScheduledAt, MaxAttempts: o.MaxAttempts}
	if o.Unique {
		opts.UniqueOpts = river.UniqueOpts{
			ByArgs: true,
			// A finished job does not stand in the way of doing it again.
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
				rivertype.JobStateRunning, rivertype.JobStateScheduled,
			},
		}
	}
	return opts
}

// Inserted says what an insert did.
type Inserted struct {
	ID int64
	// Duplicate is true when a unique insert found the job already there; ID
	// is then that job's.
	Duplicate bool
}

// Insert enqueues a job.
func (r *Runner) Insert(ctx context.Context, args river.JobArgs, opts InsertOpts) (Inserted, error) {
	res, err := r.client.Insert(ctx, args, opts.river())
	if err != nil {
		return Inserted{}, err
	}
	return Inserted{ID: res.Job.ID, Duplicate: res.UniqueSkippedAsDuplicate}, nil
}

// InsertTx enqueues a job as part of a transaction: it becomes visible to the
// workers when the transaction commits and never exists if it rolls back.
func (r *Runner) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts InsertOpts) (Inserted, error) {
	res, err := r.client.InsertTx(ctx, tx, args, opts.river())
	if err != nil {
		return Inserted{}, err
	}
	return Inserted{ID: res.Job.ID, Duplicate: res.UniqueSkippedAsDuplicate}, nil
}

// Job is a job as a status page shows it.
type Job struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"maxAttempts"`
	CreatedAt   time.Time  `json:"createdAt"`
	ScheduledAt time.Time  `json:"scheduledAt"`
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
	FinalizedAt *time.Time `json:"finalizedAt,omitempty"`
	// LastError is what the latest failed attempt returned.
	LastError string `json:"lastError,omitempty"`
}

func fromRow(row *rivertype.JobRow) Job {
	job := Job{
		ID: row.ID, Kind: row.Kind, Queue: row.Queue, State: string(row.State),
		Attempt: row.Attempt, MaxAttempts: row.MaxAttempts,
		CreatedAt: row.CreatedAt, ScheduledAt: row.ScheduledAt,
		AttemptedAt: row.AttemptedAt, FinalizedAt: row.FinalizedAt,
	}
	if n := len(row.Errors); n > 0 {
		job.LastError = row.Errors[n-1].Error
	}
	return job
}

// Get returns one job.
func (r *Runner) Get(ctx context.Context, id int64) (Job, error) {
	row, err := r.client.JobGet(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return fromRow(row), nil
}

// List returns the newest jobs, optionally of some queues only.
func (r *Runner) List(ctx context.Context, limit int, queues ...string) ([]Job, error) {
	params := river.NewJobListParams().OrderBy(river.JobListOrderByID, river.SortOrderDesc).First(limit)
	if len(queues) > 0 {
		params = params.Queues(queues...)
	}
	res, err := r.client.JobList(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(res.Jobs))
	for i, row := range res.Jobs {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Every returns a periodic job that enqueues args at the given interval, and
// once at start when runOnStart is set. It is inserted as unique, so a run
// that is still waiting or working is not joined by a second.
func Every(interval time.Duration, runOnStart bool, args river.JobArgs, queue string) *river.PeriodicJob {
	opts := InsertOpts{Queue: queue, Unique: true}.river()
	return river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) { return args, opts },
		&river.PeriodicJobOpts{RunOnStart: runOnStart},
	)
}

// quiet passes on warnings and errors only. River reports its job counts
// every few seconds at info, which would be most of the application's log.
type quiet struct{ slog.Handler }

func (q quiet) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn && q.Handler.Enabled(ctx, level)
}

func (q quiet) WithAttrs(attrs []slog.Attr) slog.Handler { return quiet{q.Handler.WithAttrs(attrs)} }

func (q quiet) WithGroup(name string) slog.Handler { return quiet{q.Handler.WithGroup(name)} }
