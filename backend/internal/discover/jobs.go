package discover

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// passBudget is how long one run of the job asks before it snoozes and the
// next run goes on: the providers' intervals make an ask take a few seconds.
const passBudget = 4 * time.Minute

// RunArgs is the job that works the runs due.
type RunArgs struct{}

// runKind names the job in the queue. It is stored with every job, so it
// stays.
const runKind = "recommendations.discover"

func (RunArgs) Kind() string { return runKind }

// In the metadata queue, beside the lookups and the polls of followed
// subjects: all wait on the same providers' intervals.
var runOpts = jobs.InsertOpts{Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: 5}

// Periodic is the scheduled job, also at start; a person's run starts once a
// day however often it runs.
func Periodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, RunArgs{}, jobs.QueueMetadata)
}

// RunWorker runs RunArgs.
type RunWorker struct {
	river.WorkerDefaults[RunArgs]
	Service *Service
}

func (w *RunWorker) Timeout(*river.Job[RunArgs]) time.Duration { return passBudget + time.Minute }

func (w *RunWorker) Work(ctx context.Context, _ *river.Job[RunArgs]) error {
	complete, err := w.Service.Run(ctx, time.Now().Add(passBudget))
	if err != nil {
		return err
	}
	if !complete {
		return river.JobSnooze(time.Second)
	}
	return nil
}
