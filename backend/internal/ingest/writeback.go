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
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

const (
	writeBackKind     = "ingest.write_metadata"
	writeBackAttempts = 5
	writeBackTimeout  = 5 * time.Minute
)

// ErrNotWritten is a file whose metadata cannot be written, and will not be
// however often it is tried.
var ErrNotWritten = errors.New("the metadata cannot be written into this file")

// errWrittenMeanwhile is a file another job wrote while this one did: the
// next attempt starts from what that one left.
var errWrittenMeanwhile = errors.New("the file was written by another job meanwhile")

// Edit changes how a book is described, as catalog.EditTx does, and has the
// change written into the book's EPUBs in libraries GOtome may change, so
// that it travels with the files.
func (s *Service) Edit(ctx context.Context, scope library.Scope, id uuid.UUID, e catalog.Edit) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := catalog.EditTx(ctx, tx, scope, id, e); err != nil {
			return err
		}
		if !e.ChangesValues() {
			return nil
		}
		files, err := sqlc.New(tx).ListFilesToWriteBack(ctx, id)
		if err != nil {
			return err
		}
		for _, file := range files {
			// Not unique: a job already running may have read the book
			// before this edit, and the one after it must write it again.
			_, err := s.Queue.InsertTx(ctx, tx, WriteBackArgs{FileID: file}, jobs.InsertOpts{
				Queue: jobs.QueueMetadata, MaxAttempts: writeBackAttempts,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// catalogRoles are the MARC relator codes of the roles the catalogue knows.
var catalogRoles = func() map[string]string {
	out := map[string]string{}
	for code, role := range marcRoles {
		out[role] = code
	}
	return out
}()

// WriteBack writes what the catalogue says about a file's book into the
// file. The file is written next to itself under a name the scan passes by,
// read back to check that its content is the same, and only then put in the
// original's place, by a rename in the transaction that records its new
// hash. original_sha256 keeps what arrived. A file that changed on disk
// since it was hashed is left for the scan, which will read it again.
func (s *Service) WriteBack(ctx context.Context, fileID uuid.UUID) error {
	q := sqlc.New(s.pool)
	file, err := q.GetFileForWriteBack(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if file.Format != "epub" || !file.Writable || file.Drm || file.ExtractState != "done" ||
		file.MissingAt != nil || file.TrashedAt != nil || file.Sha256 == nil {
		return nil
	}
	book, err := catalog.NewService(s.pool).Get(ctx, library.Scope{SeesAll: true}, file.BookID)
	if errors.Is(err, catalog.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	update, err := s.updateFor(book, fileID)
	if err != nil {
		return err
	}

	path := filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath))
	src, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(src, 0, info.Size())); err != nil {
		return err
	}
	if !bytes.Equal(h.Sum(nil), file.Sha256) {
		return nil
	}

	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".gotome-write")
	// A part left by a crash is of no use to anyone.
	_ = os.Remove(temp)
	out, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("write %s: %w", file.RelPath, err)
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
	err = epub.Rewrite(src, info.Size(), written, update)
	switch {
	case errors.Is(err, epub.ErrNotWritable), errors.Is(err, epub.ErrNotEPUB), errors.Is(err, epub.ErrTooLarge):
		return fmt.Errorf("%w: %v", ErrNotWritten, err)
	case err != nil:
		return fmt.Errorf("write %s: %w", file.RelPath, err)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := sameContent(temp, file.ContentSha256); err != nil {
		return fmt.Errorf("%w: %v", ErrNotWritten, err)
	}

	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		current, err := q.LockFileHash(ctx, fileID)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, file.Sha256) {
			return errWrittenMeanwhile
		}
		if err := os.Rename(temp, path); err != nil {
			return err
		}
		placed = true
		after, err := os.Stat(path)
		if err != nil {
			return err
		}
		// Should the commit fail, the file on disk is the new one and its
		// row still says the old: the next scan finds it changed and reads
		// it again, which is what it is.
		return q.SetFileWritten(ctx, sqlc.SetFileWrittenParams{
			ID: fileID, Sha256: sum.Sum(nil), SizeBytes: written.n, ModifiedAt: storedTime(after.ModTime()),
		})
	})
}

// sameContent checks that a written file reads as an EPUB with the content
// it had before, when that is known.
func sameContent(path string, want []byte) error {
	f, size, err := openFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	book, err := epub.Parse(f, size)
	if err != nil {
		return fmt.Errorf("the written file does not read: %w", err)
	}
	if want != nil && !bytes.Equal(book.ContentHash, want) {
		return errors.New("the written file's content differs from the original's")
	}
	return nil
}

// updateFor is what is written into one file of the book: everything the
// catalogue says, with the identifiers of the book and of this file. The
// cover is written only when a person chose or removed it; otherwise the
// file's own stays.
func (s *Service) updateFor(book catalog.Book, fileID uuid.UUID) (epub.Update, error) {
	m := epub.Metadata{
		Title: book.Title, Subtitle: book.Subtitle, Language: book.Language, Publisher: book.Publisher,
		Published: book.Published(), Description: book.Description, Subjects: book.Tags,
		Series: book.Series, SeriesIndex: book.SeriesIndex,
	}
	for _, c := range book.Contributors {
		if code, ok := catalogRoles[c.Role]; ok {
			m.Contributors = append(m.Contributors, epub.Contributor{Name: c.Name, FileAs: c.SortName, Role: code})
		}
	}
	for _, ident := range book.Identifiers {
		if ident.FileID == nil || *ident.FileID == fileID {
			m.Identifiers = append(m.Identifiers, epub.Identifier{Scheme: ident.Type, Value: ident.Value})
		}
	}
	u := epub.Update{Metadata: m, Modified: time.Now().UTC()}
	if book.Sources[catalog.FieldCover] != catalog.SourceManual {
		return u, nil
	}
	if book.CoverKey == "" {
		u.RemoveCover = true
		return u, nil
	}
	path, err := s.covers.Path(book.CoverKey, covers.Large)
	if err != nil {
		return epub.Update{}, fmt.Errorf("the cover: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return epub.Update{}, err
	}
	u.Cover = &epub.Cover{MediaType: "image/jpeg", Data: data}
	return u, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// WriteBackArgs is the job that writes a book's metadata into one file.
type WriteBackArgs struct {
	FileID uuid.UUID `json:"fileId"`
}

func (WriteBackArgs) Kind() string { return writeBackKind }

// WriteBackWorker runs WriteBackArgs.
type WriteBackWorker struct {
	river.WorkerDefaults[WriteBackArgs]
	Service *Service
}

func (w *WriteBackWorker) Timeout(*river.Job[WriteBackArgs]) time.Duration { return writeBackTimeout }

func (w *WriteBackWorker) Work(ctx context.Context, job *river.Job[WriteBackArgs]) error {
	err := w.Service.WriteBack(ctx, job.Args.FileID)
	if errors.Is(err, ErrNotWritten) {
		w.Service.log.Warn("metadata not written", "file", job.Args.FileID, "error", err)
		return river.JobCancel(err)
	}
	return err
}
