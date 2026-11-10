package readimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

// What the preview found for a row.
const (
	MatchBook      = "book"
	MatchNew       = "new"
	MatchAmbiguous = "ambiguous"
	MatchSkipped   = "skipped"
)

// What an import came to for a row.
const (
	OutcomeCreated   = "created"
	OutcomeChanged   = "changed"
	OutcomeUnchanged = "unchanged"
	OutcomeSkipped   = "skipped"
	OutcomeFailed    = "failed"
)

// The states of an import.
const (
	StatePreview = "preview"
	StateRunning = "running"
	StateDone    = "done"
)

const (
	budget      = 30 * time.Second
	batch       = 20
	jobAttempts = 10
	jobKind     = "readimport.run"
	// listed is how many skipped rows a preview names.
	listed = 100
)

var (
	// ErrNotFound is an import that does not exist or is somebody else's.
	ErrNotFound = errors.New("no such import")
	// ErrStarted is an import that was started already.
	ErrStarted = errors.New("the import was started already")
	// ErrBadChoice is a choice for a row that is not ambiguous or not
	// among the books it may be.
	ErrBadChoice = errors.New("that is not a choice for this row")
)

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service imports reading histories.
type Service struct {
	pool  *pgxpool.Pool
	books *catalog.Service
	log   *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
	// Lookup, when set, is called in the transaction that makes a book for
	// a row, to ask for the book to be looked up: enrich.Service.EnqueueTx.
	Lookup func(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool, books *catalog.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, books: books, log: log}
}

// Ambiguity is a row that may be several books of the libraries the person
// sees, for them to pick from or skip.
type Ambiguity struct {
	Row        int
	Title      string
	Authors    []string
	Candidates []catalog.Summary
	// Choice is the book picked; Skip says the row is left out.
	Choice *uuid.UUID
	Skip   bool
}

// Skipped is a row nothing can be imported from.
type Skipped struct {
	Row     int
	Title   string
	Message string
}

// Import is an import and what it found or came to.
type Import struct {
	ID      uuid.UUID
	Source  string
	State   string
	Library uuid.UUID
	// Overwrite is whether what the person already set is replaced.
	Overwrite bool
	CreatedAt time.Time
	Total     int
	// Matched, New, Ambiguous and Skipped count the rows the preview found.
	Matched   int
	New       int
	Ambiguous int
	Skipped   int
	// RoundedRatings counts ratings of a fraction of a star, rounded to
	// whole stars.
	RoundedRatings int
	Ambiguities    []Ambiguity
	SkippedRows    []Skipped
	// Outcomes counts the rows by what the import came to, once it runs.
	Outcomes map[string]int
}

// Create reads an export and records the import as a preview: nothing of the
// person's reading is written. The library takes the placeholders of the
// books that are new; left out, it is the one they chose before, the only one
// they see, or the one that holds most of the books the export names, and
// catalog.ErrNeedsLibrary says there is none to choose. ErrUnknownFormat is
// a file that is neither a Goodreads nor a StoryGraph export.
func (s *Service) Create(ctx context.Context, scope library.Scope, data []byte, libraryID *uuid.UUID) (Import, error) {
	source, rows, err := Parse(data)
	if err != nil {
		return Import{}, err
	}
	q := sqlc.New(s.pool)
	found := make([]matched, len(rows))
	var held []uuid.UUID
	for i, row := range rows {
		if row.Skip != "" {
			found[i] = matched{kind: MatchSkipped}
			continue
		}
		if found[i], err = match(ctx, q, scope, row); err != nil {
			return Import{}, err
		}
		if found[i].book != nil {
			held = append(held, *found[i].book)
		}
	}
	var target uuid.UUID
	if libraryID != nil {
		visible, err := q.IsLibraryVisible(ctx, sqlc.IsLibraryVisibleParams{ID: *libraryID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return Import{}, err
		}
		if !visible {
			return Import{}, catalog.ErrNotFound
		}
		target = *libraryID
		if err := s.books.RememberHistoryLibrary(ctx, scope, target); err != nil {
			return Import{}, err
		}
	} else if target, err = s.books.HistoryLibrary(ctx, scope, nil, ""); errors.Is(err, catalog.ErrNeedsLibrary) && len(held) > 0 {
		target, err = q.LibraryHoldingMostOf(ctx, held)
	}
	if err != nil {
		return Import{}, err
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.DeleteStaleReadingImports(ctx, scope.Viewer); err != nil {
			return err
		}
		var err error
		if id, err = q.CreateReadingImport(ctx, sqlc.CreateReadingImportParams{
			UserID: scope.Viewer, Source: source, LibraryID: target, SeesAll: scope.SeesAll,
		}); err != nil {
			return err
		}
		for i, row := range rows {
			m := found[i]
			var status *string
			if row.Status != "" {
				status = &row.Status
			}
			var rating *int16
			if row.Rating > 0 {
				rating = &row.Rating
			}
			err := q.AddReadingImportRow(ctx, sqlc.AddReadingImportRowParams{
				ImportID: id, RowNo: int32(row.No), Title: row.Title, Authors: nonNil(row.Authors), Isbns: nonNil(row.ISBNs),
				Status: status, Rating: rating, Rounded: row.Rounded, ReadOn: row.ReadOn, Shelves: nonNil(row.Shelves),
				Review: row.Review, Match: m.kind, BookID: m.book, Candidates: append([]uuid.UUID{}, m.candidates...),
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Import{}, err
	}
	return s.Get(ctx, scope, id)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type matched struct {
	kind       string
	book       *uuid.UUID
	candidates []uuid.UUID
}

// match finds the books of the libraries the scope sees that a row may be:
// by ISBN, else by title and an author.
func match(ctx context.Context, q *sqlc.Queries, scope library.Scope, row Row) (matched, error) {
	keys := []string{}
	for _, a := range row.Authors {
		if k := catalog.PersonKey(a); k != "" {
			keys = append(keys, k)
		}
	}
	found, err := q.MatchImportBooks(ctx, sqlc.MatchImportBooksParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Isbns: nonNil(row.ISBNs),
		TitleKey: catalog.Key(row.MatchTitle()), PersonKeys: keys,
	})
	if err != nil {
		return matched{}, err
	}
	var byISBN, byTitle []uuid.UUID
	for _, f := range found {
		if f.ByIsbn {
			byISBN = append(byISBN, f.ID)
		} else {
			byTitle = append(byTitle, f.ID)
		}
	}
	pick := byISBN
	if len(pick) == 0 {
		pick = byTitle
	}
	switch len(pick) {
	case 0:
		return matched{kind: MatchNew}, nil
	case 1:
		return matched{kind: MatchBook, book: &pick[0]}, nil
	}
	return matched{kind: MatchAmbiguous, candidates: pick}, nil
}

// Get returns an import the scope's user made, with the books they may
// still see among its candidates.
func (s *Service) Get(ctx context.Context, scope library.Scope, id uuid.UUID) (Import, error) {
	q := sqlc.New(s.pool)
	imp, err := q.GetReadingImport(ctx, sqlc.GetReadingImportParams{ID: id, UserID: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return Import{}, ErrNotFound
	}
	if err != nil {
		return Import{}, err
	}
	rows, err := q.ListReadingImportRows(ctx, id)
	if err != nil {
		return Import{}, err
	}
	out := Import{
		ID: imp.ID, Source: imp.Source, State: StatePreview, Library: imp.LibraryID, Overwrite: imp.Overwrite,
		CreatedAt: imp.CreatedAt, Total: len(rows), Ambiguities: []Ambiguity{}, SkippedRows: []Skipped{}, Outcomes: map[string]int{},
	}
	switch {
	case imp.FinishedAt != nil:
		out.State = StateDone
	case imp.StartedAt != nil:
		out.State = StateRunning
	}
	for _, r := range rows {
		if r.Rounded {
			out.RoundedRatings++
		}
		if r.Outcome != nil {
			out.Outcomes[*r.Outcome]++
		}
		switch r.Match {
		case MatchBook:
			out.Matched++
		case MatchNew:
			out.New++
		case MatchSkipped:
			out.Skipped++
			if len(out.SkippedRows) < listed {
				out.SkippedRows = append(out.SkippedRows, Skipped{Row: int(r.RowNo), Title: r.Title, Message: skipMessage(r)})
			}
		case MatchAmbiguous:
			out.Ambiguous++
			summaries, err := s.books.Summaries(ctx, scope, r.Candidates)
			if err != nil {
				return Import{}, err
			}
			out.Ambiguities = append(out.Ambiguities, Ambiguity{
				Row: int(r.RowNo), Title: r.Title, Authors: r.Authors, Candidates: summaries, Choice: r.BookID, Skip: r.Skip,
			})
		}
	}
	return out, nil
}

func skipMessage(r sqlc.ReadingImportRow) string {
	if strings.TrimSpace(r.Title) == "" {
		return "The row has no title."
	}
	return "The row's shelf is none GOtome knows."
}

// Choose answers an ambiguous row: the book it is, or that it is left out.
// A book that is not among its candidates is an ErrBadChoice.
func (s *Service) Choose(ctx context.Context, scope library.Scope, id uuid.UUID, row int, book *uuid.UUID, skip bool) error {
	q := sqlc.New(s.pool)
	imp, err := q.GetReadingImport(ctx, sqlc.GetReadingImportParams{ID: id, UserID: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if imp.StartedAt != nil {
		return ErrStarted
	}
	if (book == nil) == !skip {
		return ErrBadChoice
	}
	n, err := q.ChooseReadingImportRow(ctx, sqlc.ChooseReadingImportRowParams{
		ImportID: id, RowNo: int32(row), BookID: book, Skip: skip,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrBadChoice
	}
	return nil
}

// Start queues the job that writes a previewed import.
func (s *Service) Start(ctx context.Context, scope library.Scope, id uuid.UUID, overwrite bool) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		imp, err := q.GetReadingImport(ctx, sqlc.GetReadingImportParams{ID: id, UserID: scope.Viewer})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		n, err := q.StartReadingImport(ctx, sqlc.StartReadingImportParams{ID: id, Overwrite: overwrite})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrStarted
		}
		_, err = s.Queue.InsertTx(ctx, tx, Args{ImportID: id, LibraryID: imp.LibraryID}, jobs.InsertOpts{
			Queue: jobs.QueueDefault, MaxAttempts: jobAttempts,
		})
		return err
	})
}

// Run writes the rows of a started import whose turn has not come, for a
// while, and reports whether it got to the end. A row's outcome is
// recorded in the transaction that writes it, so a run cut off and run
// again does each row once.
func (s *Service) Run(ctx context.Context, id uuid.UUID) (bool, error) {
	q := sqlc.New(s.pool)
	imp, err := q.GetReadingImportAny(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (imp.StartedAt == nil || imp.FinishedAt != nil) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	started := time.Now()
	for time.Since(started) < budget {
		next, err := q.NextReadingImportRows(ctx, sqlc.NextReadingImportRowsParams{ImportID: id, Limit: batch})
		if err != nil {
			return false, err
		}
		if len(next) == 0 {
			_, err := q.FinishReadingImport(ctx, id)
			return true, err
		}
		for _, row := range next {
			if err := s.one(ctx, imp, row); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

var seriesPlace = regexp.MustCompile(`\(([^()#]*?),?\s*#(\d+(?:\.\d+)?)\)\s*$`)

// one writes one row and records what it came to. It returns an error only
// for what stops the whole run.
func (s *Service) one(ctx context.Context, imp sqlc.ReadingImport, row sqlc.ReadingImportRow) error {
	scope := library.Scope{Viewer: imp.UserID, SeesAll: imp.SeesAll}
	var outcome, message string
	var book *uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		outcome, message, book, err = s.write(ctx, tx, scope, imp, row)
		if err != nil {
			return err
		}
		return sqlc.New(tx).SetReadingImportRowOutcome(ctx, sqlc.SetReadingImportRowOutcomeParams{
			ImportID: imp.ID, RowNo: row.RowNo, Outcome: &outcome, Message: message, BookID: book,
		})
	})
	if err == nil || ctx.Err() != nil {
		return err
	}
	s.log.Warn("reading import", "import", imp.ID, "row", row.RowNo, "error", err)
	failed := OutcomeFailed
	return sqlc.New(s.pool).SetReadingImportRowOutcome(ctx, sqlc.SetReadingImportRowOutcomeParams{
		ImportID: imp.ID, RowNo: row.RowNo, Outcome: &failed, Message: "The row could not be imported.",
	})
}

func (s *Service) write(ctx context.Context, tx pgx.Tx, scope library.Scope, imp sqlc.ReadingImport, row sqlc.ReadingImportRow) (outcome, message string, book *uuid.UUID, err error) {
	q := sqlc.New(tx)
	if row.Match == MatchSkipped || row.Skip || row.Status == nil {
		return OutcomeSkipped, "Left out.", nil, nil
	}
	if row.Match == MatchAmbiguous && row.BookID == nil {
		return OutcomeSkipped, "Several books fit and none was chosen.", nil, nil
	}
	if err := q.HoldLibrary(ctx, imp.LibraryID); err != nil {
		return "", "", nil, err
	}
	var id uuid.UUID
	if row.BookID != nil {
		visible, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: []uuid.UUID{*row.BookID}, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return "", "", nil, err
		}
		if len(visible) > 0 {
			id = visible[0]
		}
	}
	created := false
	if id == uuid.Nil {
		r := Row{Title: row.Title, Authors: row.Authors, ISBNs: row.Isbns}
		m, err := match(ctx, q, scope, r)
		if err != nil {
			return "", "", nil, err
		}
		switch m.kind {
		case MatchBook:
			id = *m.book
		case MatchAmbiguous:
			return OutcomeSkipped, "Several books fit and none was chosen.", nil, nil
		default:
			visible, err := q.IsLibraryVisible(ctx, sqlc.IsLibraryVisibleParams{ID: imp.LibraryID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
			if err != nil {
				return "", "", nil, err
			}
			if !visible {
				return OutcomeFailed, "The library for new books is gone, or no longer one you see.", nil, nil
			}
			if id, err = createPlaceholder(ctx, tx, imp.LibraryID, row); err != nil {
				return "", "", nil, err
			}
			created = true
			if s.Lookup != nil {
				if err := s.Lookup(ctx, tx, id); err != nil {
					return "", "", nil, err
				}
			}
		}
	}
	in := catalog.ImportedReading{Status: *row.Status, Note: row.Review, Overwrite: imp.Overwrite}
	if row.Rating != nil {
		in.Rating = *row.Rating
	}
	if *row.Status == catalog.StatusCompleted {
		in.FinishedOn = row.ReadOn
	}
	changed, err := catalog.ImportReadingTx(ctx, tx, scope.Viewer, id, in)
	if err != nil {
		return "", "", nil, err
	}
	for _, name := range row.Shelves {
		added, err := addToShelf(ctx, q, scope, name, id)
		if err != nil {
			return "", "", nil, err
		}
		changed = changed || added
	}
	switch {
	case created:
		return OutcomeCreated, "", &id, nil
	case changed:
		return OutcomeChanged, "", &id, nil
	}
	return OutcomeUnchanged, "", &id, nil
}

func createPlaceholder(ctx context.Context, tx pgx.Tx, libraryID uuid.UUID, row sqlc.ReadingImportRow) (uuid.UUID, error) {
	r := Row{Title: row.Title}
	in := catalog.NewBook{LibraryID: libraryID, Title: r.MatchTitle(), Source: "import"}
	if m := seriesPlace.FindStringSubmatch(row.Title); m != nil {
		in.Series = strings.TrimSpace(m[1])
		if n, err := strconv.ParseFloat(m[2], 64); err == nil && in.Series != "" {
			in.SeriesIndex = &n
		}
	}
	for _, a := range row.Authors {
		in.Contributors = append(in.Contributors, catalog.NewContributor{Name: a, Role: catalog.RoleAuthor})
	}
	for _, isbn := range row.Isbns {
		in.Identifiers = append(in.Identifiers, catalog.Identifier{Type: catalog.IDISBN, Value: isbn})
	}
	if in.Title == "" {
		return uuid.Nil, fmt.Errorf("a book needs a title")
	}
	return catalog.CreatePlaceholderTx(ctx, tx, in)
}

// addToShelf puts a book into the person's collection of that name, which
// is made when they have none, unless it is full.
func addToShelf(ctx context.Context, q *sqlc.Queries, scope library.Scope, name string, book uuid.UUID) (bool, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return false, nil
	}
	id, err := q.FindOwnedCollection(ctx, sqlc.FindOwnedCollectionParams{OwnerID: scope.Viewer, Column2: name})
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = q.CreateCollection(ctx, sqlc.CreateCollectionParams{OwnerID: scope.Viewer, Name: name, Visibility: "private"})
	}
	if err != nil {
		return false, err
	}
	if n, err := q.CountCollectionItems(ctx, id); err != nil || n >= shelves.MaxBooks {
		return false, err
	}
	added, err := q.AddCollectionItems(ctx, sqlc.AddCollectionItemsParams{
		CollectionID: id, Ids: []uuid.UUID{book}, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil || added == 0 {
		return false, err
	}
	return true, q.TouchCollection(ctx, id)
}

// Args is the job that writes an import. It names the library, so that the
// jobs page shows it only to those who see it.
type Args struct {
	ImportID  uuid.UUID `json:"importId"`
	LibraryID uuid.UUID `json:"libraryId"`
}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (Args) Kind() string { return jobKind }

// Worker runs Args.
type Worker struct {
	river.WorkerDefaults[Args]
	Service *Service
}

func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	done, err := w.Service.Run(ctx, job.Args.ImportID)
	switch {
	case err != nil:
		return err
	case !done:
		return river.JobSnooze(time.Second)
	}
	return nil
}
