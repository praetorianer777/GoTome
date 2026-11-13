package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/textproc"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

// saveWordsTx records the words of a primary text file's chunks (#526). It
// runs where the chunks are written, with their text in memory, and replaces
// the counts of an earlier chunking.
func saveWordsTx(ctx context.Context, q *sqlc.Queries, fileID uuid.UUID, texts []string) error {
	counts := wordcount.Count(texts)
	return q.UpsertFileWords(ctx, sqlc.UpsertFileWordsParams{
		FileID: fileID, Version: wordcount.Version, Words: int32(len(counts)), Counts: wordcount.Encode(counts),
	})
}

// CountWords counts the words of a primary text file read again from the
// file, for books chunked before counting existed. A file that is gone,
// unreadable or no longer its book's text is left as it is.
func (s *Service) CountWords(ctx context.Context, fileID uuid.UUID) error {
	file, err := sqlc.New(s.pool).GetFileForChunking(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	extract, ok := extractors[file.Format]
	if !ok || file.PrimaryTextFileID == nil || *file.PrimaryTextFileID != fileID {
		return nil
	}
	got, err := safely(ctx, file.Format, extract, filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrUnreadable):
		return nil
	case err != nil:
		return err
	}
	chunks := textproc.Split(chunkSections(file.Format, got.Sections), textproc.Options{CharsPerPage: charsPerPage})
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrary(ctx, file.LibraryID); err != nil {
			return err
		}
		primary, err := q.GetPrimaryTextFile(ctx, file.BookID)
		if err != nil || primary == nil || *primary != fileID {
			return err
		}
		return saveWordsTx(ctx, q, fileID, texts)
	})
}

// CountFiles counts the primary text files whose words are not counted yet,
// until the deadline; it says whether any are left. A file that cannot be
// counted for good (gone, broken) would come back every time, so the pass
// goes on by ID and stops at the end of the list once.
func (s *Service) CountFiles(ctx context.Context, deadline time.Time) (bool, error) {
	q := sqlc.New(s.pool)
	after := uuid.Nil
	for {
		ids, err := q.ListFilesToCount(ctx, sqlc.ListFilesToCountParams{After: after, Page: indexPage, Version: wordcount.Version})
		if err != nil || len(ids) == 0 {
			return false, err
		}
		for _, id := range ids {
			if time.Now().After(deadline) {
				return true, nil
			}
			if err := s.CountWords(ctx, id); err != nil {
				return false, fmt.Errorf("count words of file %s: %w", id, err)
			}
			after = id
		}
	}
}

// EnqueueCounting queues the job that counts what is not counted; it finds
// nothing to do when everything is.
func (s *Service) EnqueueCounting(ctx context.Context) error {
	_, err := s.Queue.InsertMany(ctx, []river.JobArgs{CountFilesArgs{}}, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	return err
}

// CountFilesArgs is the job that reads files again to count their words.
type CountFilesArgs struct{}

// countFilesKind names the job in the queue. It is stored with every job,
// so it stays.
const countFilesKind = "ingest.count_words"

func (CountFilesArgs) Kind() string { return countFilesKind }

// CountFilesWorker runs CountFilesArgs, for passBudget at a time.
type CountFilesWorker struct {
	river.WorkerDefaults[CountFilesArgs]
	Service *Service
}

func (w *CountFilesWorker) Timeout(*river.Job[CountFilesArgs]) time.Duration {
	return passBudget + extractTimeout
}

func (w *CountFilesWorker) Work(ctx context.Context, _ *river.Job[CountFilesArgs]) error {
	more, err := w.Service.CountFiles(ctx, time.Now().Add(passBudget))
	if err != nil {
		return err
	}
	if more {
		return river.JobSnooze(time.Second)
	}
	return nil
}
