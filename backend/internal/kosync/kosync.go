// Package kosync is the server KOReader's own progress sync talks to
// (#198): it names a document by an MD5 of samples of its file
// (PartialMD5), which is worked out for every ebook file, and reports and
// asks for a place in it, which is kept as the person's ebook progress.
package kosync

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

// ErrUnknown is a document of no book the person sees.
var ErrUnknown = errors.New("no such document")

// Locators of places KOReader sent: its XPointer in an EPUB, prefixed so
// that the web reader takes the fraction instead. A PDF's page is kept as
// the PDF reader writes it, "page:<n>".
const xpointerPrefix = "koreader:"

// clientPrefix marks a position written by KOReader on a device, followed
// by the device's ID as KOReader gives it.
const clientPrefix = "koreader:"

// Service is the progress sync.
type Service struct {
	pool    *pgxpool.Pool
	reading *reading.Service
	log     *slog.Logger
}

// NewService returns a Service on the pool, keeping places through reading.
func NewService(pool *pgxpool.Pool, reading *reading.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, reading: reading, log: log}
}

// Position is a place in a document as KOReader reports and asks for it.
type Position struct {
	Document   string
	Progress   string
	Percentage float64
	Device     string
	DeviceID   string
	Timestamp  time.Time
}

type document struct {
	file, book uuid.UUID
	format     string
	pages      *int32
}

func (s *Service) document(ctx context.Context, scope library.Scope, md5 string) (document, error) {
	row, err := sqlc.New(s.pool).GetKoreaderDocument(ctx, sqlc.GetKoreaderDocumentParams{
		Md5: strings.ToLower(md5), Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return document{}, ErrUnknown
	}
	if err != nil {
		return document{}, err
	}
	return document{file: row.ID, book: row.BookID, format: row.Format, pages: row.PageCount}, nil
}

// Push keeps the place KOReader reports, under #44's rule: a place behind
// one another client wrote is not written, and the person is offered the
// further one when KOReader next asks.
func (s *Service) Push(ctx context.Context, scope library.Scope, p Position) (time.Time, error) {
	doc, err := s.document(ctx, scope, p.Document)
	if err != nil {
		return time.Time{}, err
	}
	u := reading.Update{Progress: reading.Progress{
		Medium: reading.MediumEbook, FileID: &doc.file, Locator: xpointerPrefix + p.Progress,
		Fraction: min(max(p.Percentage, 0), 1), ClientID: clientPrefix + p.DeviceID,
	}}
	if doc.format == "pdf" {
		page, err := strconv.Atoi(p.Progress)
		if err != nil || page < 1 {
			return time.Time{}, reading.Problems{"progress": "A PDF's place is a page number."}
		}
		page32 := int32(page)
		u.Locator, u.Page = "page:"+p.Progress, &page32
	}
	saved, _, err := s.reading.Save(ctx, scope, doc.book, u)
	if err != nil {
		return time.Time{}, err
	}
	return saved.UpdatedAt, nil
}

// Pull is where the person is in the document, as KOReader goes to it,
// and false where no place can be given: none was saved, or one KOReader
// cannot go to.
func (s *Service) Pull(ctx context.Context, scope library.Scope, md5 string) (Position, bool, error) {
	doc, err := s.document(ctx, scope, md5)
	if err != nil {
		return Position{}, false, err
	}
	state, err := s.reading.Get(ctx, scope, doc.book)
	if err != nil {
		return Position{}, false, err
	}
	for _, p := range state.Progress {
		if p.Medium != reading.MediumEbook {
			continue
		}
		place, ok := placeFor(doc, p)
		if !ok {
			return Position{}, false, nil
		}
		device, deviceID := "GOtome", "gotome"
		if id, ok := strings.CutPrefix(p.ClientID, clientPrefix); ok {
			device, deviceID = "KOReader", id
		}
		return Position{
			Document: md5, Progress: place, Percentage: p.Fraction,
			Device: device, DeviceID: deviceID, Timestamp: p.UpdatedAt,
		}, true, nil
	}
	return Position{}, false, nil
}

// spineStep is the step of an EPUB CFI that names the spine item.
var spineStep = regexp.MustCompile(`^epubcfi\(/6/(\d+)`)

// placeFor translates a saved place into one KOReader goes to in the
// document: its own XPointer as it sent it, a PDF's page, or the chapter a
// CFI of the same file is in, as crengine numbers the spine's documents.
func placeFor(doc document, p reading.Progress) (string, bool) {
	sameFile := p.FileID != nil && *p.FileID == doc.file
	if doc.format == "pdf" {
		if sameFile && p.Page != nil {
			return strconv.Itoa(int(*p.Page)), true
		}
		if doc.pages == nil {
			return "", false
		}
		return strconv.Itoa(max(1, int(p.Fraction*float64(*doc.pages)+0.5))), true
	}
	if !sameFile {
		return "", false
	}
	if xpointer, ok := strings.CutPrefix(p.Locator, xpointerPrefix); ok {
		return xpointer, true
	}
	if m := spineStep.FindStringSubmatch(p.Locator); m != nil {
		step, _ := strconv.Atoi(m[1])
		return "/body/DocFragment[" + strconv.Itoa(step/2) + "]", true
	}
	return "", false
}

// HashArgs is the job that works out the KOReader MD5 of the ebook files
// that have none.
type HashArgs struct{}

const hashKind = "kosync.hash_files"

func (HashArgs) Kind() string { return hashKind }

// hashBatch is how many files a pass reads at most: each is 12 KB at most.
const hashBatch = 500

// HashWorker runs HashArgs.
type HashWorker struct {
	river.WorkerDefaults[HashArgs]
	Service *Service
}

func (w *HashWorker) Work(ctx context.Context, _ *river.Job[HashArgs]) error {
	n, err := w.Service.HashFiles(ctx)
	if err != nil {
		return err
	}
	if n == hashBatch {
		return river.JobSnooze(time.Second)
	}
	return nil
}

// HashFiles works out the KOReader MD5 of a batch of ebook files that have
// none and returns how many it took. A file that cannot be read gets an
// empty one, which names no document, until its bytes change.
func (s *Service) HashFiles(ctx context.Context) (int, error) {
	q := sqlc.New(s.pool)
	files, err := q.ListFilesToHash(ctx, hashBatch)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		sum, err := PartialMD5(filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath)))
		if err != nil {
			s.log.Warn("reading a file for KOReader's sync", "file", f.ID, "error", err)
		}
		if err := q.SetKoreaderMD5(ctx, sqlc.SetKoreaderMD5Params{ID: f.ID, KoreaderMd5: &sum}); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// Periodic is the hashing pass, every interval and at start: files new or
// changed since are hashed by it.
func Periodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, HashArgs{}, jobs.QueueDefault)
}
