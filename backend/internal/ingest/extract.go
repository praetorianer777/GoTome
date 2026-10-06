package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

const (
	// extractTimeout ends the reading of one file that does not end.
	extractTimeout = 10 * time.Minute
	// A file that cannot be parsed is recorded as failed and not tried again;
	// these attempts are for the disk or the database failing underneath.
	extractAttempts = 3
	maxErrorLen     = 500
)

// Section is a stretch of a file's text: an EPUB's chapter, a PDF's page.
type Section struct {
	// Label names the section for a person: a chapter's title, a page's
	// number. It may be empty.
	Label string
	Text  string
}

// Extracted is what reading one file yields.
type Extracted struct {
	Metadata catalog.FileMetadata
	// Sections are the text in reading order, which the primary text file's
	// book keeps as chunks.
	Sections []Section
	// Cover is the cover image as the file holds it, or nil.
	Cover []byte
	// ContentSHA256 is the hash of the content without the metadata, for
	// formats that can tell the two apart.
	ContentSHA256 []byte
	DRM           bool
	HasText       bool
	// Pages is nil when the format has none and none can be worked out.
	Pages          *int32
	PagesEstimated bool
	// DurationMS, Track, Disc and Chapters are an audio file's.
	DurationMS  *int64
	Track, Disc *int32
	Chapters    []Chapter
}

// ErrUnreadable is what an Extractor wraps when the file is not what its
// name says, or is broken: a fact about the file, recorded with it. Any other
// error is taken to be passing, and the file is tried again.
var ErrUnreadable = errors.New("the file cannot be read as its format")

// Extractor reads one file of a format.
type Extractor func(ctx context.Context, path string) (Extracted, error)

// extractors are the formats there is a reader for. Files of other formats
// wait as pending until there is one.
var extractors = map[string]Extractor{
	"epub": extractEPUB,
	"pdf":  extractPDF,
	"mobi": extractMOBI,
	"azw3": extractMOBI,
	"azw":  extractMOBI,
	"m4b":  extractAudio,
	"m4a":  extractAudio,
	"mp3":  extractAudio,
	"flac": extractAudio,
	"ogg":  extractAudio,
	"opus": extractAudio,
}

// EnqueuePending asks for every file of the library that is still to be read
// to be read. A file already waiting in the queue is not queued twice.
func (s *Service) EnqueuePending(ctx context.Context, libraryID uuid.UUID) error {
	ids, err := sqlc.New(s.pool).ListPendingExtractions(ctx, sqlc.ListPendingExtractionsParams{
		LibraryID: libraryID, Formats: readableFormats(),
	})
	if err != nil || len(ids) == 0 {
		return err
	}
	args := make([]river.JobArgs, len(ids))
	for i, id := range ids {
		args[i] = ExtractArgs{FileID: id}
	}
	_, err = s.Queue.InsertMany(ctx, args, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	return err
}

// readableFormats are the formats a reader is registered for.
func readableFormats() []string { return slices.Sorted(maps.Keys(extractors)) }

// Extract reads one file and describes its book with what it says. A file
// that cannot be parsed is recorded as failed, which is not an error: the
// next file is none the worse for it.
func (s *Service) Extract(ctx context.Context, fileID uuid.UUID) error {
	q := sqlc.New(s.pool)
	file, err := q.GetFileForExtraction(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	extract, ok := extractors[file.Format]
	if !ok || file.MissingAt != nil || file.TrashedAt != nil || file.Sha256 == nil {
		return nil
	}
	path := filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath))

	got, err := safely(ctx, extract, path)
	got.Metadata.Format = file.Format
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Gone since the scan; the next scan marks it missing.
		return nil
	case errors.Is(err, ErrUnreadable):
		s.log.Warn("extraction failed", "file", file.RelPath, "error", err)
		message := err.Error()
		if len(message) > maxErrorLen {
			// Cut between bytes, the end may be half a character, which the
			// database would refuse.
			message = strings.ToValidUTF8(message[:maxErrorLen], "")
		}
		return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			qtx := sqlc.New(tx)
			if err := qtx.SetFileExtractFailed(ctx, sqlc.SetFileExtractFailedParams{ID: fileID, Sha256: file.Sha256, Error: &message}); err != nil {
				return err
			}
			book, err := qtx.GetBookTitleAndLibrary(ctx, file.BookID)
			if err != nil {
				return err
			}
			return s.Events.QueueTx(ctx, tx, notify.Event{
				Kind: notify.KindFilesUnreadable, Libraries: []uuid.UUID{file.LibraryID}, BookID: &file.BookID,
				Data: map[string]any{"file": filepath.Base(file.RelPath), "title": book.Title},
				Link: "/books/" + file.BookID.String(),
			})
		})
	case err != nil:
		return err
	}

	if got.Cover != nil {
		key, err := s.covers.Put(got.Cover)
		switch {
		case errors.Is(err, covers.ErrNotAnImage):
			// A book with a broken cover is a book without a cover.
			s.log.Debug("cover not usable", "file", file.RelPath, "error", err)
		case err != nil:
			return fmt.Errorf("store the cover: %w", err)
		default:
			got.Metadata.CoverKey = key
		}
	}

	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		n, err := q.SetFileExtracted(ctx, sqlc.SetFileExtractedParams{
			ID: fileID, Sha256: file.Sha256, ContentSha256: got.ContentSHA256, Drm: got.DRM,
			HasText: &got.HasText, PageCount: got.Pages, PagesEstimated: got.PagesEstimated,
			DurationMs: got.DurationMS, TrackNumber: got.Track, DiscNumber: got.Disc,
		})
		if err != nil || n == 0 {
			return err
		}
		// A file a placeholder waited for joins it first, so that what the
		// file says describes the placeholder.
		bookID, err := catalog.FulfilPlaceholderTx(ctx, tx, file.BookID, got.Metadata)
		if err != nil {
			return err
		}
		if bookID != file.BookID {
			if err := s.wishFulfilledTx(ctx, tx, bookID, file.LibraryID); err != nil {
				return err
			}
		}
		if err := catalog.ApplyFileMetadataTx(ctx, tx, fileID, got.Metadata); err != nil {
			return err
		}
		if s.OnExtracted != nil {
			if err := s.OnExtracted(ctx, tx, bookID); err != nil {
				return err
			}
		}
		if err := q.DeleteFileChapters(ctx, fileID); err != nil {
			return err
		}
		for i, c := range got.Chapters {
			err := q.AddFileChapter(ctx, sqlc.AddFileChapterParams{
				FileID: fileID, Position: int32(i), Title: c.Title, StartMs: c.StartMS, EndMs: c.EndMS,
			})
			if err != nil {
				return err
			}
		}
		// The book is locked by now, so the parts of one audiobook are put in
		// order by one file's job at a time.
		if err := orderParts(ctx, q, bookID); err != nil {
			return err
		}
		return s.textReadTx(ctx, tx, bookID, file.LibraryID, fileID, file.Format, got.Metadata.Language, got.Sections)
	})
}

// orderParts puts the parts of a book's audiobook in the order of their
// disc and track tags, once every part has a track tag. Until then, and in a
// book whose parts carry none, the order the scan read off the names stays.
func orderParts(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID) error {
	parts, err := q.ListBookParts(ctx, bookID)
	if err != nil || len(parts) < 2 {
		return err
	}
	for _, p := range parts {
		if p.TrackNumber == nil {
			return nil
		}
	}
	disc := func(p sqlc.ListBookPartsRow) int32 {
		if p.DiscNumber == nil {
			return 0
		}
		return *p.DiscNumber
	}
	slices.SortFunc(parts, func(a, b sqlc.ListBookPartsRow) int {
		if d := disc(a) - disc(b); d != 0 {
			return int(d)
		}
		if t := *a.TrackNumber - *b.TrackNumber; t != 0 {
			return int(t)
		}
		return compareNatural(a.RelPath, b.RelPath)
	})
	for i, p := range parts {
		if *p.PartIndex == int32(i) {
			continue
		}
		index := int32(i)
		if err := q.SetFilePartIndex(ctx, sqlc.SetFilePartIndexParams{ID: p.ID, PartIndex: &index}); err != nil {
			return err
		}
	}
	return nil
}

// safely runs the extractor and turns a panic into a failure of that one
// file. The parsers read files from anywhere; one that trips a parser must
// not be tried again and again by the queue.
func safely(ctx context.Context, extract Extractor, path string) (got Extracted, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: the reader crashed: %v", ErrUnreadable, r)
		}
	}()
	return extract(ctx, path)
}

// ExtractArgs is the job that reads one file.
type ExtractArgs struct {
	FileID uuid.UUID `json:"fileId"`
}

// extractKind names the job in the queue. It is stored with every job, so it
// stays.
const extractKind = "ingest.extract_file"

func (ExtractArgs) Kind() string { return extractKind }

// ExtractWorker runs ExtractArgs.
type ExtractWorker struct {
	river.WorkerDefaults[ExtractArgs]
	Service *Service
}

func (w *ExtractWorker) Timeout(*river.Job[ExtractArgs]) time.Duration { return extractTimeout }

func (w *ExtractWorker) Work(ctx context.Context, job *river.Job[ExtractArgs]) error {
	return w.Service.Extract(ctx, job.Args.FileID)
}

// openFile opens a file for an extractor and says how large it is.
func openFile(path string) (*os.File, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// wishFulfilledTx tells those who wished for the book that it arrived.
func (s *Service) wishFulfilledTx(ctx context.Context, tx pgx.Tx, bookID, libraryID uuid.UUID) error {
	q := sqlc.New(tx)
	wishers, err := q.ListWishers(ctx, bookID)
	if err != nil || len(wishers) == 0 {
		return err
	}
	book, err := q.GetBookTitleAndLibrary(ctx, bookID)
	if err != nil {
		return err
	}
	return s.Events.QueueTx(ctx, tx, notify.Event{
		Kind: notify.KindWishFulfilled, Libraries: []uuid.UUID{libraryID}, BookID: &bookID,
		Data: map[string]any{"title": book.Title}, Link: "/books/" + bookID.String(), Recipients: wishers,
	})
}
