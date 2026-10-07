package releases

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// passBudget is how long one run asks before it snoozes and the next run
// goes on: the providers' intervals make a subject take a few seconds.
const passBudget = 4 * time.Minute

// PollArgs is the job that asks the providers about the subjects due.
type PollArgs struct{}

// pollKind names the job in the queue. It is stored with every job, so it
// stays.
const pollKind = "releases.poll"

func (PollArgs) Kind() string { return pollKind }

// In the metadata queue, beside the lookups: both wait on the same
// providers' intervals.
var pollOpts = jobs.InsertOpts{Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: 5}

// Periodic is the scheduled poll, also at start; a subject is asked about
// once a day however often it runs.
func Periodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, PollArgs{}, jobs.QueueMetadata)
}

// PollWorker runs PollArgs.
type PollWorker struct {
	river.WorkerDefaults[PollArgs]
	Service *Service
}

func (w *PollWorker) Timeout(*river.Job[PollArgs]) time.Duration { return passBudget + time.Minute }

func (w *PollWorker) Work(ctx context.Context, _ *river.Job[PollArgs]) error {
	complete, err := w.Service.Poll(ctx, time.Now().Add(passBudget))
	if err != nil {
		return err
	}
	if !complete {
		return river.JobSnooze(time.Second)
	}
	return nil
}
