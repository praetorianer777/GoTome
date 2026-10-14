package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

var (
	// ErrNotFixable is a file Fix does not change: not an EPUB, protected
	// by DRM, or one it cannot read.
	ErrNotFixable = errors.New("the file cannot be fixed")
	// ErrNothingToFix is a fix that found nothing to change in the file.
	ErrNothingToFix = errors.New("there is nothing to fix in the file")
	// ErrFileChanged is a file whose bytes are not those last read: the
	// scan reads it again first.
	ErrFileChanged = errors.New("the file changed since it was read")
)

// Fix makes an EPUB of a writable library the scope sees lighter for
// e-readers, by the fixes of the findings given (epub.Fix). The fixed file
// is written beside the original under a name the scan passes by and read
// back; then, in one transaction, the original goes into the trash as a
// file of its own, named "<name> (original).epub" so that it can be
// restored beside the fixed one, and the fixed file takes its place and
// its record, original_sha256 kept. The file is read again for its text,
// places read in it are kept by how far they are, and devices that had
// the book are sent it again.
func (s *Service) Fix(ctx context.Context, scope library.Scope, fileID uuid.UUID, findings []string) (epub.Fixed, error) {
	f, err := sqlc.New(s.pool).GetFileToFix(ctx, sqlc.GetFileToFixParams{ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return epub.Fixed{}, ErrNoFile
	case err != nil:
		return epub.Fixed{}, err
	case f.TrashedAt != nil:
		return epub.Fixed{}, ErrNoFile
	case !f.Writable:
		return epub.Fixed{}, ErrReadOnly
	case f.MissingAt != nil:
		return epub.Fixed{}, ErrFileMissing
	case f.Format != "epub" || f.Drm || f.Sha256 == nil:
		return epub.Fixed{}, ErrNotFixable
	}

	path := filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath))
	src, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return epub.Fixed{}, ErrFileMissing
	}
	if err != nil {
		return epub.Fixed{}, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return epub.Fixed{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(src, 0, info.Size())); err != nil {
		return epub.Fixed{}, err
	}
	if !bytes.Equal(h.Sum(nil), f.Sha256) {
		return epub.Fixed{}, ErrFileChanged
	}

	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".gotome-fix")
	_ = os.Remove(temp)
	out, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return epub.Fixed{}, fmt.Errorf("write %s: %w", f.RelPath, err)
	}
	placed := false
	defer func() {
		if !placed {
			out.Close()
			_ = os.Remove(temp)
		}
	}()
	sum := sha256.New()
	written := &countingWriter{w: io.MultiWriter(out, sum)}
	fixed, err := epub.Fix(src, info.Size(), written, findings, time.Now().UTC())
	switch {
	case errors.Is(err, epub.ErrNotWritable), errors.Is(err, epub.ErrNotEPUB), errors.Is(err, epub.ErrTooLarge):
		return epub.Fixed{}, fmt.Errorf("%w: %v", ErrNotFixable, err)
	case err != nil:
		return epub.Fixed{}, fmt.Errorf("fix %s: %w", f.RelPath, err)
	case !fixed.Any():
		return fixed, ErrNothingToFix
	}
	if err := out.Sync(); err != nil {
		return epub.Fixed{}, err
	}
	if err := out.Close(); err != nil {
		return epub.Fixed{}, err
	}
	if err := sameContent(temp, nil); err != nil {
		return epub.Fixed{}, fmt.Errorf("%w: %v", ErrNotFixable, err)
	}
	measures, err := measureEPUB(temp)
	if err != nil {
		return epub.Fixed{}, fmt.Errorf("%w: %v", ErrNotFixable, err)
	}

	var undo []func()
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfFiles(ctx, []uuid.UUID{fileID}); err != nil {
			return err
		}
		current, err := q.LockFileHash(ctx, fileID)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, f.Sha256) {
			return ErrFileChanged
		}
		if err := q.LockLibraryFiles(ctx, f.LibraryID); err != nil {
			return err
		}
		relPath, err := originalPath(ctx, q, f.LibraryID, f.RootPath, f.RelPath)
		if err != nil {
			return err
		}
		trashedID, err := q.AddTrashedOriginal(ctx, sqlc.AddTrashedOriginalParams{RelPath: relPath, By: scope.Viewer, ID: fileID})
		if err != nil {
			return err
		}
		if err := q.ProgressOfFileByFraction(ctx, &fileID); err != nil {
			return err
		}
		if err := q.SendBookAgain(ctx, f.BookID); err != nil {
			return err
		}
		if _, err := s.Queue.InsertTx(ctx, tx, ExtractArgs{FileID: fileID}, jobs.InsertOpts{
			Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
		}); err != nil {
			return err
		}
		if err := s.filesChangedTx(ctx, tx, f.BookID); err != nil {
			return err
		}

		// Last, so that nothing after them can fail but the commit.
		trashed := trashPath(f.RootPath, trashedID, relPath)
		if err := os.MkdirAll(filepath.Dir(trashed), 0o755); err != nil {
			return err
		}
		if err := os.Rename(path, trashed); err != nil {
			return err
		}
		undo = append(undo, func() { s.moveBack(trashed, path) })
		if err := os.Rename(temp, path); err != nil {
			return err
		}
		placed = true
		undo = append(undo, func() { _ = os.Rename(path, temp) })
		after, err := os.Stat(path)
		if err != nil {
			return err
		}
		sha := sum.Sum(nil)
		if err := q.SetFileFixed(ctx, sqlc.SetFileFixedParams{
			ID: fileID, Sha256: sha, SizeBytes: written.n, ModifiedAt: storedTime(after.ModTime()),
		}); err != nil {
			return err
		}
		// After the new hash, which clears the measures of the old bytes.
		return setMeasures(ctx, q, fileID, sha, measures)
	})
	if err != nil {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		if placed {
			_ = os.Remove(temp)
		}
		return epub.Fixed{}, err
	}
	return fixed, nil
}

// originalPath is where the file before a fix lies once restored: beside
// the fixed one, as "<name> (original).epub", numbered when that is taken.
func originalPath(ctx context.Context, q *sqlc.Queries, libraryID uuid.UUID, root, relPath string) (string, error) {
	dir, base := path.Split(relPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for n := 1; ; n++ {
		suffix := " (original)"
		if n > 1 {
			suffix = " (original " + strconv.Itoa(n) + ")"
		}
		candidate := dir + stem + suffix + ext
		taken, err := q.RelPathTaken(ctx, sqlc.RelPathTakenParams{LibraryID: libraryID, RelPath: candidate})
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(candidate))); !taken && errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
	}
}

// FixBook fixes each of the book's EPUBs that has one of the findings, as
// Fix does, and says how many it changed. A file of a read-only library is
// an error; one in which there turned out to be nothing to fix is not.
func (s *Service) FixBook(ctx context.Context, scope library.Scope, bookID uuid.UUID, findings []string) (int, error) {
	files, err := sqlc.New(s.pool).ListFilesToFix(ctx, sqlc.ListFilesToFixParams{BookID: bookID, Findings: findings})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range files {
		f, err := sqlc.New(s.pool).GetFileToFix(ctx, sqlc.GetFileToFixParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return n, err
		}
		own := slices.DeleteFunc(slices.Clone(findings), func(x string) bool { return !slices.Contains(f.EreaderFindings, x) })
		switch _, err := s.Fix(ctx, scope, id, own); {
		case errors.Is(err, ErrNothingToFix):
		case err != nil:
			return n, err
		default:
			n++
		}
	}
	return n, nil
}
