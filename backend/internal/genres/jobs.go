package genres

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// passBudget is how long one run works before it snoozes and the next run
// goes on.
const passBudget = 5 * time.Minute

// SuggestArgs is the job that gives genres to every book a pass has not
// looked at.
type SuggestArgs struct{}

// suggestKind names the job in the queue. It is stored with every job, so
// it stays.
const suggestKind = "genres.suggest_books"

func (SuggestArgs) Kind() string { return suggestKind }

// On the embed queue, whose one worker keeps it from reading vectors while
// they are being made.
var suggestOpts = jobs.InsertOpts{Queue: jobs.QueueEmbed, Unique: true, MaxAttempts: 5}

// Enqueue asks for a pass; one already waiting or running is enough.
func (s *Service) Enqueue(ctx context.Context) error {
	_, err := s.Queue.Insert(ctx, SuggestArgs{}, suggestOpts)
	return err
}

// Periodic is the scheduled pass, also at start, which picks up books that
// changed or were embedded since.
func Periodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, SuggestArgs{}, jobs.QueueEmbed)
}

// SuggestWorker runs SuggestArgs.
type SuggestWorker struct {
	river.WorkerDefaults[SuggestArgs]
	Service *Service
}

func (w *SuggestWorker) Timeout(*river.Job[SuggestArgs]) time.Duration {
	return passBudget + time.Minute
}

func (w *SuggestWorker) Work(ctx context.Context, _ *river.Job[SuggestArgs]) error {
	complete, err := w.Service.Suggest(ctx, time.Now().Add(passBudget))
	switch {
	case errors.Is(err, similar.ErrUnavailable):
		w.Service.log.Warn("genres are not suggested", "error", err)
		return nil
	case err != nil:
		return err
	case !complete:
		return river.JobSnooze(time.Second)
	}
	return nil
}
