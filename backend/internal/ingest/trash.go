package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// trashFolder holds a library's trashed files, each in a folder named by
// its ID. A dot folder, which the scan passes by.
const trashFolder = ".trash"

var (
	// ErrNoFile is a file that does not exist or that the viewer does not
	// see, or, for a restore or a purge, one that is not in the trash.
	ErrNoFile = errors.New("no such file")
	// ErrReadOnly is a file of a library GOtome may not write to.
	ErrReadOnly = errors.New("the library is read-only")
	// ErrFileMissing is a file the last scan did not find, which cannot be
	// moved.
	ErrFileMissing = errors.New("the file is missing")
	// ErrPathTaken is a restore to a path where another file now lies.
	ErrPathTaken = errors.New("another file is where this one was")
)

// trashPath is where a trashed file lies.
func trashPath(root string, id uuid.UUID, relPath string) string {
	return filepath.Join(root, trashFolder, id.String(), path.Base(relPath))
}

// Trash moves a file of a writable library the scope sees into the
// library's trash. The record keeps its path, which stays taken, and the
// book reads its text from another file if this one was it. Nothing is
// deleted: Restore brings it back until the trash is purged.
func (s *Service) Trash(ctx context.Context, scope library.Scope, fileID uuid.UUID) error {
	var from, to string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		f, err := q.GetFileToTrash(ctx, sqlc.GetFileToTrashParams{ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoFile
		case err != nil:
			return err
		case f.TrashedAt != nil:
			return ErrNoFile
		case !f.Writable:
			return ErrReadOnly
		case f.MissingAt != nil:
			return ErrFileMissing
		}
		if err := q.LockLibraryFiles(ctx, f.LibraryID); err != nil {
			return err
		}
		if err := q.SetFileTrashed(ctx, sqlc.SetFileTrashedParams{ID: fileID, By: &scope.Viewer}); err != nil {
			return err
		}
		if err := s.textGoneTx(ctx, tx, f.BookID, fileID); err != nil {
			return err
		}
		if err := s.filesChangedTx(ctx, tx, f.BookID); err != nil {
			return err
		}
		// Last, so that nothing after it can fail but the commit.
		from = filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath))
		to = trashPath(f.RootPath, fileID, f.RelPath)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		return os.Rename(from, to)
	})
	if err != nil && to != "" {
		s.moveBack(to, from)
	}
	return err
}

// Restore brings a trashed file of a writable library the scope sees back
// to where it was, and to its book.
func (s *Service) Restore(ctx context.Context, scope library.Scope, fileID uuid.UUID) error {
	var from, to string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		f, err := q.GetFileToTrash(ctx, sqlc.GetFileToTrashParams{ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNoFile
		case err != nil:
			return err
		case f.TrashedAt == nil:
			return ErrNoFile
		case !f.Writable:
			return ErrReadOnly
		}
		if err := q.LockLibraryFiles(ctx, f.LibraryID); err != nil {
			return err
		}
		to = filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath))
		if _, err := os.Lstat(to); err == nil {
			return ErrPathTaken
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := q.SetFileRestored(ctx, fileID); err != nil {
			return err
		}
		primary, changed, err := choosePrimaryTx(ctx, q, f.BookID)
		if err != nil {
			return err
		}
		if changed && primary == fileID {
			if _, err := s.Queue.InsertTx(ctx, tx, ChunkArgs{FileID: fileID}, jobs.InsertOpts{
				Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
			}); err != nil {
				return err
			}
		}
		if err := s.filesChangedTx(ctx, tx, f.BookID); err != nil {
			return err
		}
		from = trashPath(f.RootPath, fileID, f.RelPath)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		return os.Rename(from, to)
	})
	if err != nil && from != "" {
		s.moveBack(to, from)
	} else if err == nil {
		_ = os.Remove(filepath.Dir(from))
	}
	return err
}

// moveBack undoes a move whose record failed to commit.
func (s *Service) moveBack(movedTo, from string) {
	if _, err := os.Lstat(movedTo); err != nil {
		return
	}
	if err := os.Rename(movedTo, from); err != nil {
		s.log.Error("trash: cannot move a file back after its record failed", "from", movedTo, "to", from, "error", err)
	}
}

// textGoneTx settles the book's text once the file can no longer be read
// for it: its chunks go, and another file is chosen and queued if it was
// the book's text.
func (s *Service) textGoneTx(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID) error {
	q := sqlc.New(tx)
	if err := q.DeleteFileChunks(ctx, fileID); err != nil {
		return err
	}
	if err := q.DropFileSignature(ctx, fileID); err != nil {
		return err
	}
	primary, changed, err := choosePrimaryTx(ctx, q, bookID)
	if err != nil || !changed || primary == uuid.Nil {
		return err
	}
	_, err = s.Queue.InsertTx(ctx, tx, ChunkArgs{FileID: primary}, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	return err
}

func (s *Service) filesChangedTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
	if s.OnFilesChanged == nil {
		return nil
	}
	return s.OnFilesChanged(ctx, tx, bookID)
}

// Purge deletes a trashed file for good: from the disk, and its record.
// The book stays, with its other files or none.
func (s *Service) Purge(ctx context.Context, fileID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		f, err := q.GetTrashedFile(ctx, fileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoFile
		}
		if err != nil {
			return err
		}
		if err := q.DeleteTrashedFile(ctx, fileID); err != nil {
			return err
		}
		if err := s.filesChangedTx(ctx, tx, f.BookID); err != nil {
			return err
		}
		// Last, so that nothing after it can fail but the commit.
		dir := filepath.Dir(trashPath(f.RootPath, fileID, f.RelPath))
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("delete %s: %w", dir, err)
		}
		return nil
	})
}

// PurgeExpired purges the files trashed longer ago than the retention.
func (s *Service) PurgeExpired(ctx context.Context, retention time.Duration) (int, error) {
	ids, err := sqlc.New(s.pool).ListExpiredTrash(ctx, time.Now().Add(-retention))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		switch err := s.Purge(ctx, id); {
		case errors.Is(err, ErrNoFile):
		case err != nil:
			return n, err
		default:
			n++
		}
	}
	if n > 0 {
		s.log.Info("purged the trash", "files", n)
	}
	return n, nil
}

// TrashedFile is a file in the trash, as its list shows it.
type TrashedFile = sqlc.ListTrashRow

// ListTrash lists the trashed files of the libraries the scope sees, of one
// when named, the latest first.
func (s *Service) ListTrash(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) ([]TrashedFile, error) {
	return sqlc.New(s.pool).ListTrash(ctx, sqlc.ListTrashParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
}

// PurgeTrashArgs is the job that purges what was trashed longer ago than
// the retention.
type PurgeTrashArgs struct{}

// purgeKind names the job in the queue. It is stored with every job, so it
// stays.
const purgeKind = "ingest.purge_trash"

func (PurgeTrashArgs) Kind() string { return purgeKind }

// PurgeTrashWorker runs PurgeTrashArgs. Retention says how long a file
// stays in the trash, as the settings have it now.
type PurgeTrashWorker struct {
	river.WorkerDefaults[PurgeTrashArgs]
	Service   *Service
	Retention func(context.Context) time.Duration
}

func (w *PurgeTrashWorker) Work(ctx context.Context, _ *river.Job[PurgeTrashArgs]) error {
	_, err := w.Service.PurgeExpired(ctx, w.Retention(ctx))
	return err
}
