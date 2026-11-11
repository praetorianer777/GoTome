package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// The runs of the settings page: a measure is a match-check, a learning a
// match-train.
const (
	runMeasure = "measure"
	runLearn   = "learn"
)

// How many books each run looks up unless it is asked for another number.
const (
	DefaultMeasureBooks = 200
	DefaultLearnBooks   = 1000
	// MaxRunBooks bounds what a request may ask: every book costs the
	// providers a few requests.
	MaxRunBooks = 5000
	// relearnAfter is how long a learned score stands before the weekly
	// learning makes it again.
	relearnAfter = 7 * 24 * time.Hour
	// runTimeout is longer than any run: a thousand books take hours at
	// the providers' pace.
	runTimeout = 24 * time.Hour
)

// ErrRunBusy is a run asked for while one of its kind waits or runs.
var ErrRunBusy = errors.New("this is already running")

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	Insert(ctx context.Context, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// RunArgs is the job that runs a measure or a learning.
type RunArgs struct {
	Run   string `json:"run"`
	Books int    `json:"books"`
}

// runKind names the job in the queue. It is stored with every job, so it
// stays.
const runKind = "metadata.match_run"

func (RunArgs) Kind() string { return runKind }

var runOpts = jobs.InsertOpts{Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: 3}

// AutoLearnArgs is the scheduled check whether a week has passed since the
// score was learned.
type AutoLearnArgs struct{}

const autoLearnKind = "metadata.auto_learn"

func (AutoLearnArgs) Kind() string { return autoLearnKind }

// AutoLearnPeriodic checks every so often; the learning itself is weekly.
func AutoLearnPeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, false, AutoLearnArgs{}, jobs.QueueMetadata)
}

// Request queues a measure or a learning of that many books, and says
// ErrRunBusy if one is waiting or running.
func (s *Service) Request(ctx context.Context, kind string, books int) error {
	if s.Queue == nil {
		return errors.New("no job queue")
	}
	if books <= 0 {
		books = DefaultMeasureBooks
		if kind == runLearn {
			books = DefaultLearnBooks
		}
	}
	books = min(books, MaxRunBooks)
	q := sqlc.New(s.pool)
	taken, err := q.QueueMatchRun(ctx, sqlc.QueueMatchRunParams{Kind: kind, Requested: int32(books)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRunBusy
	}
	if err != nil || taken == "" {
		return err
	}
	if _, err := s.Queue.Insert(ctx, RunArgs{Run: kind, Books: books}, runOpts); err != nil {
		_ = q.FinishMatchRun(ctx, sqlc.FinishMatchRunParams{Kind: kind, State: "failed", Error: "the job could not be queued"})
		return err
	}
	return nil
}

// RequestMeasure and RequestLearn are Request for each kind.
func (s *Service) RequestMeasure(ctx context.Context, books int) error {
	return s.Request(ctx, runMeasure, books)
}

func (s *Service) RequestLearn(ctx context.Context, books int) error {
	return s.Request(ctx, runLearn, books)
}

// threshold is metadata.matchThreshold as it stands.
func (s *Service) threshold(ctx context.Context) float64 {
	if s.matchThreshold == nil {
		return 0
	}
	return s.matchThreshold(ctx)
}

// Run does a requested measure or learning, recording its progress and its
// report. A run that fails for a reason of its own is recorded and not
// retried; one cut short by a shutdown is, and goes on through the cache.
func (s *Service) Run(ctx context.Context, kind string, books int) error {
	q := sqlc.New(s.pool)
	start := func(n int, estimate time.Duration) {
		_ = q.StartMatchRun(ctx, sqlc.StartMatchRunParams{Kind: kind, Total: int32(n), EstimateSeconds: int32(estimate.Seconds())})
	}
	progress := func(done, _ int) {
		_ = q.ProgressMatchRun(ctx, sqlc.ProgressMatchRunParams{Kind: kind, Done: int32(done)})
	}
	var report any
	var err error
	switch kind {
	case runLearn:
		report, err = s.Train(ctx, TrainOptions{Books: books, Threshold: s.threshold(ctx), Start: start, Progress: progress})
	default:
		report, err = s.Check(ctx, CheckOptions{Books: books, Threshold: s.threshold(ctx), Start: start, Progress: progress})
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	finish := sqlc.FinishMatchRunParams{Kind: kind, State: "done"}
	if err != nil {
		finish.State, finish.Error, report = "failed", err.Error(), nil
	}
	if report != nil {
		finish.Report, _ = json.Marshal(report)
	}
	return q.FinishMatchRun(context.WithoutCancel(ctx), finish)
}

// LearnIfDue asks for a learning when the setting is on, enough books have
// an ISBN, and the score was learned more than a week ago.
func (s *Service) LearnIfDue(ctx context.Context) error {
	if s.learnScore == nil || !s.learnScore(ctx) {
		return nil
	}
	status, err := s.Scoring(ctx)
	if err != nil {
		return err
	}
	if status.BooksWithISBN < LearnFrom {
		return nil
	}
	if status.Learned != nil && time.Since(status.Learned.LearnedAt) < relearnAfter {
		return nil
	}
	if status.Learn != nil && status.Learn.FinishedAt != nil && time.Since(*status.Learn.FinishedAt) < relearnAfter {
		return nil
	}
	err = s.RequestLearn(ctx, DefaultLearnBooks)
	if errors.Is(err, ErrRunBusy) {
		return nil
	}
	return err
}

// RunWorker runs RunArgs.
type RunWorker struct {
	river.WorkerDefaults[RunArgs]
	Service *Service
}

func (w *RunWorker) Timeout(*river.Job[RunArgs]) time.Duration { return runTimeout }

func (w *RunWorker) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	return w.Service.Run(ctx, job.Args.Run, job.Args.Books)
}

// AutoLearnWorker runs AutoLearnArgs.
type AutoLearnWorker struct {
	river.WorkerDefaults[AutoLearnArgs]
	Service *Service
}

func (w *AutoLearnWorker) Work(ctx context.Context, _ *river.Job[AutoLearnArgs]) error {
	return w.Service.LearnIfDue(ctx)
}
