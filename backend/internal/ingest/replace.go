package ingest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	type move struct{ from, to string }
	var moved []move
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		files, err := q.ListLiveBookFiles(ctx, gone)
		if err != nil {
			return err
		}
		var pending []move
		for _, id := range files {
			f, err := q.GetFileToTrash(ctx, sqlc.GetFileToTrashParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
			if err != nil {
				return err
			}
			if !f.Writable {
				return ErrReadOnly
			}
			if len(pending) == 0 {
				if err := q.LockLibraryFiles(ctx, f.LibraryID); err != nil {
					return err
				}
			}
			if err := q.SetFileTrashed(ctx, sqlc.SetFileTrashedParams{ID: id, By: &scope.Viewer}); err != nil {
				return err
			}
			if err := q.DropFileSignature(ctx, id); err != nil {
				return err
			}
			pending = append(pending, move{
				from: filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath)),
				to:   trashPath(f.RootPath, id, f.RelPath),
			})
		}
		if err := s.mergeTx(ctx, tx, scope, keep, gone, fields); err != nil {
			return err
		}
		if err := q.ProgressByFraction(ctx, keep); err != nil {
			return err
		}
		if err := q.SetPairReplaced(ctx, sqlc.SetPairReplacedParams{IntoBook: keep, FromBook: gone}); err != nil {
			return err
		}
		// Last, so that nothing after them can fail but the commit.
		for _, m := range pending {
			if err := os.MkdirAll(filepath.Dir(m.to), 0o755); err != nil {
				return err
			}
			if err := os.Rename(m.from, m.to); err != nil {
				return err
			}
			moved = append(moved, m)
		}
		return nil
	})
	if err != nil {
		for _, m := range moved {
			s.moveBack(m.to, m.from)
		}
	}
	return err
}
