package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Scan states, as stored.
const (
	StateQueued  = "queued"
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
)

const (
	// passBudget is how long one run of the scan job works before it hands
	// the queue back and continues in its next run. It keeps a run well below
	// passTimeout however large the library is.
	passBudget = 20 * time.Minute
	// passTimeout ends a run that hangs, on a dead network mount for one. A
	// job without a timeout would never be taken up again after a crash,
	// which is why there is a budget and not simply no limit. It stays below
	// the hour after which the queue considers a running job stuck.
	passTimeout = 45 * time.Minute
	// A run that is cut off, by a shutdown or a crash, is one attempt used up.
	// A scan that fails is not tried again at all: the next schedule, or
	// whoever fixed the cause, asks for a new one.
	scanAttempts = 5
	// keptScans is how many past scans of a library are remembered.
	keptScans = 50
)

// What a scan that failed says to whoever looks at the library. Where the
// folder lies on the server is not for every reader, so the detail stays in
// the log.
const (
	messageNoFolder = "The library's folder could not be read. Check that it is mounted into the GOtome container."
	messageFailed   = "The scan stopped on an error. The server log says why."
)

// Scan is one scan of a library as the API shows it.
type Scan struct {
	ID          uuid.UUID  `json:"id"`
	LibraryID   uuid.UUID  `json:"libraryId"`
	State       string     `json:"state"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	// FilesSeen is how many files of known formats the folder holds.
	FilesSeen     int32 `json:"filesSeen"`
	FilesAdded    int32 `json:"filesAdded"`
	FilesChanged  int32 `json:"filesChanged"`
	FilesMoved    int32 `json:"filesMoved"`
	FilesRestored int32 `json:"filesRestored"`
	FilesMissing  int32 `json:"filesMissing"`
	// FilesSkipped is how many files and folders could not be read.
	FilesSkipped int32  `json:"filesSkipped"`
	BooksAdded   int32  `json:"booksAdded"`
	Error        string `json:"error,omitempty"`
}

func scanFromRow(row sqlc.LibraryScan) Scan {
	scan := Scan{
		ID: row.ID, LibraryID: row.LibraryID, State: row.State, RequestedAt: row.RequestedAt,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		FilesSeen: row.FilesSeen, FilesAdded: row.FilesAdded, FilesChanged: row.FilesChanged,
		FilesMoved: row.FilesMoved, FilesRestored: row.FilesRestored, FilesMissing: row.FilesMissing,
		FilesSkipped: row.FilesSkipped, BooksAdded: row.BooksAdded,
	}
	if row.Error != nil {
		scan.Error = *row.Error
	}
	return scan
}

// Queue is the part of the job runner the service uses.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
	InsertMany(ctx context.Context, args []river.JobArgs, opts jobs.InsertOpts) (int, error)
	Get(ctx context.Context, id int64) (jobs.Job, error)
	Retry(ctx context.Context, id int64) (jobs.Job, error)
	Cancel(ctx context.Context, id int64) (jobs.Job, error)
}

// Service asks for scans and runs them.
type Service struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	libraries *library.Service
	scanner   *Scanner
	covers    *covers.Store
	// Queue is set once the job runner exists: the runner is built from the
	// workers, and the workers from this service.
	Queue Queue
	// UploadLimit is the largest file Upload takes, in bytes.
	UploadLimit int64
	// OnExtracted, when set, runs in the transaction that records what a
	// file says about its book: looking the book up starts there.
	OnExtracted func(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error
	// OnChunked, when set, runs in the transaction that wrote a book's
	// chunks from the file: its signature for overlap is made there.
	OnChunked func(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID) error
}

// NewService returns a Service. Its Queue must be set before Request is called.
func NewService(pool *pgxpool.Pool, libraries *library.Service, store *covers.Store, log *slog.Logger) *Service {
	scanner := NewScanner(pool, log)
	scanner.Budget = passBudget
	return &Service{pool: pool, log: log, libraries: libraries, scanner: scanner, covers: store}
}

// Request asks for the library to be scanned and returns the scan that will
// do it. While a scan of the library is waiting or running, that is the one
// returned: asking twice is one scan. by is nil when the schedule asks.
//
// That there is one scan at a time is kept by the scans table, not by the
// queue: a job that has just finished its scan is still listed as running
// there for a moment, and would turn away the next request for nothing.
func (s *Service) Request(ctx context.Context, libraryID uuid.UUID, by *uuid.UUID) (Scan, error) {
	var scan Scan
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.QueueScan(ctx, sqlc.QueueScanParams{LibraryID: libraryID, RequestedBy: by})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err = q.LockActiveScan(ctx, libraryID)
			if errors.Is(err, pgx.ErrNoRows) {
				// It finished between the two statements.
				row, err = q.QueueScan(ctx, sqlc.QueueScanParams{LibraryID: libraryID, RequestedBy: by})
			}
		}
		if err != nil {
			return err
		}
		scan = scanFromRow(row)
		if row.JobID != nil {
			job, err := s.Queue.Get(ctx, *row.JobID)
			switch {
			case err == nil && !job.Finished():
				return nil
			case err != nil && !errors.Is(err, jobs.ErrNotFound):
				return err
			}
			// The scan outlived its job, which happens when a run is cut off
			// more often than the job has attempts. Asking again revives it.
		}
		job, err := s.Queue.InsertTx(ctx, tx, ScanArgs{LibraryID: libraryID}, jobs.InsertOpts{
			Queue: jobs.QueueScan, MaxAttempts: scanAttempts,
		})
		if err != nil {
			return err
		}
		return q.SetScanJob(ctx, sqlc.SetScanJobParams{ID: row.ID, JobID: &job.ID})
	})
	return scan, err
}

// RequestAll asks for every library to be scanned, which is what the schedule
// does.
func (s *Service) RequestAll(ctx context.Context) error {
	ids, err := sqlc.New(s.pool).ListLibraryIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.Request(ctx, id, nil); err != nil {
			return err
		}
	}
	return nil
}

// Latest returns the newest scan of each library the scope may see, by
// library.
func (s *Service) Latest(ctx context.Context, scope library.Scope) (map[uuid.UUID]Scan, error) {
	rows, err := sqlc.New(s.pool).LatestVisibleScans(ctx, sqlc.LatestVisibleScansParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return nil, err
	}
	scans := make(map[uuid.UUID]Scan, len(rows))
	for _, row := range rows {
		scans[row.LibraryID] = scanFromRow(row)
	}
	return scans, nil
}

// Pending returns, per library the scope may see, how many of the files its
// scans found are still waiting to be read. Libraries with none are left out.
func (s *Service) Pending(ctx context.Context, scope library.Scope) (map[uuid.UUID]int32, error) {
	rows, err := sqlc.New(s.pool).CountPendingFiles(ctx, sqlc.CountPendingFilesParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return nil, err
	}
	pending := make(map[uuid.UUID]int32, len(rows))
	for _, row := range rows {
		pending[row.LibraryID] = row.Pending
	}
	return pending, nil
}

// Failed returns, per library the scope may see, how many of its files could
// not be read. Libraries with none are left out.
func (s *Service) Failed(ctx context.Context, scope library.Scope) (map[uuid.UUID]int32, error) {
	rows, err := sqlc.New(s.pool).CountFailedFiles(ctx, sqlc.CountFailedFilesParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return nil, err
	}
	failed := make(map[uuid.UUID]int32, len(rows))
	for _, row := range rows {
		failed[row.LibraryID] = row.Failed
	}
	return failed, nil
}

// Run makes one pass of the library's scan and records it. complete is false
// when the pass stopped at its budget and another has to follow.
func (s *Service) Run(ctx context.Context, libraryID uuid.UUID, jobID int64) (complete bool, err error) {
	lib, err := s.libraries.Get(ctx, library.Scope{SeesAll: true}, libraryID)
	if errors.Is(err, library.ErrNotFound) {
		// Removed since the scan was asked for; its scans went with it.
		return true, nil
	}
	if err != nil {
		return false, err
	}
	q := sqlc.New(s.pool)
	scan, err := q.StartScan(ctx, sqlc.StartScanParams{LibraryID: libraryID, JobID: &jobID})
	if err != nil {
		return false, err
	}

	res, scanErr := s.scanner.Scan(ctx, libraryID, lib.RootPath)

	// What the pass did is on record even when the pass was cut off, so the
	// bookkeeping must not be cut off with it.
	record := context.WithoutCancel(ctx)
	err = q.RecordScanProgress(record, sqlc.RecordScanProgressParams{
		ID: scan.ID, FilesSeen: int32(res.Seen), FilesAdded: int32(res.Added),
		FilesChanged: int32(res.Changed), FilesMoved: int32(res.Moved),
		FilesRestored: int32(res.Restored), FilesMissing: int32(res.Missing),
		FilesSkipped: int32(res.Skipped), BooksAdded: int32(res.Books),
	})
	if err != nil {
		return false, err
	}
	// After every pass, not only the last: the books a long first scan has
	// taken in so far get their titles and covers while it goes on.
	if ctx.Err() == nil {
		if err := s.EnqueuePending(ctx, libraryID); err != nil {
			return false, err
		}
	}

	switch {
	case scanErr != nil && ctx.Err() != nil:
		// Stopped from outside, by a shutdown or the timeout. The scan stays
		// open and the job's next run continues it.
		return false, scanErr
	case scanErr != nil:
		s.log.Error("library scan failed", "library", lib.Name, "error", scanErr)
		message := messageFailed
		if errors.Is(scanErr, ErrNoFolder) {
			message = messageNoFolder
		}
		if err := q.FinishScan(record, sqlc.FinishScanParams{ID: scan.ID, State: StateFailed, Error: &message}); err != nil {
			return false, err
		}
		return false, scanErr
	case !res.Complete:
		return false, nil
	}

	if err := q.FinishScan(record, sqlc.FinishScanParams{ID: scan.ID, State: StateDone}); err != nil {
		return false, err
	}
	if err := q.PruneScans(record, sqlc.PruneScansParams{LibraryID: libraryID, Keep: keptScans}); err != nil {
		return false, err
	}
	if res.Added+res.Changed+res.Moved+res.Restored+res.Missing+res.Skipped > 0 {
		s.log.Info("library scanned", "library", lib.Name, "seen", res.Seen, "added", res.Added,
			"books", res.Books, "changed", res.Changed, "moved", res.Moved,
			"restored", res.Restored, "missing", res.Missing, "skipped", res.Skipped)
	}
	return true, nil
}

// ScanArgs is the job that scans one library.
type ScanArgs struct {
	LibraryID uuid.UUID `json:"libraryId"`
}

// scanKind names the job in the queue. It is stored with every job, so it
// stays.
const scanKind = "ingest.scan_library"

func (ScanArgs) Kind() string { return scanKind }

// ScanWorker runs ScanArgs.
type ScanWorker struct {
	river.WorkerDefaults[ScanArgs]
	Service *Service
}

func (w *ScanWorker) Timeout(*river.Job[ScanArgs]) time.Duration { return passTimeout }

func (w *ScanWorker) Work(ctx context.Context, job *river.Job[ScanArgs]) error {
	complete, err := w.Service.Run(ctx, job.Args.LibraryID, job.ID)
	switch {
	case err != nil && ctx.Err() != nil:
		// Cut off from outside: the next attempt continues the scan.
		return err
	case err != nil:
		// The scan is on record as failed. Trying again in a few seconds
		// finds the same missing mount.
		return river.JobCancel(err)
	case !complete:
		return river.JobSnooze(time.Second)
	}
	return nil
}

// ScanAllArgs is the scheduled job that asks for every library to be scanned.
type ScanAllArgs struct{}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (ScanAllArgs) Kind() string { return "ingest.scan_all_libraries" }

// ScanAllWorker runs ScanAllArgs.
type ScanAllWorker struct {
	river.WorkerDefaults[ScanAllArgs]
	Service *Service
}

func (w *ScanAllWorker) Work(ctx context.Context, _ *river.Job[ScanAllArgs]) error {
	return w.Service.RequestAll(ctx)
}
