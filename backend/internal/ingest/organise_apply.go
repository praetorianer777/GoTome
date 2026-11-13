package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// OrganiseRecord is a file's move as an action keeps it, to be undone.
type OrganiseRecord struct {
	FileID   uuid.UUID       `json:"fileId"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Sidecars []SidecarRecord `json:"sidecars,omitempty"`
}

// SidecarRecord is a file or folder that lay beside a book file and moved
// with it.
type SidecarRecord struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// organiseKey is the key of the action's snapshot that holds the moves; the
// tables of curation.Tables are the other keys, and it has none of them.
const organiseKey = "organise"

// sidecarExtensions are the files that lie beside a book file under its
// name and move with it: a cover, and the package file Calibre writes.
var sidecarExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".opf"}

// bookExtensions are the extensions of files a scan takes as books, which
// say whether a name beside a file belongs to another book file too.
var bookExtensions = []string{
	".epub", ".kepub", ".pdf", ".mobi", ".azw", ".azw3", ".m4b", ".m4a", ".mp3", ".flac", ".ogg", ".opus", ".cbz", ".fb2", ".djvu", ".txt",
}

type organiseSkipped struct{ reason string }

func (e *organiseSkipped) Error() string { return "skipped: " + e.reason }

type renamed struct{ from, to string }

// WorkOrganise applies the run's pattern to its library, a book at a time,
// each in a transaction of its own, for a while. It says whether the run is
// finished, and, if not, how long to wait before going on.
func (s *Service) WorkOrganise(ctx context.Context, runID uuid.UUID) (bool, time.Duration, error) {
	q := sqlc.New(s.pool)
	run, err := q.GetOrganiseRun(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if run.State != curation.StatePlanned && run.State != curation.StateRunning {
		return true, 0, nil
	}
	if run.OrganiseLibrary == nil || run.OrganisePattern == nil {
		return true, 0, q.FinishCurationRunByID(ctx, runID)
	}
	if err := q.StartDuplicatesRun(ctx, runID); err != nil {
		return false, 0, err
	}
	l, err := s.organiseLibrary(ctx, *run.OrganiseLibrary)
	if errors.Is(err, library.ErrNotFound) {
		return true, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	finish := func() (bool, time.Duration, error) { return true, 0, q.FinishCurationRunByID(ctx, runID) }
	if !organiseAllowed(l) {
		return finish()
	}
	// A scan that walked the folder before a move would find the file
	// missing where it was.
	if scanning, err := q.LibraryScanActive(ctx, l.ID); err != nil {
		return false, 0, err
	} else if scanning {
		return false, 30 * time.Second, nil
	}
	if err := probeFolder(l.ID, l.RootPath); err != nil {
		_ = s.noteUnwritable(ctx, err)
		return finish()
	}
	p, err := parsePattern(l, *run.OrganisePattern)
	if err != nil {
		return finish()
	}
	plan, err := s.planFor(ctx, l, p)
	if err != nil {
		return false, 0, err
	}

	byBook := map[uuid.UUID][]OrganiseMove{}
	var order []uuid.UUID
	for _, m := range plan.Moves {
		if _, ok := byBook[m.BookID]; !ok {
			order = append(order, m.BookID)
		}
		byBook[m.BookID] = append(byBook[m.BookID], m)
	}
	started, failures := time.Now(), 0
	for _, book := range order {
		if time.Since(started) > organiseBudget {
			return false, time.Second, nil
		}
		if ctx.Err() != nil {
			return false, 0, ctx.Err()
		}
		var done bool
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) (err error) {
			done, err = s.Curation.HasBook(ctx, tx, runID, book)
			return err
		})
		if err != nil {
			return false, 0, err
		}
		if done {
			continue
		}
		skipped, err := s.organiseBook(ctx, runID, l, book, byBook[book])
		if err != nil {
			if ctx.Err() != nil {
				return false, 0, ctx.Err()
			}
			s.log.Warn("organise: a book's files could not be moved", "book", book, "error", err)
			failures++
			if rerr := s.recordOrganiseFailure(ctx, runID, l.ID, book, err); rerr != nil {
				return false, 0, rerr
			}
			if failures >= organiseMaxFailures {
				return finish()
			}
			continue
		}
		if skipped != "" {
			if err := s.recordOrganiseFailure(ctx, runID, l.ID, book, errors.New(skipped)); err != nil {
				return false, 0, err
			}
		}
	}
	if n := len(plan.Skipped); n > 0 {
		if err := q.AddRunSkipped(ctx, sqlc.AddRunSkippedParams{ID: runID, Skipped: int32(n)}); err != nil {
			return false, 0, err
		}
	}
	return finish()
}

// recordOrganiseFailure leaves a book of the run as an action that changed
// nothing, with the reason, so that the run does not try it again.
func (s *Service) recordOrganiseFailure(ctx context.Context, runID, libraryID, book uuid.UUID, cause error) error {
	err := s.Curation.RecordPlan(ctx, runID, libraryID, curation.ActionSkip, "organise", cause.Error(), book)
	if err != nil {
		return err
	}
	return sqlc.New(s.pool).AddRunSkipped(ctx, sqlc.AddRunSkippedParams{ID: runID, Skipped: 1})
}

// organiseBook moves one book's files in a transaction: the records, the
// action that undoes them, and, last, the files on disk, put back if
// anything after the first rename fails. A non-empty skipped is the reason
// the book was left because it changed since the plan.
func (s *Service) organiseBook(ctx context.Context, runID uuid.UUID, l sqlc.GetOrganiseLibraryRow, book uuid.UUID, moves []OrganiseMove) (skipped string, err error) {
	var done []renamed
	var made []string
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		done, made = nil, nil
		q := sqlc.New(tx)
		if err := q.HoldLibrary(ctx, l.ID); err != nil {
			return err
		}
		if err := q.LockLibraryFiles(ctx, l.ID); err != nil {
			return err
		}
		rows, err := q.ListOrganiseBookFiles(ctx, sqlc.ListOrganiseBookFilesParams{LibraryID: l.ID, Books: []uuid.UUID{book}})
		if err != nil {
			return err
		}
		now := map[uuid.UUID]sqlc.ListOrganiseBookFilesRow{}
		for _, r := range rows {
			now[r.ID] = r
		}
		moving := map[uuid.UUID]bool{}
		for _, m := range moves {
			moving[m.FileID] = true
			f, ok := now[m.FileID]
			if !ok || f.TrashedAt != nil || f.MissingAt != nil || f.RelPath != m.From {
				return &organiseSkipped{"The file changed since the plan was made."}
			}
			taken, err := q.FilePathTaken(ctx, sqlc.FilePathTakenParams{LibraryID: l.ID, RelPath: m.To, FileID: m.FileID})
			if err != nil {
				return err
			}
			if taken {
				return &organiseSkipped{"Another file holds the new path now."}
			}
			if _, err := os.Lstat(abs(l.RootPath, m.To)); err == nil && !sameFile(abs(l.RootPath, m.From), abs(l.RootPath, m.To)) {
				return &organiseSkipped{"Something lies at the new path."}
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return folderError(l.ID, l.RootPath, err)
			}
			if _, err := os.Lstat(abs(l.RootPath, m.From)); err != nil {
				return &organiseSkipped{"The file is no longer where its record says."}
			}
		}
		records, err := s.withSidecars(ctx, q, l, moves, moving)
		if err != nil {
			return err
		}
		for _, r := range records {
			n, err := q.SetFilePath(ctx, sqlc.SetFilePathParams{ID: r.FileID, RelPath: r.To, Was: r.From})
			if err != nil {
				return err
			}
			if n != 1 {
				return &organiseSkipped{"The file changed since the plan was made."}
			}
		}
		if err := s.recordOrganise(ctx, tx, runID, l.ID, book, records); err != nil {
			return err
		}
		// Last, so that nothing after it can fail but the commit.
		for _, r := range records {
			if err := s.renameInto(l, r.From, r.To, &done, &made); err != nil {
				return err
			}
			for _, sc := range r.Sidecars {
				if err := s.renameInto(l, sc.From, sc.To, &done, &made); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		s.moveAllBack(l.RootPath, done, made)
		var skip *organiseSkipped
		if errors.As(err, &skip) {
			return skip.reason, nil
		}
		return "", s.noteUnwritable(ctx, err)
	}
	for _, r := range done {
		removeEmptyParents(l.RootPath, path.Dir(filepath.ToSlash(relTo(l.RootPath, r.from))))
	}
	return "", nil
}

func abs(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

func relTo(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return "."
	}
	return r
}

func sameFile(a, b string) bool {
	x, err := os.Lstat(a)
	if err != nil {
		return false
	}
	y, err := os.Lstat(b)
	if err != nil {
		return false
	}
	return os.SameFile(x, y)
}

// renameInto moves a file or folder of the library to a path whose folders
// it makes, noting what it did so that a failure later can be taken back.
func (s *Service) renameInto(l sqlc.GetOrganiseLibraryRow, fromRel, toRel string, done *[]renamed, made *[]string) error {
	from, to := abs(l.RootPath, fromRel), abs(l.RootPath, toRel)
	var missing []string
	for dir := filepath.Dir(to); dir != l.RootPath && len(dir) > len(l.RootPath); dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		missing = append(missing, dir)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		*made = append(*made, missing...)
		return folderError(l.ID, l.RootPath, err)
	}
	*made = append(*made, missing...)
	if err := os.Rename(from, to); err != nil {
		return folderError(l.ID, l.RootPath, err)
	}
	*done = append(*done, renamed{from, to})
	return nil
}

// moveAllBack puts the renames back, the last first, and takes away the
// folders it made for them.
func (s *Service) moveAllBack(root string, done []renamed, made []string) {
	for i := len(done) - 1; i >= 0; i-- {
		r := done[i]
		if _, err := os.Lstat(r.to); err != nil {
			continue
		}
		if err := os.Rename(r.to, r.from); err != nil {
			s.log.Error("organise: cannot move a file back after its record failed", "from", r.to, "to", r.from, "error", err)
		}
	}
	for i := len(made) - 1; i >= 0; i-- {
		_ = os.Remove(made[i])
	}
}

// removeEmptyParents removes the folder, and the folders above it up to
// the library's own, while each is empty. A folder that holds anything
// stays, so does the library's root.
func removeEmptyParents(root, rel string) {
	for rel != "." && rel != "" && rel != "/" {
		if err := os.Remove(abs(root, rel)); err != nil {
			return
		}
		rel = path.Dir(rel)
	}
}

// withSidecars adds to each move the files beside the book file that go
// along: a KOReader folder (name.sdr) and a cover or package file under
// the same name. They stay where they are if another book file has that
// name too, unless that is a file of this book that goes to the same new
// name, or if something already lies at their new place.
func (s *Service) withSidecars(ctx context.Context, q *sqlc.Queries, l sqlc.GetOrganiseLibraryRow, moves []OrganiseMove, moving map[uuid.UUID]bool) ([]OrganiseRecord, error) {
	type stem struct{ dir, name string }
	stemOf := func(rel string) stem {
		base := path.Base(rel)
		return stem{path.Dir(rel), strings.TrimSuffix(base, path.Ext(base))}
	}
	var candidates []string
	for _, m := range moves {
		st := stemOf(m.From)
		for _, e := range bookExtensions {
			candidates = append(candidates, path.Join(st.dir, st.name+e))
		}
	}
	holders, err := q.ListFilesAtPaths(ctx, sqlc.ListFilesAtPathsParams{LibraryID: l.ID, Paths: candidates})
	if err != nil {
		return nil, err
	}
	type group struct {
		to       stem
		same     bool
		outsider bool
		first    int
	}
	groups := map[stem]*group{}
	records := make([]OrganiseRecord, len(moves))
	for i, m := range moves {
		records[i] = OrganiseRecord{FileID: m.FileID, From: m.From, To: m.To}
		from, to := stemOf(m.From), stemOf(m.To)
		g, ok := groups[from]
		if !ok {
			g = &group{to: to, same: true, first: i}
			groups[from] = g
		}
		if g.to != to {
			g.same = false
		}
	}
	for _, h := range holders {
		if !moving[h.ID] {
			if g, ok := groups[stemOf(h.RelPath)]; ok {
				g.outsider = true
			}
		}
	}
	for from, g := range groups {
		if !g.same || g.outsider || from == g.to {
			continue
		}
		var found []SidecarRecord
		add := func(suffix string, wantDir bool) {
			src, dst := path.Join(from.dir, from.name+suffix), path.Join(g.to.dir, g.to.name+suffix)
			info, err := os.Lstat(abs(l.RootPath, src))
			if err != nil || info.IsDir() != wantDir || (!wantDir && !info.Mode().IsRegular()) {
				return
			}
			if _, err := os.Lstat(abs(l.RootPath, dst)); err == nil || !errors.Is(err, fs.ErrNotExist) {
				return
			}
			found = append(found, SidecarRecord{From: src, To: dst})
		}
		add(".sdr", true)
		for _, e := range sidecarExtensions {
			add(e, false)
		}
		records[g.first].Sidecars = found
	}
	return records, nil
}

// recordOrganise writes the action of a book's move, with the moves as its
// snapshot, which UndoOrganiseTx reads.
func (s *Service) recordOrganise(ctx context.Context, tx pgx.Tx, runID, libraryID, book uuid.UUID, records []OrganiseRecord) error {
	data, err := json.Marshal(map[string][]OrganiseRecord{organiseKey: records})
	if err != nil {
		return err
	}
	rule := "organise"
	_, err = sqlc.New(tx).InsertCurationAction(ctx, sqlc.InsertCurationActionParams{
		RunID: runID, LibraryID: libraryID, Kind: curation.ActionOrganise, Rule: &rule,
		Books: []uuid.UUID{book}, TrashedFiles: []uuid.UUID{}, Before: data, After: []byte("{}"),
	})
	return err
}

// UndoOrganiseTx prepares the undoing of an organise action in the
// transaction that records it: the files' records get their old paths back,
// and the moves to make on disk once it has committed are returned. When
// the files are not as the action left them, it says so as a state and a
// message and changes nothing.
func (s *Service) UndoOrganiseTx(ctx context.Context, tx pgx.Tx, libraryID uuid.UUID, before []byte) ([]curation.FileMove, string, string, error) {
	var snap map[string][]OrganiseRecord
	if err := json.Unmarshal(before, &snap); err != nil {
		return nil, "", "", err
	}
	records := snap[organiseKey]
	q := sqlc.New(tx)
	ids := make([]uuid.UUID, len(records))
	for i, r := range records {
		ids[i] = r.FileID
	}
	rows, err := q.ListOrganiseUndoFiles(ctx, ids)
	if err != nil {
		return nil, "", "", err
	}
	now := map[uuid.UUID]sqlc.ListOrganiseUndoFilesRow{}
	for _, r := range rows {
		now[r.ID] = r
	}
	var moves []curation.FileMove
	for _, r := range records {
		f, ok := now[r.FileID]
		switch {
		case !ok:
			return nil, curation.OutcomeConflict, "A file the action moved has been deleted for good.", nil
		case f.TrashedAt != nil || f.RelPath != r.To:
			return nil, curation.OutcomeConflict, "A file the action moved has been moved, renamed or trashed since.", nil
		case !f.Writable:
			return nil, curation.OutcomeNotUndoable, "The library is read-only.", nil
		}
		from, to := abs(f.RootPath, r.To), abs(f.RootPath, r.From)
		if _, err := os.Lstat(from); err != nil {
			return nil, curation.OutcomeNotUndoable, "A file the action moved is no longer there.", nil
		}
		if _, err := os.Lstat(to); err == nil {
			return nil, curation.OutcomeConflict, "Another file is where a moved file was.", nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", "", err
		}
		taken, err := q.FilePathTaken(ctx, sqlc.FilePathTakenParams{LibraryID: libraryID, RelPath: r.From, FileID: r.FileID})
		if err != nil {
			return nil, "", "", err
		}
		if taken {
			return nil, curation.OutcomeConflict, "Another file's record holds the path of a moved file.", nil
		}
		n, err := q.SetFilePath(ctx, sqlc.SetFilePathParams{ID: r.FileID, RelPath: r.From, Was: r.To})
		if err != nil {
			return nil, "", "", err
		}
		if n != 1 {
			return nil, curation.OutcomeConflict, "A file the action moved has changed since.", nil
		}
		moves = append(moves, curation.FileMove{From: from, To: to, Stop: f.RootPath})
		for _, sc := range r.Sidecars {
			sfrom, sto := abs(f.RootPath, sc.To), abs(f.RootPath, sc.From)
			if _, err := os.Lstat(sfrom); err != nil {
				continue
			}
			if _, err := os.Lstat(sto); err == nil {
				continue
			}
			moves = append(moves, curation.FileMove{From: sfrom, To: sto, Stop: f.RootPath})
		}
	}
	return moves, "", "", nil
}

// OrganiseWorker runs OrganiseArgs.
type OrganiseWorker struct {
	river.WorkerDefaults[OrganiseArgs]
	Service *Service
}

func (w *OrganiseWorker) Work(ctx context.Context, job *river.Job[OrganiseArgs]) error {
	done, wait, err := w.Service.WorkOrganise(ctx, job.Args.RunID)
	if err != nil {
		return err
	}
	if !done {
		return river.JobSnooze(wait)
	}
	return nil
}
