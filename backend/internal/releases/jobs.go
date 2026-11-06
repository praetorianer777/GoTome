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
func Periodic(interval time.Duration) jobs.Schedule {
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

// FetchFeedsArgs is the job that fetches the feeds due.
type FetchFeedsArgs struct{}

// fetchKind names the job in the queue. It is stored with every job, so it
// stays.
const fetchKind = "releases.fetch_feeds"

func (FetchFeedsArgs) Kind() string { return fetchKind }

// Beside the poll, as both reach other servers; a feed has no interval to
// wait on, but a feed that is slow to answer keeps a worker.
var fetchOpts = jobs.InsertOpts{Queue: jobs.QueueMetadata, Unique: true, MaxAttempts: 5}

// FeedsPeriodic is the scheduled fetch, also at start; a feed is fetched
// once an hour however often it runs, less often after failures.
func FeedsPeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, FetchFeedsArgs{}, jobs.QueueMetadata)
}

// FetchFeedsWorker runs FetchFeedsArgs.
type FetchFeedsWorker struct {
	river.WorkerDefaults[FetchFeedsArgs]
	Service *Service
}

func (w *FetchFeedsWorker) Timeout(*river.Job[FetchFeedsArgs]) time.Duration {
	return passBudget + feedTimeout + time.Minute
}

func (w *FetchFeedsWorker) Work(ctx context.Context, _ *river.Job[FetchFeedsArgs]) error {
	complete, err := w.Service.FetchFeeds(ctx, time.Now().Add(passBudget))
	if err != nil {
		return err
	}
	if !complete {
		return river.JobSnooze(time.Second)
	}
	return nil
}
