package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Groups of job states, as the status page filters by them.
const (
	JobsWaiting = "waiting"
	JobsRunning = "running"
	JobsFailed  = "failed"
	JobsDone    = "done"
)

// jobStates are the queue's own states in each group. A job that failed but
// will be tried again is waiting; one that will not is failed, as is one
// somebody cancelled.
var jobStates = map[string][]string{
	JobsWaiting: {"available", "scheduled", "retryable", "pending"},
	JobsRunning: {"running"},
	JobsFailed:  {"discarded", "cancelled"},
	JobsDone:    {"completed"},
}

// MaxJobs is the most jobs one look at the status page lists.
const MaxJobs = 200

// ErrNoJob is a job that does not exist, is cleared away, or is about a
// library the viewer may not see.
var ErrNoJob = errors.New("no such job")

// JobStatus is a background job and what it is about.
type JobStatus struct {
	jobs.Job
	// LibraryID and LibraryName are the library the job works on, if any.
	LibraryID   *uuid.UUID
	LibraryName string
	// FileID, FilePath and BookID are the file a job reads or writes.
	FileID   *uuid.UUID
	FilePath string
	BookID   *uuid.UUID
}

// jobsQuery lists jobs with the library and file they concern. Which jobs
// those are is read from their arguments, by kind. A job about a library
// the viewer may not see is left out; one about no library, such as the
// sweep of old sessions, is listed for everyone who may see jobs at all.
const jobsQuery = `
SELECT j.id, j.kind, j.queue, j.state::text, j.attempt, j.max_attempts,
       j.created_at, j.scheduled_at, j.attempted_at, j.finalized_at,
       COALESCE(j.errors[array_length(j.errors, 1)]->>'error', ''),
       lib.id, COALESCE(lib.name, ''), f.id, COALESCE(f.rel_path, ''), f.book_id
FROM river_job j
LEFT JOIN book_files f
       ON j.kind IN ('` + extractKind + `', '` + writeBackKind + `') AND f.id = (j.args->>'fileId')::uuid
LEFT JOIN libraries lib
       ON lib.id = CASE WHEN j.kind = '` + scanKind + `' THEN (j.args->>'libraryId')::uuid ELSE f.library_id END
WHERE (lib.id IS NULL OR lib.id IN (SELECT visible_library_ids($1, $2)))
  AND (cardinality($3::text[]) = 0 OR j.state::text = ANY($3::text[]))
  AND ($4::bigint = 0 OR j.id = $4)
ORDER BY j.id DESC
LIMIT $5`

// Jobs lists the newest jobs the scope may see, of the state groups given or
// of all. The queue clears finished jobs away after a while.
func (s *Service) Jobs(ctx context.Context, scope library.Scope, groups []string, limit int) ([]JobStatus, error) {
	var states []string
	for _, g := range groups {
		in, ok := jobStates[g]
		if !ok {
			return nil, fmt.Errorf("no job state %q", g)
		}
		states = append(states, in...)
	}
	return s.jobs(ctx, scope, states, 0, min(max(limit, 1), MaxJobs))
}

func (s *Service) jobs(ctx context.Context, scope library.Scope, states []string, id int64, limit int) ([]JobStatus, error) {
	if states == nil {
		states = []string{}
	}
	rows, err := s.pool.Query(ctx, jobsQuery, scope.Viewer, scope.SeesAll, states, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobStatus{}
	for rows.Next() {
		var j JobStatus
		if err := rows.Scan(&j.ID, &j.Kind, &j.Queue, &j.State, &j.Attempt, &j.MaxAttempts,
			&j.CreatedAt, &j.ScheduledAt, &j.AttemptedAt, &j.FinalizedAt, &j.LastError,
			&j.LibraryID, &j.LibraryName, &j.FileID, &j.FilePath, &j.BookID); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// job is one job the scope may see, or ErrNoJob.
func (s *Service) job(ctx context.Context, scope library.Scope, id int64) (JobStatus, error) {
	found, err := s.jobs(ctx, scope, nil, id, 1)
	if err != nil {
		return JobStatus{}, err
	}
	if len(found) == 0 {
		return JobStatus{}, ErrNoJob
	}
	return found[0], nil
}

// RetryJob runs a job that is not running again, as soon as a worker is free.
func (s *Service) RetryJob(ctx context.Context, scope library.Scope, id int64) (JobStatus, error) {
	return s.actOn(ctx, scope, id, s.Queue.Retry)
}

// CancelJob stops a job that waits or runs.
func (s *Service) CancelJob(ctx context.Context, scope library.Scope, id int64) (JobStatus, error) {
	return s.actOn(ctx, scope, id, s.Queue.Cancel)
}

func (s *Service) actOn(ctx context.Context, scope library.Scope, id int64, act func(context.Context, int64) (jobs.Job, error)) (JobStatus, error) {
	if _, err := s.job(ctx, scope, id); err != nil {
		return JobStatus{}, err
	}
	if _, err := act(ctx, id); errors.Is(err, jobs.ErrNotFound) {
		return JobStatus{}, ErrNoJob
	} else if err != nil {
		return JobStatus{}, err
	}
	return s.job(ctx, scope, id)
}

// Reread has files read again: the one given, or a library's that failed,
// or all of a library's. Files of formats without a reader are left alone.
func (s *Service) Reread(ctx context.Context, files []uuid.UUID) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	var ids []uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		ids, err = sqlc.New(tx).ResetExtraction(ctx, sqlc.ResetExtractionParams{Ids: files, Formats: readableFormats()})
		if err != nil {
			return err
		}
		for _, id := range ids {
			_, err := s.Queue.InsertTx(ctx, tx, ExtractArgs{FileID: id}, jobs.InsertOpts{
				Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return len(ids), err
}

// LibraryFiles lists the files of a library to read again: the failed ones,
// or all.
func (s *Service) LibraryFiles(ctx context.Context, libraryID uuid.UUID, failedOnly bool) ([]uuid.UUID, error) {
	return sqlc.New(s.pool).ListLibraryFilesToReread(ctx, sqlc.ListLibraryFilesToRereadParams{
		LibraryID: libraryID, FailedOnly: failedOnly,
	})
}
