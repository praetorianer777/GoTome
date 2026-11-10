package updates

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// CheckArgs is the job that asks GitHub for the latest release when an
// answer is due.
type CheckArgs struct{}

// checkKind names the job in the queue. It is stored with every job, so it
// stays.
const checkKind = "updates.check"

func (CheckArgs) Kind() string { return checkKind }

// Periodic is the scheduled check, also at start. It runs more often than
// GitHub is asked so that a failed request is tried again within the hour.
func Periodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, CheckArgs{}, jobs.QueueDefault)
}

// CheckWorker runs CheckArgs.
type CheckWorker struct {
	river.WorkerDefaults[CheckArgs]
	Service *Service
}

func (w *CheckWorker) Timeout(*river.Job[CheckArgs]) time.Duration { return 2 * timeout }

func (w *CheckWorker) Work(ctx context.Context, _ *river.Job[CheckArgs]) error {
	_, err := w.Service.Check(ctx)
	return err
}
