// Package ingest brings the files of a library's folder into the catalogue:
// it notices what is new, what changed, what moved and what is gone. A scan
// only ever reads the folder. A file that disappears is marked missing, and
// nothing on disk is deleted, moved or written. The one thing that writes is
// an upload, and only into a managed library.
package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// ErrNoFolder is returned when the library's folder cannot be read at all,
// which is usually a mount that is not there. Nothing is marked missing then.
var ErrNoFolder = errors.New("the library's folder cannot be read")

// errTaken is a path the scan found new that has a row by the time the scan
// stores it.
var errTaken = errors.New("the path was taken while the scan ran")

// Result is what one pass of a scan did.
type Result struct {
	// Seen is how many files of known formats the walk found.
	Seen int
	// Added, Changed and Moved count files that are new, that have other
	// content than before, and that were found under another path.
	Added, Changed, Moved int
	// Restored counts files that had been missing and are back.
	Restored int
	Missing  int
	// Skipped counts files and folders that could not be read.
	Skipped int
	// Books is how many books the new files made.
	Books int
	// Hashed is how many files were read to the end. An unchanged library
	// costs none.
	Hashed int
	// Complete is false when the pass stopped at its time budget. Everything
	// it did is stored, and the next pass goes on from there.
	Complete bool
}

// Scanner scans library folders.
type Scanner struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	// Budget is how long one pass may take before it stops between two
	// folders; zero is no limit.
	Budget time.Duration
}

// NewScanner returns a Scanner without a time budget.
func NewScanner(pool *pgxpool.Pool, log *slog.Logger) *Scanner {
	return &Scanner{pool: pool, log: log}
}

// known is a file the library already has a row for, kept up to date while
// the pass moves and adds files.
type known struct {
	id       uuid.UUID
	bookID   uuid.UUID
	relPath  string
	format   string
	size     int64
	modified time.Time
	sha256   []byte
	part     *int32
	missing  bool
	trashed  bool
	// seen is set when the walk found a file at the row's path.
	seen bool
}

// Scan makes one pass over the library's folder.
func (s *Scanner) Scan(ctx context.Context, libraryID uuid.UUID, rootPath string) (Result, error) {
	started := time.Now()
	var res Result

	root, err := filepath.EvalSymlinks(rootPath)
	if err == nil {
		var info fs.FileInfo
		if info, err = os.Stat(root); err == nil && !info.IsDir() {
			err = errors.New("not a folder")
		}
	}
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrNoFolder, err)
	}

	rows, err := sqlc.New(s.pool).ListFilesForScan(ctx, libraryID)
	if err != nil {
		return res, err
	}
	byPath := make(map[string]*known, len(rows))
	units := map[string][]*known{}
	for _, row := range rows {
		k := &known{
			id: row.ID, bookID: row.BookID, relPath: row.RelPath, format: row.Format,
			size: row.SizeBytes, modified: storedTime(row.ModifiedAt), sha256: row.Sha256,
			part: row.PartIndex, missing: row.MissingAt != nil, trashed: row.TrashedAt != nil,
		}
		byPath[k.relPath] = k
		if !k.trashed {
			unit := unitOf(k.relPath)
			units[unit] = append(units[unit], k)
		}
	}

	w, err := walk(ctx, root, s.log)
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, fmt.Errorf("%w: %v", ErrNoFolder, err)
	}
	res.Seen, res.Skipped = len(w.files), w.skipped

	// A pass that has read nothing yet goes on whatever the clock says, or a
	// slow walk would keep every pass from getting anywhere.
	overBudget := func() bool { return s.Budget > 0 && res.Hashed > 0 && time.Since(started) > s.Budget }
	q := sqlc.New(s.pool)

	fresh := map[string][]found{}
	for _, f := range w.files {
		k, ok := byPath[f.relPath]
		if !ok {
			unit := unitOf(f.relPath)
			fresh[unit] = append(fresh[unit], f)
			continue
		}
		k.seen = true
		unchanged := k.size == f.size && k.modified.Equal(f.modified) && k.sha256 != nil
		switch {
		case k.trashed:
		case unchanged && !k.missing:
		case unchanged:
			if err := q.RestoreFile(ctx, k.id); err != nil {
				return res, err
			}
			k.missing = false
			res.Restored++
		default:
			if overBudget() {
				return res, nil
			}
			if err := s.rehash(ctx, q, root, f, k, &res); err != nil {
				return res, err
			}
		}
	}

	// What the library knows and the walk did not find, by content: a new
	// file with the same content is that file under another path.
	vanished := map[string][]*known{}
	for _, k := range byPath {
		if !k.seen && !k.trashed && k.sha256 != nil && !below(k.relPath, w.unreadable) {
			vanished[string(k.sha256)] = append(vanished[string(k.sha256)], k)
		}
	}
	for _, candidates := range vanished {
		slices.SortFunc(candidates, func(a, b *known) int { return compareNatural(a.relPath, b.relPath) })
	}

	for _, unit := range slices.Sorted(maps.Keys(fresh)) {
		if overBudget() {
			return res, nil
		}
		if err := s.importUnit(ctx, libraryID, root, unit, fresh[unit], units, vanished, &res); err != nil {
			return res, err
		}
	}

	var gone []uuid.UUID
	for _, k := range byPath {
		if !k.seen && !k.trashed && !k.missing && !below(k.relPath, w.unreadable) {
			gone = append(gone, k.id)
		}
	}
	if len(gone) > 0 {
		n, err := q.MarkFilesMissing(ctx, gone)
		if err != nil {
			return res, err
		}
		res.Missing = int(n)
	}
	res.Complete = true
	return res, nil
}

// rehash reads a file whose size or time differs from what is stored.
func (s *Scanner) rehash(ctx context.Context, q *sqlc.Queries, root string, f found, k *known, res *Result) error {
	now, sum, err := hashFile(ctx, filepath.Join(root, filepath.FromSlash(f.relPath)), f.relPath, f.format)
	if err != nil {
		return s.skip(ctx, f.relPath, err, res)
	}
	res.Hashed++
	err = q.UpdateFileContent(ctx, sqlc.UpdateFileContentParams{
		ID: k.id, SizeBytes: now.size, ModifiedAt: now.modified, Sha256: sum,
	})
	if err != nil {
		return err
	}
	switch {
	case !bytes.Equal(k.sha256, sum):
		res.Changed++
	case k.missing:
		res.Restored++
	}
	k.size, k.modified, k.sha256, k.missing = now.size, now.modified, sum, false
	return nil
}

// skip notes a file that could not be read and lets the scan go on, unless
// the reason is that the scan itself was told to stop.
func (s *Scanner) skip(ctx context.Context, relPath string, err error, res *Result) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	s.log.Warn("scan: cannot read", "path", relPath, "error", err)
	res.Skipped++
	return nil
}

// importUnit takes in the new files of one folder: those that are known
// files under a new path are moved, the rest join the book they belong to or
// make a new one. The folder is stored as a whole or not at all.
func (s *Scanner) importUnit(
	ctx context.Context, libraryID uuid.UUID, root, unit string, files []found,
	units map[string][]*known, vanished map[string][]*known, res *Result,
) error {
	type hashed struct {
		found
		sum []byte
	}
	var added []hashed
	type move struct {
		file hashed
		row  *known
	}
	var moves []move
	for _, f := range files {
		now, sum, err := hashFile(ctx, filepath.Join(root, filepath.FromSlash(f.relPath)), f.relPath, f.format)
		if err != nil {
			if err := s.skip(ctx, f.relPath, err, res); err != nil {
				return err
			}
			continue
		}
		res.Hashed++
		h := hashed{found: now, sum: sum}
		candidates := vanished[string(sum)]
		at := slices.IndexFunc(candidates, func(k *known) bool { return k.format == f.format })
		if at < 0 {
			added = append(added, h)
			continue
		}
		moves = append(moves, move{file: h, row: candidates[at]})
		vanished[string(sum)] = slices.Delete(candidates, at, at+1)
	}
	if len(added) == 0 && len(moves) == 0 {
		return nil
	}

	// The folder as it will be: what was already in it, what moved in, and
	// what is new.
	members := map[string]*known{}
	for _, k := range units[unit] {
		members[k.relPath] = k
	}
	for _, m := range moves {
		delete(members, m.row.relPath)
		members[m.file.relPath] = m.row
	}
	var groups []Group
	if len(added) > 0 {
		paths := slices.Collect(maps.Keys(members))
		for _, a := range added {
			paths = append(paths, a.relPath)
		}
		groups = groupUnit(unit, paths)
	}
	newFiles := map[string]hashed{}
	for _, a := range added {
		newFiles[a.relPath] = a
	}

	type insert struct {
		file   hashed
		id     uuid.UUID
		bookID uuid.UUID
		part   *int32
	}
	var inserted []insert
	reparted := map[*known]*int32{}
	books := 0
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.LockLibraryFiles(ctx, libraryID); err != nil {
			return err
		}
		paths := make([]string, 0, len(files))
		for _, f := range files {
			paths = append(paths, f.relPath)
		}
		taken, err := q.ListTakenPaths(ctx, sqlc.ListTakenPathsParams{LibraryID: libraryID, Paths: paths})
		if err != nil {
			return err
		}
		if len(taken) > 0 {
			return errTaken
		}
		for _, m := range moves {
			err := q.MoveFile(ctx, sqlc.MoveFileParams{
				ID: m.row.id, RelPath: m.file.relPath, SizeBytes: m.file.size, ModifiedAt: m.file.modified,
			})
			if err != nil {
				return fmt.Errorf("move %s: %w", m.file.relPath, err)
			}
		}
		for _, g := range groups {
			if !slices.ContainsFunc(g.Files, func(p string) bool { _, isNew := newFiles[p]; return isNew }) {
				continue
			}
			bookID := uuid.Nil
			for _, p := range g.Files {
				if k, ok := members[p]; ok {
					bookID = k.bookID
					break
				}
			}
			if bookID == uuid.Nil {
				var err error
				bookID, err = catalog.CreateBookTx(ctx, tx, catalog.NewBook{
					LibraryID: libraryID, Title: g.Title, Source: catalog.SourceFilename,
				})
				if err != nil {
					return fmt.Errorf("book for %s: %w", g.Files[0], err)
				}
				books++
			}
			for _, p := range g.Files {
				var part *int32
				if index, ok := g.Parts[p]; ok {
					part = &index
				}
				if k, ok := members[p]; ok {
					if !equalPart(k.part, part) {
						if err := q.SetFilePartIndex(ctx, sqlc.SetFilePartIndexParams{ID: k.id, PartIndex: part}); err != nil {
							return err
						}
						reparted[k] = part
					}
					continue
				}
				f := newFiles[p]
				id, err := catalog.AddFileTx(ctx, tx, bookID, libraryID, catalog.NewFile{
					RelPath: f.relPath, Format: f.format, Size: f.size, ModifiedAt: f.modified,
					SHA256: f.sum, PartIndex: part,
				})
				if err != nil {
					return fmt.Errorf("file %s: %w", f.relPath, err)
				}
				inserted = append(inserted, insert{file: f, id: id, bookID: bookID, part: part})
			}
		}
		return nil
	})
	if errors.Is(err, errTaken) {
		// An upload put a file here after the pass listed what the library
		// knows. What the pass decided for this folder is out of date; the
		// next scan decides again with the upload in view.
		s.log.Debug("scan: folder changed by an upload, left for the next scan", "folder", unit)
		return nil
	}
	if err != nil {
		return err
	}

	for _, m := range moves {
		from := unitOf(m.row.relPath)
		units[from] = slices.DeleteFunc(units[from], func(k *known) bool { return k == m.row })
		m.row.relPath, m.row.size, m.row.modified = m.file.relPath, m.file.size, m.file.modified
		m.row.seen, m.row.missing = true, false
		units[unit] = append(units[unit], m.row)
	}
	for k, part := range reparted {
		k.part = part
	}
	for _, in := range inserted {
		units[unit] = append(units[unit], &known{
			id: in.id, bookID: in.bookID, relPath: in.file.relPath, format: in.file.format,
			size: in.file.size, modified: in.file.modified, sha256: in.file.sum, part: in.part, seen: true,
		})
	}
	res.Moved += len(moves)
	res.Added += len(inserted)
	res.Books += books
	return nil
}

func equalPart(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// below reports whether relPath lies in one of the folders.
func below(relPath string, folders []string) bool {
	for _, folder := range folders {
		if strings.HasPrefix(relPath, folder+"/") {
			return true
		}
	}
	return false
}
