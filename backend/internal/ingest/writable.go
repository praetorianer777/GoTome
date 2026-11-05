package ingest

import (
	"context"
	"errors"
	"fmt"

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
// reason.
type unwritableError struct {
	library uuid.UUID
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

// probeFolder checks, before anything is written into a writable
// library's folder, that it can be written at all; a folder that cannot
// would otherwise fail every file on its own. It touches no database, so
// it may run inside a transaction; noteUnwritable records what it found
// once that is over.
func probeFolder(libraryID uuid.UUID, root string) error {
	if err := library.ProbeWritable(root); err != nil {
		return &unwritableError{library: libraryID, reason: library.UnwritableReason(err)}
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
		return sqlc.New(s.pool).ClearLibraryUnwritable(ctx, libraryID)
	}
	if err := s.noteUnwritable(ctx, err); !errors.Is(err, ErrFolderReadOnly) {
		return err
	}
	return nil
}
