package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What became of an upload.
const (
	UploadAdded = "added"
	// UploadDuplicate is a file whose exact bytes a library already has.
	// Nothing was stored.
	UploadDuplicate = "duplicate"
)

var (
	// ErrNotManaged is an upload into a library GOtome does not arrange.
	// External libraries are only ever read.
	ErrNotManaged = errors.New("uploads go into managed libraries only")
	// ErrUploadTooLarge is an upload larger than Service.UploadLimit.
	ErrUploadTooLarge = errors.New("the file is larger than an upload may be")
	// ErrUnknownFormat is a file whose name does not end in a format GOtome
	// reads.
	ErrUnknownFormat = errors.New("not a format GOtome knows")
)

const (
	// stagingFolder holds an upload until the whole file has arrived. The
	// scan passes it by, as it passes every folder whose name starts with a
	// dot, and it lies in the library so that moving a file out of it into
	// place is a rename on the same file system.
	stagingFolder = ".uploads"
	// staleUpload is how old a staged file has to be before it is taken for
	// the remains of an upload the server did not live to finish.
	staleUpload = 24 * time.Hour
	// maxNameBytes keeps a folder or file name well under the 255 bytes file
	// systems allow, with room for " (99)" and an extension.
	maxNameBytes = 200
	// maxNameTries bounds the search for a free name when the name a file
	// arrived with is taken.
	maxNameTries = 100
)

// Storage is what a user's uploads take up, and how much they may.
type Storage struct {
	UsedBytes int64
	// QuotaBytes is nil for no limit.
	QuotaBytes *int64
}

// QuotaError is an upload that would take its uploader past their quota.
type QuotaError struct {
	Storage
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("the upload would pass the quota: %d of %d bytes used", e.UsedBytes, *e.QuotaBytes)
}

// Storage returns what the user's uploads take up and may.
func (s *Service) Storage(ctx context.Context, userID uuid.UUID) (Storage, error) {
	return storageOf(ctx, sqlc.New(s.pool), userID)
}

func storageOf(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (Storage, error) {
	row, err := q.GetUserStorage(ctx, userID)
	if err != nil {
		return Storage{}, err
	}
	return Storage{UsedBytes: row.UsedBytes, QuotaBytes: row.QuotaBytes}, nil
}

// CheckRoom returns a *QuotaError when a file of the size would not fit
// into the user's quota, so that an upload is refused before its body is
// read. Upload checks again with the size the file turns out to have.
func (s *Service) CheckRoom(ctx context.Context, userID uuid.UUID, size int64) error {
	st, err := s.Storage(ctx, userID)
	if err != nil {
		return err
	}
	if st.QuotaBytes != nil && st.UsedBytes+size > *st.QuotaBytes {
		return &QuotaError{Storage: st}
	}
	return nil
}

// Uploaded is what became of one uploaded file.
type Uploaded struct {
	// Outcome is added, or duplicate when a library the uploader can see
	// already has the file; the book is then the one that has it.
	Outcome   string    `json:"outcome"`
	BookID    uuid.UUID `json:"bookId"`
	LibraryID uuid.UUID `json:"libraryId"`
	// Title is the book's title as it stands. For a new book that is a guess
	// from the file's name until the file has been read.
	Title string `json:"title"`
	// FileID is the stored file, for an upload that was added.
	FileID *uuid.UUID `json:"fileId,omitempty"`
}

// Upload stores a file somebody sends into a managed library and adds it to
// the catalogue. It lands in a folder named after the book the file name
// suggests, with the other formats and parts of that book. A file whose
// exact bytes the uploader can already see in any library is not stored
// again. A file that would take the uploader past their quota is refused
// with a *QuotaError.
//
// The file is written to the library's staging folder first and moved into
// place in the same transaction that records it, so an upload that fails or
// is cut off leaves nothing in the library.
func (s *Service) Upload(ctx context.Context, lib library.Library, scope library.Scope, name string, body io.Reader) (Uploaded, error) {
	if lib.Mode != library.ModeManaged {
		return Uploaded{}, ErrNotManaged
	}
	stem, format := splitName(name)
	if _, ok := catalog.KindOf(format); !ok {
		return Uploaded{}, fmt.Errorf("%w: %q", ErrUnknownFormat, name)
	}

	staging := filepath.Join(lib.RootPath, stagingFolder)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return Uploaded{}, err
	}
	s.clearStale(staging)
	tmp, err := os.CreateTemp(staging, "*.part")
	if err != nil {
		return Uploaded{}, err
	}
	staged := tmp.Name()
	defer os.Remove(staged)

	// The file may be as large as uploads may be, or as there is room left
	// for, whichever is less; reading stops one byte past that.
	storage, err := s.Storage(ctx, scope.Viewer)
	if err != nil {
		return Uploaded{}, err
	}
	room, tooLarge := s.UploadLimit, error(ErrUploadTooLarge)
	if q := storage.QuotaBytes; q != nil && max(*q-storage.UsedBytes, 0) < room {
		room, tooLarge = max(*q-storage.UsedBytes, 0), &QuotaError{Storage: storage}
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, room+1))
	if err == nil && n > room {
		err = tooLarge
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Uploaded{}, err
	}
	info, err := os.Stat(staged)
	if err != nil {
		return Uploaded{}, err
	}
	sum := h.Sum(nil)

	var out Uploaded
	var placed string
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.LockLibraryFiles(ctx, lib.ID); err != nil {
			return err
		}
		// Asked under the lock, so that the same file sent twice at once is
		// stored once.
		dup, err := q.FindVisibleFileByHash(ctx, sqlc.FindVisibleFileByHashParams{
			Sha256: sum, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		})
		if err == nil {
			out = Uploaded{Outcome: UploadDuplicate, BookID: dup.BookID, LibraryID: dup.LibraryID, Title: dup.Title}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// Under the uploader's own lock: another of their uploads may have
		// taken the room since the file began to arrive.
		if err := q.LockUserStorage(ctx, scope.Viewer); err != nil {
			return err
		}
		st, err := storageOf(ctx, q, scope.Viewer)
		if err != nil {
			return err
		}
		if st.QuotaBytes != nil && st.UsedBytes+info.Size() > *st.QuotaBytes {
			return &QuotaError{Storage: st}
		}

		folder := folderFor(stem, format)
		relPath, err := freePath(ctx, q, lib, folder, stem, format)
		if err != nil {
			return err
		}
		bookID, part, err := s.bookFor(ctx, tx, lib.ID, folder, relPath)
		if err != nil {
			return err
		}
		fileID, err := catalog.AddFileTx(ctx, tx, bookID, lib.ID, catalog.NewFile{
			RelPath: relPath, Format: format, Size: info.Size(), ModifiedAt: storedTime(info.ModTime()),
			SHA256: sum, PartIndex: part, UploadedBy: &scope.Viewer,
		})
		if err != nil {
			return err
		}
		if _, ok := extractors[format]; ok {
			_, err := s.Queue.InsertTx(ctx, tx, ExtractArgs{FileID: fileID}, jobs.InsertOpts{
				Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
			})
			if err != nil {
				return err
			}
		}
		title, err := q.GetBookTitle(ctx, bookID)
		if err != nil {
			return err
		}

		// Last, so that nothing after it can fail but the commit.
		full := filepath.Join(lib.RootPath, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.Rename(staged, full); err != nil {
			return err
		}
		placed = full
		out = Uploaded{Outcome: UploadAdded, BookID: bookID, LibraryID: lib.ID, Title: title, FileID: &fileID}
		return nil
	})
	if err != nil && placed != "" {
		// The commit failed after the file was moved into place. It is the
		// file this upload put there a moment ago, which nothing records.
		if rmErr := os.Remove(placed); rmErr != nil {
			s.log.Error("upload: cannot take back a file whose record failed", "path", placed, "error", rmErr)
		}
		_ = os.Remove(filepath.Dir(placed))
	}
	if err != nil {
		return Uploaded{}, err
	}
	return out, nil
}

// bookFor decides which book a new file at relPath belongs to, the way a
// scan groups the files of a folder, and returns the file's position among
// the book's parts if it is one. A file that joins no book in the folder
// makes a new one.
func (s *Service) bookFor(ctx context.Context, tx pgx.Tx, libraryID uuid.UUID, folder, relPath string) (uuid.UUID, *int32, error) {
	q := sqlc.New(tx)
	rows, err := q.ListFolderFiles(ctx, sqlc.ListFolderFilesParams{LibraryID: libraryID, Folder: folder})
	if err != nil {
		return uuid.Nil, nil, err
	}
	members := map[string]sqlc.ListFolderFilesRow{}
	paths := []string{relPath}
	for _, row := range rows {
		if unitOf(row.RelPath) == folder {
			members[row.RelPath] = row
			paths = append(paths, row.RelPath)
		}
	}
	groups := groupUnit(folder, paths)
	at := slices.IndexFunc(groups, func(g Group) bool { return slices.Contains(g.Files, relPath) })
	if at < 0 {
		return uuid.Nil, nil, fmt.Errorf("%s fell into no group", relPath)
	}
	g := groups[at]

	bookID := uuid.Nil
	for _, p := range g.Files {
		if m, ok := members[p]; ok {
			bookID = m.BookID
			break
		}
	}
	if bookID == uuid.Nil {
		bookID, err = catalog.CreateBookTx(ctx, tx, catalog.NewBook{
			LibraryID: libraryID, Title: g.Title, Source: catalog.SourceFilename,
		})
		if err != nil {
			return uuid.Nil, nil, err
		}
	}
	// A new part may move the ones after it along.
	for _, p := range g.Files {
		m, ok := members[p]
		if !ok {
			continue
		}
		var part *int32
		if index, ok := g.Parts[p]; ok {
			part = &index
		}
		if !equalPart(m.PartIndex, part) {
			if err := q.SetFilePartIndex(ctx, sqlc.SetFilePartIndexParams{ID: m.ID, PartIndex: part}); err != nil {
				return uuid.Nil, nil, err
			}
		}
	}
	if index, ok := g.Parts[relPath]; ok {
		return bookID, &index, nil
	}
	return bookID, nil, nil
}

// freePath is where a file of that name goes in the folder: under its own
// name, or with a number after it when a file there already has that name,
// on disk or in the catalogue.
func freePath(ctx context.Context, q *sqlc.Queries, lib library.Library, folder, stem, format string) (string, error) {
	for i := 1; i <= maxNameTries; i++ {
		name := stem
		if i > 1 {
			name += " (" + strconv.Itoa(i) + ")"
		}
		relPath := folder + "/" + name + "." + format
		taken, err := q.ListTakenPaths(ctx, sqlc.ListTakenPathsParams{LibraryID: lib.ID, Paths: []string{relPath}})
		if err != nil {
			return "", err
		}
		if len(taken) > 0 {
			continue
		}
		_, err = os.Lstat(filepath.Join(lib.RootPath, filepath.FromSlash(relPath)))
		if errors.Is(err, os.ErrNotExist) {
			return relPath, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s.%s in %s", stem, format, folder)
}

// splitName takes a name as a browser sends it apart into a stem that is
// safe as a file name and the format.
func splitName(name string) (stem, format string) {
	// Some browsers send the path the file was picked from.
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	ext := path.Ext(name)
	format = strings.ToLower(strings.TrimPrefix(ext, "."))
	stem = safeName(strings.TrimSuffix(name, ext))
	if stem == "" {
		stem = format
	}
	return stem, format
}

// folderFor names the folder an upload goes into after the book its name
// suggests: "Dune - Part 02.mp3" into "Dune", "Emma.epub" into "Emma".
func folderFor(stem, format string) string {
	title := stem
	if kind, _ := catalog.KindOf(format); kind == catalog.KindAudio {
		if pt := partOf(".", stem+"."+format); pt.base != "" {
			title = pt.base
		}
	}
	if folder := safeName(cleanTitle(title)); folder != "" {
		return folder
	}
	return stem
}

// safeName makes a name from somebody else's computer safe as one element of
// a path on any file system a library may be on: no separators, none of the
// characters Windows shares refuse, no control characters, no leading dot
// that would hide it, and not too long.
func safeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(name, "") {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	name = strings.Trim(strings.Join(strings.Fields(b.String()), " "), ". ")
	for len(name) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return strings.TrimRight(name, ". ")
}

// clearStale removes what uploads the server did not live to finish left in
// the staging folder. Files that are younger may be uploads still arriving.
func (s *Service) clearStale(staging string) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.Type().IsRegular() || time.Since(info.ModTime()) < staleUpload {
			continue
		}
		if err := os.Remove(filepath.Join(staging, e.Name())); err != nil {
			s.log.Warn("upload: cannot remove a stale upload", "path", e.Name(), "error", err)
		}
	}
}
