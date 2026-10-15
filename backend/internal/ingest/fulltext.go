package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/textproc"
)

// Reindex hands the chunks of a book's primary text file to the full-text
// index, read again from the file (#265): the index holds no text to be
// built again from, and chunks cut before it existed know nothing of where
// they are in the file. When the file still cuts into the chunks stored,
// those keep their IDs and learn their place; otherwise they are cut again,
// as Chunk does. A file that is gone or unreadable is left as it is.
func (s *Service) Reindex(ctx context.Context, fileID uuid.UUID) error {
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
		return q.SetFileIndexed(ctx, fileID)
	}
	got, err := safely(ctx, file.Format, extract, filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrUnreadable):
		s.log.Warn("the text of a file could not be read again for the full-text index", "file", fileID, "error", err)
		return q.SetFileIndexed(ctx, fileID)
	case err != nil:
		return err
	}
	lang := got.Metadata.Language
	if file.Language != nil {
		lang = *file.Language
	}
	chunks := textproc.Split(chunkSections(file.Format, got.Sections), textproc.Options{CharsPerPage: charsPerPage, LangHint: lang})
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrary(ctx, file.LibraryID); err != nil {
			return err
		}
		primary, err := q.GetPrimaryTextFile(ctx, file.BookID)
		if err != nil || primary == nil || *primary != fileID {
			return err
		}
		stored, err := q.ListFileChunks(ctx, fileID)
		if err != nil {
			return err
		}
		if !sameChunks(stored, chunks) {
			return s.writeChunksTx(ctx, tx, file.BookID, file.LibraryID, fileID, file.Format, lang, got.Sections)
		}
		if len(chunks) > 0 {
			if err := placeChunksTx(ctx, q, stored, chunks, got.Sections); err != nil {
				return err
			}
		}
		return s.indexChunksTx(ctx, q, tx, file.BookID, file.LibraryID, fileID, chunks)
	})
}

// sameChunks says the text cuts into the chunks stored: as many, each
// where it was, in the language it had.
func sameChunks(stored []sqlc.ListFileChunksRow, chunks []textproc.Chunk) bool {
	if len(stored) != len(chunks) {
		return false
	}
	for i, c := range chunks {
		r := stored[i]
		if int(r.Position) != c.Position || int(r.CharOffset) != c.Offset || r.Lang != c.Lang {
			return false
		}
	}
	return true
}

// indexPage is how many files IndexFiles looks up at a time.
const indexPage = 100

// IndexFiles reindexes the primary text files whose chunks the full-text
// index is still to get, until the deadline; it says whether any are left.
func (s *Service) IndexFiles(ctx context.Context, deadline time.Time) (bool, error) {
	q := sqlc.New(s.pool)
	after := uuid.Nil
	for {
		ids, err := q.ListFilesToIndex(ctx, sqlc.ListFilesToIndexParams{After: after, Page: indexPage})
		if err != nil || len(ids) == 0 {
			return false, err
		}
		for _, id := range ids {
			if time.Now().After(deadline) {
				return true, nil
			}
			if err := s.Reindex(ctx, id); err != nil {
				return false, fmt.Errorf("index file %s: %w", id, err)
			}
			after = id
		}
	}
}

// EnqueueIndexing queues the job that hands the index what it lacks; it
// finds nothing to do when the index has everything.
func (s *Service) EnqueueIndexing(ctx context.Context) error {
	_, err := s.Queue.InsertMany(ctx, []river.JobArgs{IndexFilesArgs{}}, jobs.InsertOpts{
		Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
	})
	return err
}

// IndexFilesArgs is the job that reads files again for the full-text index.
type IndexFilesArgs struct{}

// indexFilesKind names the job in the queue. It is stored with every job,
// so it stays.
const indexFilesKind = "ingest.index_files"

func (IndexFilesArgs) Kind() string { return indexFilesKind }

// IndexFilesWorker runs IndexFilesArgs, for passBudget at a time.
type IndexFilesWorker struct {
	river.WorkerDefaults[IndexFilesArgs]
	Service *Service
}

func (w *IndexFilesWorker) Timeout(*river.Job[IndexFilesArgs]) time.Duration {
	return passBudget + extractTimeout
}

func (w *IndexFilesWorker) Work(ctx context.Context, _ *river.Job[IndexFilesArgs]) error {
	more, err := w.Service.IndexFiles(ctx, time.Now().Add(passBudget))
	if err != nil {
		return err
	}
	if more {
		return river.JobSnooze(time.Second)
	}
	return nil
}

// PassageRef is a chunk to read again from its file.
type PassageRef struct {
	ChunkID  int64
	FileID   uuid.UUID
	Format   string
	Path     string
	Position int
	// Sources, From and To are where the chunk is in the file, as
	// book_chunks records it; no sources reads the file whole.
	Sources  []string
	From, To int
}

// passageTimeout bounds the reading of one file's passages.
const passageTimeout = 10 * time.Second

// ReadPassages reads the chunks' text again from their files: an EPUB's
// documents the chunk spans, a PDF's pages, any other file whole. A chunk
// whose file cannot be read is left out of the answer, which a caller shows
// without its text.
func ReadPassages(ctx context.Context, refs []PassageRef) map[int64]string {
	out := map[int64]string{}
	byFile := map[uuid.UUID][]PassageRef{}
	var order []uuid.UUID
	for _, r := range refs {
		if _, ok := byFile[r.FileID]; !ok {
			order = append(order, r.FileID)
		}
		byFile[r.FileID] = append(byFile[r.FileID], r)
	}
	for _, id := range order {
		fileCtx, cancel := context.WithTimeout(ctx, passageTimeout)
		readFilePassages(fileCtx, byFile[id], out)
		cancel()
	}
	return out
}

func readFilePassages(ctx context.Context, refs []PassageRef, out map[int64]string) {
	var whole []textproc.Chunk
	wholeRead := false
	for _, r := range refs {
		if ctx.Err() != nil {
			return
		}
		if len(r.Sources) > 0 {
			if texts, err := readSources(ctx, r); err == nil {
				out[r.ChunkID] = textproc.Slice(texts, r.From, r.To)
				continue
			}
		}
		if !wholeRead {
			wholeRead = true
			extract, ok := extractors[r.Format]
			if !ok {
				return
			}
			got, err := safely(ctx, r.Format, extract, r.Path)
			if err != nil {
				return
			}
			whole = textproc.Split(chunkSections(r.Format, got.Sections), textproc.Options{CharsPerPage: charsPerPage})
		}
		if r.Position < len(whole) {
			out[r.ChunkID] = whole[r.Position].Text
		}
	}
}

// readSources reads the sections a chunk spans, as extraction read them.
func readSources(ctx context.Context, r PassageRef) ([]string, error) {
	switch r.Format {
	case "epub":
		f, size, err := openFile(r.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return epub.ReadDocuments(f, size, r.Sources)
	case "pdf":
		first, err1 := strconv.Atoi(r.Sources[0])
		last, err2 := strconv.Atoi(r.Sources[len(r.Sources)-1])
		if err1 != nil || err2 != nil || first < 1 || last < first {
			return nil, fmt.Errorf("pages %v", r.Sources)
		}
		text, err := runPoppler(ctx, pdftotext, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last), "-enc", "UTF-8", "-eol", "unix", r.Path, "-")
		if err != nil {
			return nil, err
		}
		pages := strings.Split(string(text), "\f")
		if len(pages) < last-first+1 {
			return nil, fmt.Errorf("%d pages read of %d", len(pages), last-first+1)
		}
		pages = pages[:last-first+1]
		for i := range pages {
			pages[i] = strings.TrimSpace(pages[i])
		}
		return pages, nil
	}
	return nil, errors.New("the format is read whole")
}

// FileTexts is the text of a primary text file cut into its chunks again,
// read from the file, for what needs all of it, such as signing it for
// overlap again. Nil for a file that is gone, unreadable, or no longer its
// book's text.
func (s *Service) FileTexts(ctx context.Context, fileID uuid.UUID) ([]string, error) {
	file, err := sqlc.New(s.pool).GetFileForChunking(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	extract, ok := extractors[file.Format]
	if !ok || file.PrimaryTextFileID == nil || *file.PrimaryTextFileID != fileID {
		return nil, nil
	}
	got, err := safely(ctx, file.Format, extract, filepath.Join(file.RootPath, filepath.FromSlash(file.RelPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrUnreadable):
		return nil, nil
	case err != nil:
		return nil, err
	}
	lang := got.Metadata.Language
	if file.Language != nil {
		lang = *file.Language
	}
	chunks := textproc.Split(chunkSections(file.Format, got.Sections), textproc.Options{CharsPerPage: charsPerPage, LangHint: lang})
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	return texts, nil
}

// Passages reads the file's chunks at the positions again from the file, in
// the order of their positions; those that cannot be read are left out.
func (s *Service) Passages(ctx context.Context, fileID uuid.UUID, positions []int32) ([]string, error) {
	places, err := sqlc.New(s.pool).ListChunkPlaces(ctx, sqlc.ListChunkPlacesParams{FileID: fileID, Positions: positions})
	if err != nil || len(places) == 0 {
		return nil, err
	}
	refs := make([]PassageRef, len(places))
	for i, p := range places {
		refs[i] = PassageRef{
			ChunkID: p.ID, FileID: fileID, Format: p.Format,
			Path:     filepath.Join(p.RootPath, filepath.FromSlash(p.RelPath)),
			Position: int(p.Position), Sources: p.Sources, From: int(p.SourceFrom), To: int(p.SourceTo),
		}
	}
	texts := ReadPassages(ctx, refs)
	var out []string
	for _, r := range refs {
		if t, ok := texts[r.ChunkID]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
