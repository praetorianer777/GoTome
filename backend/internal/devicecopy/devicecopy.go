// Package devicecopy makes the copy of an EPUB that a device is sent in
// place of the library's file (#501): the file as epub.Fix leaves it, so
// that a book with e-reader findings reaches a device fixed even from a
// library GOtome cannot write to. The library's file is only read.
//
// Copies are a cache under GOTOME_DATA_DIR/device-copies, named by the
// SHA-256 of the file they were made from, their kind and its version, and
// described by device_copies rows. Fixed copies are made by a job when a
// book is queued for a device, never while a device downloads: a download
// that finds no copy ready sends the file itself. A KEPUB (#411), which a
// Kobo reader is sent where its owner chose so, is made when the sync tells
// the reader of the book, and again by the download that finds it gone.
package devicecopy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
)

// The most the copies keep: bytes on disk, and how long one stays unused.
// Past them the least recently used goes; its hashes stay, so that a
// device that holds it is still known.
const (
	MaxBytes   = 4 << 30
	MaxAgeDays = 90
)

// The kinds of copy, and what each is named by.
const (
	KindFix   = "fix"
	KindKepub = "kepub"
)

// Dir is where the copies lie.
func Dir(dataDir string) string { return filepath.Join(dataDir, "device-copies") }

// Queue is the job queue the service asks for copies with.
type Queue interface {
	InsertMany(ctx context.Context, args []river.JobArgs, opts jobs.InsertOpts) (int, error)
}

// Service makes and finds the copies.
type Service struct {
	pool *pgxpool.Pool
	dir  string
	log  *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
	// Enabled says whether devices are sent fixed copies (the setting
	// devices.fixOnSend); unset, they are.
	Enabled func(context.Context) (bool, error)
	// Version is the version of the copies made and sent: epub.FixVersion,
	// which a test raises.
	Version int16
	// KepubVersion is the version of the KEPUBs made: epub.KepubVersion.
	KepubVersion int16

	// converting keeps one conversion of a file at a time.
	converting sync.Mutex
}

// NewService returns a Service keeping its copies under dataDir.
func NewService(pool *pgxpool.Pool, dataDir string, log *slog.Logger) *Service {
	return &Service{pool: pool, dir: Dir(dataDir), log: log, Version: epub.FixVersion, KepubVersion: epub.KepubVersion}
}

func (s *Service) enabled(ctx context.Context) bool {
	if s.Enabled == nil {
		return true
	}
	on, err := s.Enabled(ctx)
	if err != nil {
		s.log.Warn("reading the setting for fixed copies", "error", err)
		return false
	}
	return on
}

func (s *Service) pathOf(source []byte, kind string, version int16) string {
	if kind == KindKepub {
		return filepath.Join(s.dir, fmt.Sprintf("%s-kepub-v%d.kepub.epub", hex.EncodeToString(source), version))
	}
	return filepath.Join(s.dir, fmt.Sprintf("%s-v%d.epub", hex.EncodeToString(source), version))
}

// Copy is a copy that is ready.
type Copy struct {
	Path   string
	SHA256 []byte
	Size   int64
}

// Ready is the copy made from a file of this hash, if there is one on disk
// and fixed copies are on. A copy that was removed from under the cache is
// noted, so that the next request makes it again.
func (s *Service) Ready(ctx context.Context, source []byte) (*Copy, error) {
	if len(source) == 0 || !s.enabled(ctx) {
		return nil, nil
	}
	return s.ready(ctx, source, KindFix, s.Version)
}

// KepubReady is the KEPUB made from a file of this hash, if there is one on
// disk.
func (s *Service) KepubReady(ctx context.Context, source []byte) (*Copy, error) {
	if len(source) == 0 {
		return nil, nil
	}
	return s.ready(ctx, source, KindKepub, s.KepubVersion)
}

func (s *Service) ready(ctx context.Context, source []byte, kind string, version int16) (*Copy, error) {
	q := sqlc.New(s.pool)
	row, err := q.GetDeviceCopy(ctx, sqlc.GetDeviceCopyParams{SourceSha256: source, Kind: kind, FixVersion: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.State != "ready" {
		return nil, nil
	}
	path := s.pathOf(source, kind, version)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != *row.SizeBytes {
		if err := q.EvictDeviceCopy(ctx, sqlc.EvictDeviceCopyParams{SourceSha256: source, Kind: kind, FixVersion: version}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if time.Since(row.LastUsedAt) > time.Hour {
		_ = q.TouchDeviceCopy(ctx, sqlc.TouchDeviceCopyParams{SourceSha256: source, Kind: kind, FixVersion: version})
	}
	return &Copy{Path: path, SHA256: row.Sha256, Size: info.Size()}, nil
}

// SizeOf is the size of the ready copy of a file, for a device that is told
// of the download before it makes it.
func (s *Service) SizeOf(ctx context.Context, fileID uuid.UUID) (int64, bool) {
	if !s.enabled(ctx) {
		return 0, false
	}
	return s.sizeOf(ctx, fileID, KindFix, s.Version)
}

// KepubSizeOf is the size of the ready KEPUB of a file.
func (s *Service) KepubSizeOf(ctx context.Context, fileID uuid.UUID) (int64, bool) {
	return s.sizeOf(ctx, fileID, KindKepub, s.KepubVersion)
}

func (s *Service) sizeOf(ctx context.Context, fileID uuid.UUID, kind string, version int16) (int64, bool) {
	row, err := sqlc.New(s.pool).GetReadyCopyOfFile(ctx, sqlc.GetReadyCopyOfFileParams{ID: fileID, Kind: kind, FixVersion: version})
	if err != nil || row.SizeBytes == nil {
		return 0, false
	}
	return *row.SizeBytes, true
}

// Request queues the making of a copy of each EPUB with findings among the
// books' files that has none yet. It is safe to ask again for the same
// books, and costs one query when there is nothing to make.
func (s *Service) Request(ctx context.Context, bookIDs []uuid.UUID) error {
	if len(bookIDs) == 0 || s.Queue == nil || !s.enabled(ctx) {
		return nil
	}
	files, err := sqlc.New(s.pool).ListFilesToCopy(ctx, sqlc.ListFilesToCopyParams{BookIds: bookIDs, FixVersion: s.Version})
	if err != nil || len(files) == 0 {
		return err
	}
	args := make([]river.JobArgs, len(files))
	for i, f := range files {
		args[i] = MakeArgs{FileID: f.ID}
	}
	_, err = s.Queue.InsertMany(ctx, args, jobs.InsertOpts{Queue: jobs.QueueExtract, Unique: true, MaxAttempts: 3})
	return err
}

// fixesFor are the fixes a file's findings call for, as the bulk action
// "Everything found" has them: fonts are cut down, not removed.
func fixesFor(findings []string) []string {
	var out []string
	for _, f := range findings {
		if f == epub.LargeFonts {
			f = epub.SubsetFonts
		}
		if slices.Contains(epub.Fixes, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Make makes the copy of one file, unless there is one or the file needs
// none. A file Fix finds nothing to change in, or cannot read, is noted, and
// the note is not an error: the file is sent as it is.
func (s *Service) Make(ctx context.Context, fileID uuid.UUID) error {
	q := sqlc.New(s.pool)
	f, err := q.GetFileToCopy(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	fixes := fixesFor(f.EreaderFindings)
	if f.Format != "epub" || f.Drm || f.Sha256 == nil || f.TrashedAt != nil || f.MissingAt != nil || len(fixes) == 0 {
		return nil
	}
	if row, err := q.GetDeviceCopy(ctx, sqlc.GetDeviceCopyParams{SourceSha256: f.Sha256, Kind: KindFix, FixVersion: s.Version}); err == nil {
		if row.State == "unfixable" {
			return nil
		}
		if _, err := os.Stat(s.pathOf(f.Sha256, KindFix, s.Version)); row.State == "ready" && err == nil {
			return nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	src, info, err := openSource(f.RootPath, f.RelPath, f.Sha256)
	if src == nil || err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(s.dir, ".making-*")
	if err != nil {
		return err
	}
	temp := out.Name()
	defer os.Remove(temp)
	sum := sha256.New()
	// The time is the source's, so that the same bytes are made again if
	// the copy is evicted: a device that holds it keeps its hashes.
	fixed, err := epub.Fix(src, info.Size(), io.MultiWriter(out, sum), fixes, info.ModTime().UTC())
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	unfixable := errors.Is(err, epub.ErrNotWritable) || errors.Is(err, epub.ErrNotEPUB) || errors.Is(err, epub.ErrTooLarge)
	switch {
	case unfixable || (err == nil && !fixed.Any()):
		if err != nil {
			s.log.Info("a file is sent as it is: it cannot be fixed", "file", fileID, "error", err)
		}
		return q.SetDeviceCopy(ctx, sqlc.SetDeviceCopyParams{SourceSha256: f.Sha256, Kind: KindFix, FixVersion: s.Version, State: "unfixable"})
	case err != nil:
		return fmt.Errorf("fix %s for a device: %w", f.RelPath, err)
	}
	if err := reads(temp); err != nil {
		s.log.Warn("a fixed copy does not read; the file is sent as it is", "file", fileID, "error", err)
		return q.SetDeviceCopy(ctx, sqlc.SetDeviceCopyParams{SourceSha256: f.Sha256, Kind: KindFix, FixVersion: s.Version, State: "unfixable"})
	}
	md5, err := kosync.PartialMD5(temp)
	if err != nil {
		return err
	}
	size, err := os.Stat(temp)
	if err != nil {
		return err
	}
	if err := os.Chmod(temp, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, s.pathOf(f.Sha256, KindFix, s.Version)); err != nil {
		return err
	}
	n := size.Size()
	return q.SetDeviceCopy(ctx, sqlc.SetDeviceCopyParams{
		SourceSha256: f.Sha256, Kind: KindFix, FixVersion: s.Version, State: "ready",
		Sha256: sum.Sum(nil), SizeBytes: &n, KoreaderMd5: &md5,
	})
}

// openSource opens a library file for copying, if it lies there with the
// hash the catalogue has: when the scan has yet to read the file again, a
// copy of other bytes would be kept under the wrong hash. Neither the file
// nor an error is returned for a file that is gone or other.
func openSource(root, rel string, sum []byte) (*os.File, fs.FileInfo, error) {
	src, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	info, err := src.Stat()
	if err != nil {
		src.Close()
		return nil, nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(src, 0, info.Size())); err != nil {
		src.Close()
		return nil, nil, err
	}
	if string(h.Sum(nil)) != string(sum) {
		src.Close()
		return nil, nil, nil
	}
	return src, info, nil
}

// Kepub is the KEPUB of a file for a Kobo reader, made now if there is none
// (one conversion at a time). It is nil, and the reader is sent the EPUB,
// for a file that is no EPUB, is gone or encrypted, or that cannot be
// converted; the last is noted, so that it is not tried again.
func (s *Service) Kepub(ctx context.Context, fileID uuid.UUID) (*Copy, error) {
	q := sqlc.New(s.pool)
	f, err := q.GetFileToCopy(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if f.Format != "epub" || f.Drm || f.Sha256 == nil || f.TrashedAt != nil || f.MissingAt != nil {
		return nil, nil
	}
	if c, err := s.KepubReady(ctx, f.Sha256); c != nil || err != nil {
		return c, err
	}
	s.converting.Lock()
	defer s.converting.Unlock()
	if c, err := s.KepubReady(ctx, f.Sha256); c != nil || err != nil {
		return c, err
	}
	if row, err := q.GetDeviceCopy(ctx, sqlc.GetDeviceCopyParams{SourceSha256: f.Sha256, Kind: KindKepub, FixVersion: s.KepubVersion}); err == nil {
		if row.State == "unfixable" {
			return nil, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	src, info, err := openSource(f.RootPath, f.RelPath, f.Sha256)
	if src == nil || err != nil {
		return nil, err
	}
	defer src.Close()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	out, err := os.CreateTemp(s.dir, ".making-*")
	if err != nil {
		return nil, err
	}
	temp := out.Name()
	defer os.Remove(temp)
	sum := sha256.New()
	// The time is the source's, as for a fixed copy, so that the same
	// bytes come again if the copy is evicted.
	_, err = epub.Kepubify(src, info.Size(), io.MultiWriter(out, sum), info.ModTime().UTC())
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	note := func() (*Copy, error) {
		return nil, q.SetDeviceCopy(ctx, sqlc.SetDeviceCopyParams{SourceSha256: f.Sha256, Kind: KindKepub, FixVersion: s.KepubVersion, State: "unfixable"})
	}
	if errors.Is(err, epub.ErrNotWritable) || errors.Is(err, epub.ErrNotEPUB) || errors.Is(err, epub.ErrTooLarge) || errors.Is(err, epub.ErrUnsafe) {
		s.log.Info("a file is sent as an EPUB: it cannot be made a KEPUB", "file", fileID, "error", err)
		return note()
	}
	if err != nil {
		return nil, fmt.Errorf("make %s a KEPUB: %w", f.RelPath, err)
	}
	if err := reads(temp); err != nil {
		s.log.Warn("a KEPUB does not read; the file is sent as an EPUB", "file", fileID, "error", err)
		return note()
	}
	md5, err := kosync.PartialMD5(temp)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(temp)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(temp, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(temp, s.pathOf(f.Sha256, KindKepub, s.KepubVersion)); err != nil {
		return nil, err
	}
	n := st.Size()
	err = q.SetDeviceCopy(ctx, sqlc.SetDeviceCopyParams{
		SourceSha256: f.Sha256, Kind: KindKepub, FixVersion: s.KepubVersion, State: "ready",
		Sha256: sum.Sum(nil), SizeBytes: &n, KoreaderMd5: &md5,
	})
	if err != nil {
		return nil, err
	}
	return &Copy{Path: s.pathOf(f.Sha256, KindKepub, s.KepubVersion), SHA256: sum.Sum(nil), Size: n}, nil
}

// reads checks that the written file opens as an EPUB.
func reads(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := epub.Parse(f, info.Size()); err != nil {
		return err
	}
	if _, err := epub.Measure(f, info.Size()); err != nil {
		return err
	}
	return nil
}

// Trim removes the copies unused for MaxAgeDays and, of the rest, the least
// recently used until the copies fit in MaxBytes, and the KEPUBs of files
// no library holds any more; the notes of copies that were not made, once old; and files in the cache that no row names. An
// evicted copy keeps its hashes.
func (s *Service) Trim(ctx context.Context) error {
	q := sqlc.New(s.pool)
	old, err := q.ListCopiesToEvict(ctx, sqlc.ListCopiesToEvictParams{MaxAgeDays: MaxAgeDays, MaxBytes: MaxBytes})
	if err != nil {
		return err
	}
	for _, c := range old {
		if err := q.EvictDeviceCopy(ctx, sqlc.EvictDeviceCopyParams{SourceSha256: c.SourceSha256, Kind: c.Kind, FixVersion: c.FixVersion}); err != nil {
			return err
		}
		_ = os.Remove(s.pathOf(c.SourceSha256, c.Kind, c.FixVersion))
	}
	orphans, err := q.ListOrphanCopies(ctx)
	if err != nil {
		return err
	}
	for _, c := range orphans {
		if err := q.EvictDeviceCopy(ctx, sqlc.EvictDeviceCopyParams{SourceSha256: c.SourceSha256, Kind: c.Kind, FixVersion: c.FixVersion}); err != nil {
			return err
		}
		_ = os.Remove(s.pathOf(c.SourceSha256, c.Kind, c.FixVersion))
	}
	if _, err := q.DeleteStaleCopyNotes(ctx, MaxAgeDays); err != nil {
		return err
	}
	live, err := q.ListLiveCopies(ctx)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, c := range live {
		keep[filepath.Base(s.pathOf(c.SourceSha256, c.Kind, c.FixVersion))] = true
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		// A file just renamed in may be a moment ahead of its row, a temp
		// file a job's still writing.
		info, err := e.Info()
		if err != nil {
			continue
		}
		grace := time.Hour
		if strings.HasPrefix(e.Name(), ".making-") {
			grace = 24 * time.Hour
		}
		if time.Since(info.ModTime()) < grace {
			continue
		}
		_ = os.Remove(filepath.Join(s.dir, e.Name()))
	}
	return nil
}

const (
	makeKind = "devicecopy.make"
	trimKind = "devicecopy.trim"
)

// MakeArgs is the job that makes the copy of one file.
type MakeArgs struct {
	FileID uuid.UUID `json:"fileId"`
}

func (MakeArgs) Kind() string { return makeKind }

// MakeWorker runs MakeArgs.
type MakeWorker struct {
	river.WorkerDefaults[MakeArgs]
	Service *Service
}

func (w *MakeWorker) Work(ctx context.Context, job *river.Job[MakeArgs]) error {
	return w.Service.Make(ctx, job.Args.FileID)
}

// TrimArgs is the job that trims the copies.
type TrimArgs struct{}

func (TrimArgs) Kind() string { return trimKind }

// TrimWorker runs TrimArgs.
type TrimWorker struct {
	river.WorkerDefaults[TrimArgs]
	Service *Service
}

func (w *TrimWorker) Work(ctx context.Context, _ *river.Job[TrimArgs]) error {
	return w.Service.Trim(ctx)
}

// TrimPeriodic is the trim, every interval.
func TrimPeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, false, TrimArgs{}, jobs.QueueDefault)
}
