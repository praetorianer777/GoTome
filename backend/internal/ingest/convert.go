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
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/mobi"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

var (
	// ErrNotConvertible is a file ConvertToEPUB does not convert: not a
	// Mobipocket book (MOBI, AZW, AZW3), protected by DRM, or one it cannot
	// read.
	ErrNotConvertible = errors.New("the file cannot be converted to an EPUB")
	// ErrAlreadyConverted is a book that has the EPUB this conversion makes.
	ErrAlreadyConverted = errors.New("the book already has this EPUB")
)

// Converted says what ConvertToEPUB made.
type Converted struct {
	// FileID is the new file, Name its name in the library's folder.
	FileID uuid.UUID
	BookID uuid.UUID
	Name   string
	mobi.Converted
}

// ConvertToEPUB writes an EPUB from a MOBI, AZW or AZW3 file of a writable
// library the scope sees (mobi.ToEPUB), as a new file of the same book
// beside it. The original stays as it is. The EPUB is written under a name
// the scan passes by and read back; then, in one transaction, it is
// recorded and renamed into place, and read as any new file is.
func (s *Service) ConvertToEPUB(ctx context.Context, scope library.Scope, fileID uuid.UUID) (Converted, error) {
	f, err := sqlc.New(s.pool).GetFileToFix(ctx, sqlc.GetFileToFixParams{ID: fileID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Converted{}, ErrNoFile
	case err != nil:
		return Converted{}, err
	case f.TrashedAt != nil:
		return Converted{}, ErrNoFile
	case !f.Writable:
		return Converted{}, ErrReadOnly
	case f.MissingAt != nil:
		return Converted{}, ErrFileMissing
	case !isKindleFormat(f.Format) || f.Drm || f.Sha256 == nil:
		return Converted{}, ErrNotConvertible
	}

	if err := s.noteUnwritable(ctx, probeFolder(f.LibraryID, f.RootPath)); err != nil {
		return Converted{}, err
	}
	source := filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath))
	src, err := os.Open(source)
	if errors.Is(err, fs.ErrNotExist) {
		return Converted{}, ErrFileMissing
	}
	if err != nil {
		return Converted{}, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return Converted{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(src, 0, info.Size())); err != nil {
		return Converted{}, err
	}
	if !bytes.Equal(h.Sum(nil), f.Sha256) {
		return Converted{}, ErrFileChanged
	}

	temp := filepath.Join(filepath.Dir(source), "."+filepath.Base(source)+".gotome-convert")
	_ = os.Remove(temp)
	out, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Converted{}, s.noteUnwritable(ctx, folderError(f.LibraryID, f.RootPath, fmt.Errorf("write %s: %w", f.RelPath, err)))
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
	// The time is the file's, so that the same book gives the same bytes.
	made, err := mobi.ToEPUB(src, info.Size(), written, info.ModTime().UTC())
	switch {
	case errors.Is(err, mobi.ErrProtected), errors.Is(err, mobi.ErrNotMOBI), errors.Is(err, mobi.ErrTooLarge):
		return Converted{}, fmt.Errorf("%w: %v", ErrNotConvertible, err)
	case err != nil:
		return Converted{}, fmt.Errorf("convert %s: %w", f.RelPath, err)
	}
	if err := out.Sync(); err != nil {
		return Converted{}, err
	}
	if err := out.Close(); err != nil {
		return Converted{}, err
	}
	if err := sameContent(temp, nil); err != nil {
		return Converted{}, fmt.Errorf("%w: %v", ErrNotConvertible, err)
	}
	measures, err := measureEPUB(temp)
	if err != nil {
		return Converted{}, fmt.Errorf("%w: %v", ErrNotConvertible, err)
	}
	digest := sum.Sum(nil)

	var result Converted
	var undo func()
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
		files, err := q.ListBookFiles(ctx, f.BookID)
		if err != nil {
			return err
		}
		for _, other := range files {
			if other.Format == "epub" && bytes.Equal(other.Sha256, digest) {
				return ErrAlreadyConverted
			}
		}
		relPath, err := convertedPath(ctx, q, f.LibraryID, f.RootPath, f.RelPath)
		if err != nil {
			return err
		}
		after, err := os.Stat(temp)
		if err != nil {
			return err
		}
		newID, err := catalog.AddFileTx(ctx, tx, f.BookID, f.LibraryID, catalog.NewFile{
			RelPath: relPath, Format: "epub", Size: written.n, ModifiedAt: storedTime(after.ModTime()), SHA256: digest,
		})
		if err != nil {
			return err
		}
		if err := setMeasures(ctx, q, newID, digest, measures); err != nil {
			return err
		}
		if _, err := s.Queue.InsertTx(ctx, tx, ExtractArgs{FileID: newID}, jobs.InsertOpts{
			Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
		}); err != nil {
			return err
		}

		// Last, so that nothing after it can fail but the commit.
		full := filepath.Join(f.RootPath, filepath.FromSlash(relPath))
		if err := os.Rename(temp, full); err != nil {
			return folderError(f.LibraryID, f.RootPath, err)
		}
		placed = true
		undo = func() { _ = os.Remove(full) }
		result = Converted{FileID: newID, BookID: f.BookID, Name: path.Base(relPath), Converted: made}
		return nil
	})
	if err != nil {
		if undo != nil {
			undo()
		}
		return Converted{}, s.noteUnwritable(ctx, err)
	}
	return result, nil
}

// isKindleFormat is a format the Mobipocket reader reads.
func isKindleFormat(format string) bool {
	switch format {
	case "mobi", "azw", "azw3":
		return true
	}
	return false
}

// convertedPath is where the EPUB of a file lies: beside it under the same
// name, numbered when that is taken.
func convertedPath(ctx context.Context, q *sqlc.Queries, libraryID uuid.UUID, root, relPath string) (string, error) {
	dir, base := path.Split(relPath)
	stem := strings.TrimSuffix(base, path.Ext(base))
	for n := 1; n <= maxNameTries; n++ {
		name := stem
		if n > 1 {
			name += " (" + strconv.Itoa(n) + ")"
		}
		candidate := dir + name + ".epub"
		taken, err := q.RelPathTaken(ctx, sqlc.RelPathTakenParams{LibraryID: libraryID, RelPath: candidate})
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(candidate))); !taken && errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free name for %s as an EPUB", relPath)
}
