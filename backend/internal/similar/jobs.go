package similar

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// passBudget is how long one run embeds before it snoozes and the next run
// goes on, so that a shutdown or a cancel from the jobs page never waits
// long.
const passBudget = 5 * time.Minute

// EmbedArgs is the job that embeds every book whose vectors are missing or
// stale.
type EmbedArgs struct{}

// embedKind names the job in the queue. It is stored with every job, so it
// stays.
const embedKind = "similar.embed_books"

func (EmbedArgs) Kind() string { return embedKind }

// The embed queue has one worker, so books are embedded one at a time, and
// the queue only this job: it never holds up scanning or extraction.
var embedOpts = jobs.InsertOpts{Queue: jobs.QueueEmbed, Unique: true, MaxAttempts: 5}

// Enqueue asks for a pass over the books; a pass already waiting or
// running is enough.
func (s *Service) Enqueue(ctx context.Context) error {
	_, err := s.Queue.Insert(ctx, EmbedArgs{}, embedOpts)
	return err
}

// EnqueueTx asks for a pass once the transaction commits. It has the
// signature of ingest.Service.OnChunked.
func (s *Service) EnqueueTx(ctx context.Context, tx pgx.Tx) error {
	_, err := s.Queue.InsertTx(ctx, tx, EmbedArgs{}, embedOpts)
	return err
}

// Periodic is the scheduled pass, also at start, which picks up what changed
// while a pass was running and edits to books, which ask for none.
func Periodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, EmbedArgs{}, jobs.QueueEmbed)
}

// EmbedWorker runs EmbedArgs.
type EmbedWorker struct {
	river.WorkerDefaults[EmbedArgs]
	Service *Service
}

func (w *EmbedWorker) Timeout(*river.Job[EmbedArgs]) time.Duration { return passBudget + time.Minute }

func (w *EmbedWorker) Work(ctx context.Context, _ *river.Job[EmbedArgs]) error {
	complete, err := w.Service.Embed(ctx, time.Now().Add(passBudget), 0)
	switch {
	case errors.Is(err, ErrUnavailable):
		w.Service.log.Warn("books are not embedded", "error", err)
		return nil
	case err != nil:
		return err
	case !complete:
		return river.JobSnooze(time.Second)
	}
	return nil
}
