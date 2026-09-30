package auth

import (
	"context"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// SweepSessionsArgs is the job that deletes sessions which have run out.
// Signing in sweeps too, but an installation nobody signs in to for a month
// would keep every dead session until somebody does.
type SweepSessionsArgs struct{}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (SweepSessionsArgs) Kind() string { return "auth.sweep_sessions" }

// SweepSessionsWorker runs SweepSessionsArgs.
type SweepSessionsWorker struct {
	river.WorkerDefaults[SweepSessionsArgs]
	Service *Service
}

func (w *SweepSessionsWorker) Work(ctx context.Context, _ *river.Job[SweepSessionsArgs]) error {
	_, err := w.Service.SweepSessions(ctx)
	return err
}

// SweepSessions deletes the sessions that have expired and returns how many
// that were.
func (s *Service) SweepSessions(ctx context.Context) (int64, error) {
	return sqlc.New(s.pool).DeleteExpiredSessions(ctx, s.now())
}
