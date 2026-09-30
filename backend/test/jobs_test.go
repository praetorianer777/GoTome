//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// noteArgs is a job for the tests: it records that it ran, and can be told to
// fail its first attempts or to take a while.
type noteArgs struct {
	Note      string `json:"note"`
	FailFirst int    `json:"failFirst"`
	TakeMS    int    `json:"takeMs"`
}

func (noteArgs) Kind() string { return "test.note" }

type noteWorker struct {
	river.WorkerDefaults[noteArgs]
	mu       sync.Mutex
	ran      []string
	attempts atomic.Int64
	done     chan string
}

func newNoteWorker() *noteWorker { return &noteWorker{done: make(chan string, 100)} }

func (w *noteWorker) Work(ctx context.Context, job *river.Job[noteArgs]) error {
	w.attempts.Add(1)
	if job.Attempt <= job.Args.FailFirst {
		return errors.New("not yet")
	}
	if job.Args.TakeMS > 0 {
		select {
		case <-time.After(time.Duration(job.Args.TakeMS) * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.mu.Lock()
	w.ran = append(w.ran, job.Args.Note)
	w.mu.Unlock()
	w.done <- job.Args.Note
	return nil
}

func (w *noteWorker) notes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.ran...)
}

// wait returns the next note a job finished with, or fails the test.
func (w *noteWorker) wait(t *testing.T) string {
	t.Helper()
	select {
	case note := <-w.done:
		return note
	case <-time.After(15 * time.Second):
		t.Fatal("no job finished in time")
		return ""
	}
}

// quiet fails the test if a job finishes within the wait.
func (w *noteWorker) quiet(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case note := <-w.done:
		t.Fatalf("job %q ran, and should not have", note)
	case <-time.After(wait):
	}
}

// retryNow retries a failed job at once, so the tests do not wait out River's
// backoff.
type retryNow struct{}

func (retryNow) NextRetry(*rivertype.JobRow) time.Time { return time.Now() }

func startRunner(t *testing.T, pool *pgxpool.Pool, w *noteWorker, cfg jobs.Config) *jobs.Runner {
	t.Helper()
	workers := jobs.NewWorkers()
	river.AddWorker(workers, w)
	cfg.Logger = slog.New(slog.DiscardHandler)
	cfg.Workers = workers
	runner, err := jobs.New(pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Stop(context.Background()) })
	return runner
}

func TestJobRunsOnlyIfItsTransactionCommits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)
	w := newNoteWorker()
	runner := startRunner(t, pool, w, jobs.Config{})

	boom := errors.New("changed my mind")
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := runner.InsertTx(ctx, tx, noteArgs{Note: "rolled back"}, jobs.InsertOpts{}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	w.quiet(t, 1500*time.Millisecond)

	var held pgx.Tx
	held, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.InsertTx(ctx, held, noteArgs{Note: "committed"}, jobs.InsertOpts{}); err != nil {
		t.Fatal(err)
	}
	// Enqueued, but the transaction is still open: no worker may see it.
	w.quiet(t, 1500*time.Millisecond)
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.wait(t); got != "committed" {
		t.Errorf("the job that ran is %q", got)
	}
}

func TestJobsSurviveARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)

	// A process that only enqueues: the job waits in the database.
	first := newNoteWorker()
	workers := jobs.NewWorkers()
	river.AddWorker(workers, first)
	enqueuer, err := jobs.New(pool, jobs.Config{Logger: slog.New(slog.DiscardHandler), Workers: workers, InsertOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := enqueuer.Insert(ctx, noteArgs{Note: "left behind"}, jobs.InsertOpts{Queue: jobs.QueueScan})
	if err != nil {
		t.Fatal(err)
	}
	if job, err := enqueuer.Get(ctx, inserted.ID); err != nil || job.State != "available" || job.Queue != jobs.QueueScan {
		t.Fatalf("the waiting job: %+v, %v", job, err)
	}

	// The next process finds it and works it.
	second := newNoteWorker()
	runner := startRunner(t, pool, second, jobs.Config{})
	if got := second.wait(t); got != "left behind" {
		t.Errorf("after the restart, the job that ran is %q", got)
	}
	job := waitForState(t, runner, inserted.ID, "completed")
	if job.Attempt != 1 || job.FinalizedAt == nil {
		t.Errorf("the finished job: %+v", job)
	}
}

func waitForState(t *testing.T, runner *jobs.Runner, id int64, state string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err := runner.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == state {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d is %s, want %s: %+v", id, job.State, state, job)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFailedJobIsRetriedWithBackoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// With River's own policy, a failed job waits before its next attempt:
	// about a second after the first failure, and longer after each further
	// one. The first wait is short enough to watch.
	t.Run("backoff", func(t *testing.T) {
		pool := dbtest.New(t)
		w := newNoteWorker()
		runner := startRunner(t, pool, w, jobs.Config{})
		inserted, err := runner.Insert(ctx, noteArgs{Note: "flaky", FailFirst: 1}, jobs.InsertOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if got := w.wait(t); got != "flaky" {
			t.Errorf("the job that ran is %q", got)
		}
		job := waitForState(t, runner, inserted.ID, "completed")
		if job.Attempt != 2 || job.LastError != "not yet" {
			t.Errorf("after one failure and a success: %+v", job)
		}
		// ScheduledAt is when the retry was due. River spreads retries by a
		// tenth either way, so a second's wait is at least 0.8 s.
		if waited := job.ScheduledAt.Sub(job.CreatedAt); waited < 800*time.Millisecond {
			t.Errorf("the retry was due %v after the job was made, want about a second", waited)
		}
	})

	// Retried at once, it succeeds on the attempt after its failures.
	t.Run("success after failures", func(t *testing.T) {
		pool := dbtest.New(t)
		w := newNoteWorker()
		runner := startRunner(t, pool, w, jobs.Config{RetryPolicy: retryNow{}})
		inserted, err := runner.Insert(ctx, noteArgs{Note: "third time lucky", FailFirst: 2}, jobs.InsertOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if got := w.wait(t); got != "third time lucky" {
			t.Errorf("the job that ran is %q", got)
		}
		if job := waitForState(t, runner, inserted.ID, "completed"); job.Attempt != 3 {
			t.Errorf("finished on attempt %d, want 3", job.Attempt)
		}
	})

	// A job that keeps failing is given up on, and says why.
	t.Run("given up", func(t *testing.T) {
		pool := dbtest.New(t)
		w := newNoteWorker()
		runner := startRunner(t, pool, w, jobs.Config{RetryPolicy: retryNow{}})
		inserted, err := runner.Insert(ctx, noteArgs{Note: "hopeless", FailFirst: 99}, jobs.InsertOpts{MaxAttempts: 2})
		if err != nil {
			t.Fatal(err)
		}
		job := waitForState(t, runner, inserted.ID, "discarded")
		if job.Attempt != 2 || job.LastError != "not yet" || len(w.notes()) != 0 {
			t.Errorf("the job given up on: %+v", job)
		}
	})
}

func TestUniqueJobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)
	w := newNoteWorker()
	runner := startRunner(t, pool, w, jobs.Config{})
	unique := jobs.InsertOpts{Queue: jobs.QueueScan, Unique: true}

	// The first is still running when the second is asked for.
	first, err := runner.Insert(ctx, noteArgs{Note: "library A", TakeMS: 1500}, unique)
	if err != nil || first.Duplicate {
		t.Fatalf("first insert: %+v, %v", first, err)
	}
	again, err := runner.Insert(ctx, noteArgs{Note: "library A", TakeMS: 1500}, unique)
	if err != nil || !again.Duplicate || again.ID != first.ID {
		t.Errorf("asking again while it runs: %+v, %v, want the same job %d", again, err, first.ID)
	}
	// Other arguments are another job.
	other, err := runner.Insert(ctx, noteArgs{Note: "library B"}, unique)
	if err != nil || other.Duplicate || other.ID == first.ID {
		t.Errorf("another library: %+v, %v", other, err)
	}
	// Without Unique, the same arguments are two jobs.
	plain, err := runner.Insert(ctx, noteArgs{Note: "library A", TakeMS: 1500}, jobs.InsertOpts{Queue: jobs.QueueExtract})
	if err != nil || plain.Duplicate || plain.ID == first.ID {
		t.Errorf("a plain insert: %+v, %v", plain, err)
	}

	waitForState(t, runner, first.ID, "completed")
	// Once it is done, the same work may be asked for again.
	later, err := runner.Insert(ctx, noteArgs{Note: "library A", TakeMS: 1500}, unique)
	if err != nil || later.Duplicate || later.ID == first.ID {
		t.Errorf("asking again after it finished: %+v, %v", later, err)
	}

	listed, err := runner.List(ctx, 10, jobs.QueueScan)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].ID != later.ID {
		t.Errorf("%d scan jobs listed, newest %d; want 3 with %d first", len(listed), listed[0].ID, later.ID)
	}
}

func TestEveryQueueIsWorked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)
	w := newNoteWorker()
	runner := startRunner(t, pool, w, jobs.Config{})

	for _, queue := range jobs.Queues() {
		if _, err := runner.Insert(ctx, noteArgs{Note: queue}, jobs.InsertOpts{Queue: queue}); err != nil {
			t.Fatalf("%s: %v", queue, err)
		}
	}
	ran := map[string]bool{}
	for range jobs.Queues() {
		ran[w.wait(t)] = true
	}
	for _, queue := range jobs.Queues() {
		if !ran[queue] {
			t.Errorf("no worker took the job in queue %s", queue)
		}
	}
}

func TestPeriodicJob(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	w := newNoteWorker()
	startRunner(t, pool, w, jobs.Config{
		Periodic: []*river.PeriodicJob{jobs.Every(time.Hour, true, noteArgs{Note: "on the hour"}, jobs.QueueDefault)},
	})
	if got := w.wait(t); got != "on the hour" {
		t.Errorf("the periodic job that ran is %q", got)
	}
	// Once at start, then not again until the hour is up.
	w.quiet(t, time.Second)
}

func TestStopWaitsForRunningJobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)
	w := newNoteWorker()
	workers := jobs.NewWorkers()
	river.AddWorker(workers, w)
	runner, err := jobs.New(pool, jobs.Config{Logger: slog.New(slog.DiscardHandler), Workers: workers})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	inserted, err := runner.Insert(ctx, noteArgs{Note: "slow", TakeMS: 1200}, jobs.InsertOpts{})
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, runner, inserted.ID, "running")

	if err := runner.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop returned, so the job must have been allowed to finish.
	if got := w.notes(); len(got) != 1 || got[0] != "slow" {
		t.Errorf("after Stop the finished jobs are %v", got)
	}
}

func TestSessionSweepJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := dbtest.New(t)
	accounts, err := auth.NewService(pool, fastHash, auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		WITH u AS (INSERT INTO users (username, role) VALUES ('steve', 'admin') RETURNING id)
		INSERT INTO sessions (token_hash, user_id, expires_at)
		SELECT v.hash, u.id, v.expires FROM u, (VALUES
			('\x01'::bytea, now() - interval '1 day'),
			('\x02'::bytea, now() - interval '1 minute'),
			('\x03'::bytea, now() + interval '1 day')) AS v(hash, expires)`); err != nil {
		t.Fatal(err)
	}

	workers := jobs.NewWorkers()
	river.AddWorker(workers, &auth.SweepSessionsWorker{Service: accounts})
	runner, err := jobs.New(pool, jobs.Config{
		Logger:   slog.New(slog.DiscardHandler),
		Workers:  workers,
		Periodic: []*river.PeriodicJob{jobs.Every(time.Hour, true, auth.SweepSessionsArgs{}, jobs.QueueDefault)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Stop(context.Background()) })

	deadline := time.Now().Add(15 * time.Second)
	for {
		var left int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions left, want only the live one", left)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
