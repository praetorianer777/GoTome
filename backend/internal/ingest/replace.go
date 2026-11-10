package ingest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Replace keeps the book keep of two that are one and lets the other go:
// its files move to the trash, and the book is merged into keep with
// everything people had with it. A place read in one of its files carries
// over by how far it was, as a fraction. Restoring a file from the trash
// brings it back, to keep. Both books must be in one writable library.
func (s *Service) Replace(ctx context.Context, scope library.Scope, keep, gone uuid.UUID) error {
	keep, gone, fields, err := s.prepareMerge(ctx, scope, keep, gone, nil)
	if err != nil {
		return err
	}
	var made []fileMove
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{keep, gone}); err != nil {
			return err
		}
		snapshot, err := s.Curation.Begin(ctx, tx, curation.SetAll, keep, gone)
		if err != nil {
			return err
		}
		files, err := q.ListLiveBookFiles(ctx, gone)
		if err != nil {
			return err
		}
		pending, err := s.trashFilesTx(ctx, tx, scope, files)
		if err != nil {
			return err
		}
		if err := s.mergeTx(ctx, tx, scope, keep, gone, fields, nil); err != nil {
			return err
		}
		if err := q.ProgressByFraction(ctx, keep); err != nil {
			return err
		}
		if err := q.SetPairReplaced(ctx, sqlc.SetPairReplacedParams{IntoBook: keep, FromBook: gone}); err != nil {
			return err
		}
		if err := snapshot.Done(ctx, tx, curation.Record{
			RunKind: curation.KindDuplicates, By: scope.Viewer, Kind: curation.ActionReplace, TrashedFiles: files,
		}); err != nil {
			return err
		}
		return s.moveFiles(pending, &made)
	})
	if err != nil {
		s.moveFilesBack(made)
	}
	return s.noteUnwritable(ctx, err)
}

// fileMove is a file's move into the trash, made once its record is.
type fileMove struct {
	bookID  uuid.UUID
	from    string
	to      string
	library uuid.UUID
	root    string
}

// trashFilesTx records the files, of libraries the scope sees and that
// GOtome may change, as trashed, and says where each is to move to. The
// moves are made last, by moveFiles, so that nothing after them can fail
// but the commit.
func (s *Service) trashFilesTx(ctx context.Context, tx pgx.Tx, scope library.Scope, ids []uuid.UUID) ([]fileMove, error) {
	q := sqlc.New(tx)
	var pending []fileMove
	for _, id := range ids {
		f, err := q.GetFileToTrash(ctx, sqlc.GetFileToTrashParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return nil, err
		}
		if !f.Writable {
			return nil, ErrReadOnly
		}
		if len(pending) == 0 {
			if err := probeFolder(f.LibraryID, f.RootPath); err != nil {
				return nil, err
			}
			if err := q.LockLibraryFiles(ctx, f.LibraryID); err != nil {
				return nil, err
			}
		}
		if err := q.SetFileTrashed(ctx, sqlc.SetFileTrashedParams{ID: id, By: &scope.Viewer}); err != nil {
			return nil, err
		}
		if err := q.DropFileSignature(ctx, id); err != nil {
			return nil, err
		}
		pending = append(pending, fileMove{
			bookID:  f.BookID,
			from:    filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath)),
			to:      trashPath(f.RootPath, id, f.RelPath),
			library: f.LibraryID,
			root:    f.RootPath,
		})
	}
	return pending, nil
}

// moveFiles makes the moves, adding each to made as it is done.
func (s *Service) moveFiles(pending []fileMove, made *[]fileMove) error {
	for _, m := range pending {
		*made = append(*made, m)
		if err := os.MkdirAll(filepath.Dir(m.to), 0o755); err != nil {
			return folderError(m.library, m.root, err)
		}
		if err := os.Rename(m.from, m.to); err != nil {
			return folderError(m.library, m.root, err)
		}
	}
	return nil
}

// moveFilesBack takes back the moves that were made before a record failed.
func (s *Service) moveFilesBack(made []fileMove) {
	for _, m := range made {
		s.moveBack(m.to, m.from)
		_ = os.Remove(filepath.Dir(m.to))
	}
}
