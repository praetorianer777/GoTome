package ingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/organise"
)

var (
	// ErrOrganiseOff is a library whose files GOtome may not rename: an
	// external one that is read-only or has not been opted in.
	ErrOrganiseOff = errors.New("organising is not allowed for this library")
	// ErrOrganiseBusy is a library that has a run waiting or going.
	ErrOrganiseBusy = errors.New("the library is being organised")
)

// PatternError is a pattern that cannot be used, with the reason.
type PatternError struct{ Reason string }

func (e *PatternError) Error() string { return e.Reason }

// Why a file is left where it is.
const (
	SkipMissing = "missing"
	SkipTooLong = "too-long"
)

// OrganiseMove is one file's way to the place its book's metadata gives it.
type OrganiseMove struct {
	FileID uuid.UUID
	BookID uuid.UUID
	Title  string
	From   string
	To     string
	// Collision says the name had to take a number, as another file has
	// the plain one.
	Collision bool
}

// OrganiseSkip is a file the pattern leaves alone, and why.
type OrganiseSkip struct {
	FileID uuid.UUID
	BookID uuid.UUID
	Title  string
	Path   string
	Reason string
}

// OrganisePlan is what a pattern would do to a library.
type OrganisePlan struct {
	Pattern string
	// Files is how many files were looked at, in Books books.
	Files     int
	Books     int
	Unchanged int
	Moves     []OrganiseMove
	// Collisions counts the files, moved or not, that carry a number.
	Collisions int
	Skipped    []OrganiseSkip
}

func pathKey(p string) string { return strings.ToLower(norm.NFC.String(p)) }

func extensionOf(f sqlc.ListOrganiseFilesRow) string {
	if e := strings.ToLower(strings.TrimPrefix(path.Ext(f.RelPath), ".")); e != "" {
		return e
	}
	return f.Format
}

func seriesNumber(i *float64) string {
	if i == nil {
		return ""
	}
	whole, frac, _ := strings.Cut(strconv.FormatFloat(*i, 'f', -1, 64), ".")
	if len(whole) < 2 {
		whole = "0" + whole
	}
	if frac != "" {
		return whole + "." + frac
	}
	return whole
}

func valuesOf(f sqlc.ListOrganiseFilesRow) organise.Values {
	v := organise.Values{
		"author": f.Author, "author_sort": f.AuthorSort, "title": f.Title, "series": f.Series,
		"series_index": "", "language": "", "year": "", "ext": extensionOf(f),
	}
	if f.Author == "" {
		v["author"] = "Unknown author"
	}
	if f.AuthorSort == "" {
		v["author_sort"] = v["author"]
	}
	if strings.TrimSpace(f.Title) == "" {
		v["title"] = "Untitled"
	}
	if f.Series != "" {
		v["series_index"] = seriesNumber(f.SeriesIndex)
	}
	if f.Language != nil {
		v["language"] = *f.Language
	}
	if f.PublishedOn != nil {
		v["year"] = strconv.Itoa(f.PublishedOn.Year())
	}
	return v
}

// planOrganise works out where each file goes. paths are the records of
// every file of the library, trashed and missing ones included, whose
// paths stay taken; onDisk says whether something untracked lies at a path.
// A path another file holds now is never taken, even by one that moves
// away, so a repeat of the run settles what a swap left.
func planOrganise(p organise.Pattern, files []sqlc.ListOrganiseFilesRow, paths map[uuid.UUID]string, onDisk func(string) bool) OrganisePlan {
	plan := OrganisePlan{Pattern: p.String()}
	taken := make(map[string]uuid.UUID, len(paths))
	for id, rel := range paths {
		taken[pathKey(rel)] = id
	}

	var books [][]sqlc.ListOrganiseFilesRow
	for _, f := range files {
		if n := len(books); n > 0 && books[n-1][0].BookID == f.BookID {
			books[n-1] = append(books[n-1], f)
		} else {
			books = append(books, []sqlc.ListOrganiseFilesRow{f})
		}
	}
	sortKey := func(b []sqlc.ListOrganiseFilesRow) string {
		f := b[0]
		idx := ""
		if f.SeriesIndex != nil {
			idx = fmt.Sprintf("%012.3f", *f.SeriesIndex)
		}
		return strings.Join([]string{pathKey(f.AuthorSort), pathKey(f.Series), idx, pathKey(f.Title), f.BookID.String()}, "\x00")
	}
	slices.SortStableFunc(books, func(a, b []sqlc.ListOrganiseFilesRow) int { return strings.Compare(sortKey(a), sortKey(b)) })

	plan.Books = len(books)
	for _, book := range books {
		audio := 0
		for _, f := range book {
			if f.Kind == "audio" && f.MissingAt == nil {
				audio++
			}
		}
		width := max(2, len(strconv.Itoa(audio)))
		part := 0
		for _, f := range book {
			plan.Files++
			if f.MissingAt != nil {
				plan.Skipped = append(plan.Skipped, OrganiseSkip{f.FileID, f.BookID, f.Title, f.RelPath, SkipMissing})
				continue
			}
			layout := p.Layout(valuesOf(f))
			if f.Kind == "audio" && audio > 1 {
				part++
				layout = layout.Part(part, width)
			}
			var target string
			collided := false
			for n := 1; ; n++ {
				rel, ok := layout.Rel(n)
				if !ok {
					break
				}
				if owner, used := taken[pathKey(rel)]; used {
					if owner == f.FileID {
						target, collided = rel, n > 1
						break
					}
					continue
				}
				if onDisk(rel) {
					continue
				}
				target, collided = rel, n > 1
				taken[pathKey(rel)] = f.FileID
				break
			}
			if target == "" {
				plan.Skipped = append(plan.Skipped, OrganiseSkip{f.FileID, f.BookID, f.Title, f.RelPath, SkipTooLong})
				continue
			}
			if collided {
				plan.Collisions++
			}
			if target == f.RelPath {
				plan.Unchanged++
				continue
			}
			plan.Moves = append(plan.Moves, OrganiseMove{
				FileID: f.FileID, BookID: f.BookID, Title: f.Title, From: f.RelPath, To: target, Collision: collided,
			})
		}
	}
	return plan
}

// organiseAllowed says whether GOtome may rename the files of the library.
func organiseAllowed(l sqlc.GetOrganiseLibraryRow) bool {
	return l.Writable && (l.Mode == library.ModeManaged || l.OrganiseEnabled)
}

// OrganiseAllowed says why the files of a library may not be organised,
// or "" if they may.
func OrganiseAllowed(l sqlc.GetOrganiseLibraryRow) string {
	switch {
	case !l.Writable:
		return "read-only"
	case l.Mode != library.ModeManaged && !l.OrganiseEnabled:
		return "off"
	}
	return ""
}

func (s *Service) organiseLibrary(ctx context.Context, id uuid.UUID) (sqlc.GetOrganiseLibraryRow, error) {
	l, err := sqlc.New(s.pool).GetOrganiseLibrary(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, library.ErrNotFound
	}
	return l, err
}

func parsePattern(l sqlc.GetOrganiseLibraryRow, text string) (organise.Pattern, error) {
	if text == "" {
		text = organise.PatternOr(l.OrganisePattern)
	}
	p, err := organise.Parse(text)
	if err != nil {
		return p, &PatternError{Reason: err.Error()}
	}
	return p, nil
}

// PlanOrganise says what the pattern, or the library's own when it is
// empty, would do to the library's files. It changes nothing.
func (s *Service) PlanOrganise(ctx context.Context, libraryID uuid.UUID, pattern string) (OrganisePlan, sqlc.GetOrganiseLibraryRow, error) {
	l, err := s.organiseLibrary(ctx, libraryID)
	if err != nil {
		return OrganisePlan{}, l, err
	}
	p, err := parsePattern(l, pattern)
	if err != nil {
		return OrganisePlan{}, l, err
	}
	plan, err := s.planFor(ctx, l, p)
	return plan, l, err
}

func (s *Service) planFor(ctx context.Context, l sqlc.GetOrganiseLibraryRow, p organise.Pattern) (OrganisePlan, error) {
	q := sqlc.New(s.pool)
	files, err := q.ListOrganiseFiles(ctx, l.ID)
	if err != nil {
		return OrganisePlan{}, err
	}
	rows, err := q.ListLibraryFilePaths(ctx, l.ID)
	if err != nil {
		return OrganisePlan{}, err
	}
	paths := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		paths[r.ID] = r.RelPath
	}
	onDisk := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(l.RootPath, filepath.FromSlash(rel)))
		return err == nil
	}
	return planOrganise(p, files, paths, onDisk), nil
}

// organiseKind names the job in the queue. It is stored with every job, so
// it stays.
const organiseKind = "ingest.organise_files"

// OrganiseArgs is the job that applies a run's pattern to its library.
type OrganiseArgs struct {
	RunID uuid.UUID `json:"runId"`
}

func (OrganiseArgs) Kind() string { return organiseKind }

// RequestOrganise queues a run that renames the library's files by the
// pattern, or by the library's own when it is empty, and returns it. The
// run is a curation run of kind organise, so that it can be undone.
func (s *Service) RequestOrganise(ctx context.Context, by uuid.UUID, libraryID uuid.UUID, pattern string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrary(ctx, libraryID); err != nil {
			return err
		}
		l, err := q.GetOrganiseLibrary(ctx, libraryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return library.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !organiseAllowed(l) {
			return ErrOrganiseOff
		}
		p, err := parsePattern(l, pattern)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('organise:' || $1::text, 0))`, libraryID.String()); err != nil {
			return err
		}
		busy, err := q.HasActiveOrganiseRun(ctx, &libraryID)
		if err != nil {
			return err
		}
		if busy {
			return ErrOrganiseBusy
		}
		text := p.String()
		id, err = q.CreateOrganiseRun(ctx, sqlc.CreateOrganiseRunParams{StartedBy: &by, LibraryID: &libraryID, Pattern: &text})
		if err != nil {
			return err
		}
		_, err = s.Queue.InsertTx(ctx, tx, OrganiseArgs{RunID: id}, jobs.InsertOpts{
			Queue: jobs.QueueMetadata, MaxAttempts: organiseAttempts,
		})
		return err
	})
	return id, err
}

const (
	organiseAttempts = 5
	organiseBudget   = 25 * time.Second
	// organiseMaxFailures ends a run that keeps failing, as a folder that
	// cannot be written would fail every book.
	organiseMaxFailures = 20
)
