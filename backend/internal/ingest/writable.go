package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// ErrFolderReadOnly is a writable library whose folder GOtome cannot write
// to now, such as a mount that turned read-only.
var ErrFolderReadOnly = errors.New("the library's folder cannot be written to")

// unwritableError is ErrFolderReadOnly for one library, with the system's
// reason and the folder it refused to write into.
type unwritableError struct {
	library uuid.UUID
	root    string
	folder  string
	reason  string
}

func (e *unwritableError) Error() string { return fmt.Sprintf("%v: %s", ErrFolderReadOnly, e.reason) }

func (e *unwritableError) Unwrap() error { return ErrFolderReadOnly }

// FolderReason is the system's reason in an ErrFolderReadOnly, such as
// "read-only file system", or "" for any other error.
func FolderReason(err error) string {
	var unwritable *unwritableError
	if errors.As(err, &unwritable) {
		return unwritable.reason
	}
	return ""
}

// FolderOf is the folder an ErrFolderReadOnly could not write into: the
// library's own or one inside it. It is "" for any other error.
func FolderOf(err error) string {
	var unwritable *unwritableError
	if errors.As(err, &unwritable) {
		return unwritable.folder
	}
	return ""
}

// folderError is err as ErrFolderReadOnly when the system refused to write
// into a folder of the library: no permission (EACCES, EPERM) or a
// read-only file system (EROFS). The probe of the library's folder passes
// where a folder inside it, such as the trash or an author's, is owned by
// someone else; that answers like a folder that cannot be written, not as a
// failure of the server's own. Any other error is returned as it is.
func folderError(libraryID uuid.UUID, root string, err error) error {
	if err == nil || !refused(err) {
		return err
	}
	var unwritable *unwritableError
	if errors.As(err, &unwritable) {
		return err
	}
	return &unwritableError{library: libraryID, root: root, folder: refusedFolder(err), reason: library.UnwritableReason(err)}
}

func refused(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS)
}

// refusedFolder is the folder an error refused to write into: the one a
// mkdir or a new file goes into, and of a rename's two the one that cannot
// be written, as the error does not say which.
func refusedFolder(err error) string {
	var link *os.LinkError
	if errors.As(err, &link) {
		if library.ProbeWritable(filepath.Dir(link.Old)) != nil {
			return filepath.Dir(link.Old)
		}
		return filepath.Dir(link.New)
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return filepath.Dir(pathErr.Path)
	}
	return ""
}

// probeFolder checks, before anything is written into a writable
// library's folder, that it can be written at all; a folder that cannot
// would otherwise fail every file on its own. It touches no database, so
// it may run inside a transaction; noteUnwritable records what it found
// once that is over.
func probeFolder(libraryID uuid.UUID, root string) error {
	if err := library.ProbeWritable(root); err != nil {
		return &unwritableError{library: libraryID, root: root, folder: root, reason: library.UnwritableReason(err)}
	}
	return nil
}

// noteUnwritable marks the library of an unwritable error as one whose
// folder cannot be written, which its managers are told of the first time.
// Any other error is left alone. It returns err.
func (s *Service) noteUnwritable(ctx context.Context, err error) error {
	var unwritable *unwritableError
	if !errors.As(err, &unwritable) {
		return err
	}
	record := context.WithoutCancel(ctx)
	if markErr := db.InTx(record, s.pool, func(tx pgx.Tx) error {
		reason := unwritable.reason
		if unwritable.folder != "" && unwritable.folder != unwritable.root {
			reason += " (" + unwritable.folder + ")"
		}
		name, err := sqlc.New(tx).MarkLibraryUnwritable(record, sqlc.MarkLibraryUnwritableParams{ID: unwritable.library, Reason: &reason})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		s.log.Warn("cannot write to a writable library's folder", "library", name, "error", reason)
		return s.Events.QueueTx(record, tx, notify.Event{
			Kind: notify.KindLibraryUnwritable, Libraries: []uuid.UUID{unwritable.library},
			Data: map[string]any{"library": name, "error": reason}, Link: "/admin/libraries",
		})
	}); markErr != nil {
		return markErr
	}
	return err
}

// checkFolder probes a writable library's folder at a scan: a mark from
// before is cleared when it can be written again, and set when it cannot.
func (s *Service) checkFolder(ctx context.Context, libraryID uuid.UUID, root string) error {
	err := probeFolder(libraryID, root)
	if err == nil {
		return s.WritableAgain(ctx, libraryID)
	}
	if err := s.noteUnwritable(ctx, err); !errors.Is(err, ErrFolderReadOnly) {
		return err
	}
	return nil
}

// WritableAgain follows a writable library's folder found writable: a mark
// that it was not is cleared, and the write-backs passed by meanwhile are
// queued again. Each is queued once, however often this runs.
func (s *Service) WritableAgain(ctx context.Context, libraryID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.ClearLibraryUnwritable(ctx, libraryID); err != nil {
			return err
		}
		files, err := q.TakeWriteBacksPending(ctx, libraryID)
		if err != nil {
			return err
		}
		for _, file := range files {
			if err := s.queueWriteBackTx(ctx, tx, file); err != nil {
				return err
			}
		}
		if len(files) > 0 {
			s.log.Info("queued the write-backs passed by while the folder could not be written", "library", libraryID, "files", len(files))
		}
		return nil
	})
}
