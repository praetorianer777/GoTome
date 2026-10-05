package ingest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/textproc"
)

// choosePrimaryTx makes the best of the book's files that have text its
// primary text file, the one search, duplicate detection and embeddings read:
// an EPUB, then a Kindle file, then a PDF; among equals the one that already
// is, or else the oldest. It returns the file chosen, or uuid.Nil, and
// whether that is a change.
func choosePrimaryTx(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID) (uuid.UUID, bool, error) {
	files, err := q.ListBookTextFiles(ctx, bookID)
	if err != nil {
		return uuid.Nil, false, err
	}
	current, err := q.GetPrimaryTextFile(ctx, bookID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if len(files) == 0 {
		if current == nil {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, true, q.SetPrimaryTextFile(ctx, sqlc.SetPrimaryTextFileParams{ID: bookID})
	}
	isCurrent := func(id uuid.UUID) bool { return current != nil && *current == id }
	best := slices.MinFunc(files, func(a, b sqlc.ListBookTextFilesRow) int {
		if r := cmp.Compare(catalog.FormatRank(b.Format), catalog.FormatRank(a.Format)); r != 0 {
			return r
		}
		switch {
		case isCurrent(a.ID):
			return -1
		case isCurrent(b.ID):
			return 1
		}
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return slices.Compare(a.ID[:], b.ID[:])
	})
	if isCurrent(best.ID) {
		return best.ID, false, nil
	}
	return best.ID, true, q.SetPrimaryTextFile(ctx, sqlc.SetPrimaryTextFileParams{ID: bookID, PrimaryTextFileID: &best.ID})
}

// textReadTx settles what the book's text is read from once one of its files
// has been read. When that is the file, its sections become the book's
// chunks; when it is another one, the file's chunks go and the other is
// queued to be chunked if it was not the primary before.
func (s *Service) textReadTx(ctx context.Context, tx pgx.Tx, bookID, libraryID, fileID uuid.UUID, format, langHint string, sections []Section) error {
	q := sqlc.New(tx)
	primary, changed, err := choosePrimaryTx(ctx, q, bookID)
	if err != nil {
		return err
	}
	if primary == fileID {
		return writeChunksTx(ctx, tx, bookID, libraryID, fileID, format, langHint, sections)
	}
	if err := q.DeleteFileChunks(ctx, fileID); err != nil {
		return err
	}
	if !changed || primary == uuid.Nil {
		return nil
	}
	_, err = s.Queue.InsertTx(ctx, tx, ChunkArgs{FileID: primary}, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	return err
}

// chunkSections turns an extractor's sections into textproc's. A PDF's
// sections are its pages; other formats' pages are worked out from the text,
// as their page counts are.
func chunkSections(format string, sections []Section) []textproc.Section {
	out := make([]textproc.Section, len(sections))
	for i, s := range sections {
		out[i] = textproc.Section{Label: s.Label, Text: s.Text}
		if format == "pdf" {
			out[i].Page = i + 1
			out[i].Label = ""
		}
	}
	return out
}

// writeChunksTx replaces the book's chunks with those of the file's text,
// adds their words to the vocabulary, and records the file as chunked.
func writeChunksTx(ctx context.Context, tx pgx.Tx, bookID, libraryID, fileID uuid.UUID, format, langHint string, sections []Section) error {
	q := sqlc.New(tx)
	if err := q.DeleteBookChunks(ctx, sqlc.DeleteBookChunksParams{BookID: bookID, FileID: fileID}); err != nil {
		return err
	}
	chunks := textproc.Split(chunkSections(format, sections), textproc.Options{CharsPerPage: charsPerPage, LangHint: langHint})
	rows := make([]sqlc.InsertBookChunksParams, len(chunks))
	for i, c := range chunks {
		r := sqlc.InsertBookChunksParams{
			BookID: bookID, LibraryID: libraryID, FileID: fileID, Position: int32(c.Position),
			Chapter: c.Chapter, CharOffset: int32(c.Offset), Lang: c.Lang,
		}
		if c.PageFrom > 0 {
			from, to := int32(c.PageFrom), int32(c.PageTo)
			r.PageFrom, r.PageTo = &from, &to
		}
		text := c.Text
		switch c.Lang {
		case "en":
			r.BodyEn = &text
		case "de":
			r.BodyDe = &text
		default:
			r.BodyXx = &text
		}
		rows[i] = r
	}
	if len(rows) > 0 {
		if _, err := q.InsertBookChunks(ctx, rows); err != nil {
			return err
		}
		if err := q.AddSearchWords(ctx, bookID); err != nil {
			return err
		}
	}
	return q.SetFileChunked(ctx, fileID)
}

// chunkTimeout ends the reading of one file that does not end.
const chunkTimeout = extractTimeout

// Chunk cuts the text of a book's primary text file into chunks again, from
// the file: for books read before chunks were kept, and for rebuilding them.
// A file that is no longer its book's primary text file is left alone.
func (s *Service) Chunk(ctx context.Context, fileID uuid.UUID) error {
	q := sqlc.New(s.pool)
	file, err := q.GetFileForChunking(ctx, fileID)
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
	got, err := safely(ctx, extract, filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrUnreadable):
		// Gone or broken since it was read; the scan and extraction say so.
		return nil
	case err != nil:
		return err
	}
	lang := got.Metadata.Language
	if file.Language != nil {
		lang = *file.Language
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		primary, err := q.GetPrimaryTextFile(ctx, file.BookID)
		if err != nil || primary == nil || *primary != fileID {
			return err
		}
		return writeChunksTx(ctx, tx, file.BookID, file.LibraryID, fileID, file.Format, lang, got.Sections)
	})
}

// EnqueueUnchunked asks for the text of every primary text file that has no
// chunks yet to be chunked. The app calls it at start; a file already
// waiting in the queue is not queued twice.
func (s *Service) EnqueueUnchunked(ctx context.Context) error {
	ids, err := sqlc.New(s.pool).ListUnchunkedTextFiles(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	if err := s.queueChunking(ctx, ids); err != nil {
		return err
	}
	s.log.Info("queued chunking of books read before", "files", len(ids))
	return nil
}

func (s *Service) queueChunking(ctx context.Context, ids []uuid.UUID) error {
	args := make([]river.JobArgs, len(ids))
	for i, id := range ids {
		args[i] = ChunkArgs{FileID: id}
	}
	_, err := s.Queue.InsertMany(ctx, args, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	if err != nil {
		return fmt.Errorf("queue chunking: %w", err)
	}
	return nil
}

// ChunkVersion names how text is cut into chunks. Raising it when
// textproc or the extractors' text changes makes the next start read the
// text of every book again (CheckChunkVersion).
const ChunkVersion = "1"

// chunkVersionName is the row of index_versions that holds ChunkVersion.
const chunkVersionName = "chunks"

// CheckChunkVersion compares ChunkVersion with the one the chunks were cut
// with. When it differs, every file is marked as still to be chunked, for
// EnqueueUnchunked to queue; the chunks there stay found until replaced.
// A database without a version is taken to have been cut with this one.
func (s *Service) CheckChunkVersion(ctx context.Context) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		stored, err := q.GetIndexVersion(ctx, chunkVersionName)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		case stored == ChunkVersion:
			return nil
		default:
			s.log.Info("the chunker changed; reading the text of every book again", "from", stored, "to", ChunkVersion)
			if err := q.ClearAllChunked(ctx); err != nil {
				return err
			}
		}
		return q.SetIndexVersion(ctx, sqlc.SetIndexVersionParams{Name: chunkVersionName, Version: ChunkVersion})
	})
}

// Rechunk reads the text of the visible books again, of a library or of
// one book when named, and returns how many files it queued. Search keeps
// finding the old chunks of a book until its new ones replace them.
func (s *Service) Rechunk(ctx context.Context, scope library.Scope, libraryID, bookID *uuid.UUID) (int, error) {
	ids, err := sqlc.New(s.pool).ClearChunked(ctx, sqlc.ClearChunkedParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID, BookID: bookID,
	})
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	// Should queueing fail, the files are still marked, and the next start
	// queues them.
	return len(ids), s.queueChunking(ctx, ids)
}

// ChunkArgs is the job that cuts one file's text into chunks.
type ChunkArgs struct {
	FileID uuid.UUID `json:"fileId"`
}

// chunkKind names the job in the queue. It is stored with every job, so it
// stays.
const chunkKind = "ingest.chunk_file"

func (ChunkArgs) Kind() string { return chunkKind }

// ChunkWorker runs ChunkArgs.
type ChunkWorker struct {
	river.WorkerDefaults[ChunkArgs]
	Service *Service
}

func (w *ChunkWorker) Timeout(*river.Job[ChunkArgs]) time.Duration { return chunkTimeout }

func (w *ChunkWorker) Work(ctx context.Context, job *river.Job[ChunkArgs]) error {
	return w.Service.Chunk(ctx, job.Args.FileID)
}
