package calibre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// What an import came to for a book.
const (
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

// Source is recorded in field_sources for what Calibre said. It is worth
// more than what a file says and less than anything a person set.
const Source = catalog.SourceCalibre

const (
	budget      = 30 * time.Second
	batch       = 20
	jobAttempts = 10
	jobKind     = "calibre.import"
	// listed is how many skipped or failed books an import names.
	listed = 100
	// maxCover bounds a cover file; Calibre's are a few hundred kilobytes.
	maxCover = 20 << 20
)

var (
	// ErrNotFound is an import that does not exist or is somebody else's.
	ErrNotFound = errors.New("no such import")
	// ErrStarted is an import that was started already.
	ErrStarted = errors.New("the import was started already")
	// ErrNotExternal is a library GOtome manages, which is no Calibre folder.
	ErrNotExternal = errors.New("only an external library can be a Calibre library")
	// ErrBadColumns is a choice for a custom column that does not exist or
	// that its datatype does not allow.
	ErrBadColumns = errors.New("that is not a choice for this column")
)

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Editor changes how a book is described and follows the change up:
// ingest.Service.
type Editor interface {
	EditTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, e catalog.Edit) error
}

// Service imports Calibre libraries.
type Service struct {
	pool   *pgxpool.Pool
	editor Editor
	covers *covers.Store
	log    *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool, editor Editor, store *covers.Store, log *slog.Logger) *Service {
	return &Service{pool: pool, editor: editor, covers: store, log: log}
}

// ColumnInfo is a custom column and what it may become.
type ColumnInfo struct {
	Label    string   `json:"label"`
	Name     string   `json:"name"`
	Datatype string   `json:"datatype"`
	Books    int      `json:"books"`
	Choices  []string `json:"choices"`
}

// Summary is what the preview counted.
type Summary struct {
	// Books are the books Calibre lists; Ready are those with a file in
	// the library, Skipped those without.
	Books   int `json:"books"`
	Ready   int `json:"ready"`
	Skipped int `json:"skipped"`
	// Formats counts the files by format; MissingFiles counts the files of
	// Calibre's that the library lacks, whether or not the book has others.
	Formats      map[string]int `json:"formats"`
	MissingFiles int            `json:"missingFiles"`
	Covers       int            `json:"covers"`
	Series       int            `json:"series"`
	Tags         int            `json:"tags"`
	Rated        int            `json:"rated"`
	Columns      []ColumnInfo   `json:"columns"`
}

// Skipped is a book nothing was imported for.
type Skipped struct {
	CalibreID int64
	Title     string
	Message   string
}

// Import is an import and what it found or came to.
type Import struct {
	ID        uuid.UUID
	State     string
	Library   uuid.UUID
	Ratings   bool
	Columns   map[string]string
	CreatedAt time.Time
	Summary   Summary
	// Problems are books left out or failed, at most `listed`.
	Problems []Skipped
	// Outcomes counts the books by what the import came to, once it runs.
	Outcomes map[string]int
}

// Preview reads the Calibre library that the library at libraryID is and
// records what an import of it would do. Nothing in GOtome is written but
// the preview.
func (s *Service) Preview(ctx context.Context, scope library.Scope, libraryID uuid.UUID) (Import, error) {
	q := sqlc.New(s.pool)
	lib, err := q.GetVisibleLibrary(ctx, sqlc.GetVisibleLibraryParams{ID: libraryID, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return Import{}, library.ErrNotFound
	}
	if err != nil {
		return Import{}, err
	}
	if lib.Mode != library.ModeExternal {
		return Import{}, ErrNotExternal
	}
	reader, err := Open(lib.RootPath)
	if err != nil {
		return Import{}, err
	}
	defer reader.Close()

	columns, err := reader.Columns(ctx, true)
	if err != nil {
		return Import{}, fmt.Errorf("%w: %v", ErrNotCalibre, err)
	}
	ids, err := reader.IDs(ctx)
	if err != nil {
		return Import{}, fmt.Errorf("%w: %v", ErrNotCalibre, err)
	}
	sum := Summary{Books: len(ids), Formats: map[string]int{}}
	for _, c := range columns {
		sum.Columns = append(sum.Columns, ColumnInfo{Label: c.Label, Name: c.Name, Datatype: c.Datatype, Books: c.Books, Choices: Choices(c.Datatype)})
	}
	series, tags := map[string]bool{}, map[string]bool{}
	var rows []sqlc.AddCalibreImportBooksParams
	for part := range slices.Chunk(ids, 1000) {
		books, err := reader.Books(ctx, part, nil)
		if err != nil {
			return Import{}, fmt.Errorf("%w: %v", ErrNotCalibre, err)
		}
		for _, b := range books {
			found, missing, err := s.filesOf(ctx, q, libraryID, b)
			if err != nil {
				return Import{}, err
			}
			rows = append(rows, sqlc.AddCalibreImportBooksParams{CalibreID: b.ID, Title: b.Title, Found: int32(len(found)), Missing: int32(missing)})
			sum.MissingFiles += missing
			if len(found) == 0 {
				sum.Skipped++
				continue
			}
			sum.Ready++
			for _, f := range b.Formats {
				sum.Formats[f.Format]++
			}
			if b.HasCover {
				sum.Covers++
			}
			if b.Series != "" {
				series[catalog.Key(b.Series)] = true
			}
			for _, t := range b.Tags {
				tags[catalog.Key(t)] = true
			}
			if b.Rating > 0 {
				sum.Rated++
			}
		}
	}
	sum.Series, sum.Tags = len(series), len(tags)

	summary, err := json.Marshal(sum)
	if err != nil {
		return Import{}, err
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.DeleteStaleCalibreImports(ctx, scope.Viewer); err != nil {
			return err
		}
		var err error
		if id, err = q.CreateCalibreImport(ctx, sqlc.CreateCalibreImportParams{
			UserID: scope.Viewer, LibraryID: libraryID, SeesAll: scope.SeesAll, Summary: summary,
		}); err != nil {
			return err
		}
		for i := range rows {
			rows[i].ImportID = id
		}
		_, err = q.AddCalibreImportBooks(ctx, rows)
		return err
	})
	if err != nil {
		return Import{}, err
	}
	return s.Get(ctx, scope, id)
}

// filesOf finds the files of a Calibre book in the library: exactly the
// path Calibre says, <books.path>/<data.name>.<format>.
func (s *Service) filesOf(ctx context.Context, q *sqlc.Queries, libraryID uuid.UUID, b Book) (found []sqlc.ListFilesByRelPathRow, missing int, err error) {
	paths := make([]string, 0, len(b.Formats))
	for _, f := range b.Formats {
		paths = append(paths, path.Join(b.Path, f.Name+"."+f.Format))
	}
	if len(paths) == 0 {
		return nil, 0, nil
	}
	found, err = q.ListFilesByRelPath(ctx, sqlc.ListFilesByRelPathParams{LibraryID: libraryID, Paths: paths})
	if err != nil {
		return nil, 0, err
	}
	live := found[:0:0]
	for _, f := range found {
		if f.MissingAt == nil {
			live = append(live, f)
		}
	}
	return live, len(paths) - len(live), nil
}

// Get returns an import the scope's user made.
func (s *Service) Get(ctx context.Context, scope library.Scope, id uuid.UUID) (Import, error) {
	q := sqlc.New(s.pool)
	imp, err := q.GetCalibreImport(ctx, sqlc.GetCalibreImportParams{ID: id, UserID: scope.Viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return Import{}, ErrNotFound
	}
	if err != nil {
		return Import{}, err
	}
	rows, err := q.ListCalibreImportBooks(ctx, id)
	if err != nil {
		return Import{}, err
	}
	out := Import{
		ID: id, State: StatePreview, Library: imp.LibraryID, Ratings: imp.Ratings, CreatedAt: imp.CreatedAt,
		Columns: map[string]string{}, Problems: []Skipped{}, Outcomes: map[string]int{},
	}
	if err := json.Unmarshal(imp.Summary, &out.Summary); err != nil {
		return Import{}, err
	}
	if err := json.Unmarshal(imp.Columns, &out.Columns); err != nil {
		return Import{}, err
	}
	switch {
	case imp.FinishedAt != nil:
		out.State = StateDone
	case imp.StartedAt != nil:
		out.State = StateRunning
	}
	for _, r := range rows {
		if r.Outcome != nil {
			out.Outcomes[*r.Outcome]++
		}
		failed := r.Outcome != nil && *r.Outcome == OutcomeFailed
		if (r.Found == 0 || failed) && len(out.Problems) < listed {
			message := r.Message
			if r.Found == 0 && message == "" {
				message = noFile(int(r.Missing))
			}
			out.Problems = append(out.Problems, Skipped{CalibreID: r.CalibreID, Title: r.Title, Message: message})
		}
	}
	return out, nil
}

func noFile(missing int) string {
	if missing == 0 {
		return "Calibre lists no file for this book."
	}
	return "None of its files is in the library: scan it, or they are gone."
}

// Start queues the job that imports a previewed library. ratings makes
// Calibre's ratings the caller's own; columns says per custom column (by
// label) what it becomes, and a column left out is ignored.
func (s *Service) Start(ctx context.Context, scope library.Scope, id uuid.UUID, ratings bool, columns map[string]string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		imp, err := q.GetCalibreImport(ctx, sqlc.GetCalibreImportParams{ID: id, UserID: scope.Viewer})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var sum Summary
		if err := json.Unmarshal(imp.Summary, &sum); err != nil {
			return err
		}
		chosen := map[string]string{}
		for label, choice := range columns {
			at := slices.IndexFunc(sum.Columns, func(c ColumnInfo) bool { return c.Label == label })
			if at < 0 || !slices.Contains(sum.Columns[at].Choices, choice) {
				return ErrBadColumns
			}
			if choice != ChoiceIgnore {
				chosen[label] = choice
			}
		}
		encoded, err := json.Marshal(chosen)
		if err != nil {
			return err
		}
		n, err := q.StartCalibreImport(ctx, sqlc.StartCalibreImportParams{ID: id, Ratings: ratings, Columns: encoded})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrStarted
		}
		_, err = s.Queue.InsertTx(ctx, tx, Args{ImportID: id, LibraryID: imp.LibraryID}, jobs.InsertOpts{
			Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts,
		})
		return err
	})
}

// Run imports the books of a started import whose turn has not come, for a
// while, and reports whether it got to the end. A book's outcome is recorded
// in the transaction that writes it, so a run cut off and run again does
// each book once.
func (s *Service) Run(ctx context.Context, id uuid.UUID) (bool, error) {
	q := sqlc.New(s.pool)
	imp, err := q.GetCalibreImportAny(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (imp.StartedAt == nil || imp.FinishedAt != nil) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	lib, err := q.GetLibraryRoot(ctx, imp.LibraryID)
	if err != nil {
		return false, err
	}
	opts := Options{Ratings: imp.Ratings, Columns: map[string]string{}}
	if err := json.Unmarshal(imp.Columns, &opts.Columns); err != nil {
		return false, err
	}
	reader, err := Open(lib)
	if err != nil {
		// A folder that has gone away is no reason to retry for ever.
		s.log.Warn("calibre import: cannot open the library", "import", id, "error", err)
		return s.failAll(ctx, imp, "metadata.db could not be opened.")
	}
	defer reader.Close()
	columns, err := reader.Columns(ctx, false)
	if err != nil {
		return false, err
	}
	started := time.Now()
	for time.Since(started) < budget {
		next, err := q.NextCalibreImportBooks(ctx, sqlc.NextCalibreImportBooksParams{ImportID: id, Limit: batch})
		if err != nil {
			return false, err
		}
		if len(next) == 0 {
			_, err := q.FinishCalibreImport(ctx, id)
			return true, err
		}
		ids := make([]int64, len(next))
		for i, r := range next {
			ids[i] = r.CalibreID
		}
		books, err := reader.Books(ctx, ids, columns)
		if err != nil {
			return false, err
		}
		byID := map[int64]Book{}
		for _, b := range books {
			byID[b.ID] = b
		}
		for _, row := range next {
			if err := s.one(ctx, imp, lib, row, byID[row.CalibreID], columns, opts); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func (s *Service) failAll(ctx context.Context, imp sqlc.CalibreImport, message string) (bool, error) {
	q := sqlc.New(s.pool)
	for {
		next, err := q.NextCalibreImportBooks(ctx, sqlc.NextCalibreImportBooksParams{ImportID: imp.ID, Limit: 500})
		if err != nil {
			return false, err
		}
		if len(next) == 0 {
			_, err := q.FinishCalibreImport(ctx, imp.ID)
			return true, err
		}
		for _, row := range next {
			failed := OutcomeFailed
			if err := q.SetCalibreImportBookOutcome(ctx, sqlc.SetCalibreImportBookOutcomeParams{
				ImportID: imp.ID, CalibreID: row.CalibreID, Outcome: &failed, Message: message,
			}); err != nil {
				return false, err
			}
		}
	}
}

// one imports one book and records what it came to. It returns an error
// only for what stops the whole run.
func (s *Service) one(ctx context.Context, imp sqlc.CalibreImport, root string, row sqlc.CalibreImportBook, b Book, columns []Column, opts Options) error {
	var outcome, message string
	var book *uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		outcome, message, book, err = s.write(ctx, tx, imp, root, b, columns, opts)
		if err != nil {
			return err
		}
		return sqlc.New(tx).SetCalibreImportBookOutcome(ctx, sqlc.SetCalibreImportBookOutcomeParams{
			ImportID: imp.ID, CalibreID: row.CalibreID, Outcome: &outcome, Message: message, BookID: book,
		})
	})
	if err == nil || ctx.Err() != nil {
		return err
	}
	s.log.Warn("calibre import", "import", imp.ID, "calibre book", row.CalibreID, "error", err)
	failed := OutcomeFailed
	return sqlc.New(s.pool).SetCalibreImportBookOutcome(ctx, sqlc.SetCalibreImportBookOutcomeParams{
		ImportID: imp.ID, CalibreID: row.CalibreID, Outcome: &failed, Message: "The book could not be imported.",
	})
}

// replaceable says whether a field with that source may be replaced by
// what Calibre says: it is empty, a guess, read from a file or Calibre's
// own. A person's value and a provider's stay.
func replaceable(source string) bool {
	switch source {
	case "", catalog.SourceFilename, catalog.SourceText, Source:
		return true
	}
	kind, _ := catalog.Provenance(source)
	return kind == catalog.ProvenanceFile
}

func (s *Service) write(ctx context.Context, tx pgx.Tx, imp sqlc.CalibreImport, root string, b Book, columns []Column, opts Options) (outcome, message string, book *uuid.UUID, err error) {
	if b.ID == 0 {
		return OutcomeSkipped, "The book is no longer in Calibre's library.", nil, nil
	}
	q := sqlc.New(tx)
	if err := q.HoldLibrary(ctx, imp.LibraryID); err != nil {
		return "", "", nil, err
	}
	files, missing, err := s.filesOf(ctx, q, imp.LibraryID, b)
	if err != nil {
		return "", "", nil, err
	}
	if len(files) == 0 {
		return OutcomeSkipped, noFile(missing), nil, nil
	}
	id := files[0].BookID
	for _, f := range files {
		if f.BookID != id {
			return OutcomeSkipped, "Its files lie in more than one book of GOtome.", nil, nil
		}
	}
	scope := library.Scope{Viewer: imp.UserID, SeesAll: imp.SeesAll}
	row, err := q.LockVisibleBook(ctx, sqlc.LockVisibleBookParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, "The book is gone, or no longer one you see.", nil, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	sources := map[string]string{}
	if err := json.Unmarshal(row.FieldSources, &sources); err != nil {
		return "", "", nil, err
	}
	m := Map(b, columns, opts)

	var kept []string
	edit := catalog.Edit{Source: Source}
	use := func(field string, says bool) bool {
		if !says {
			return false
		}
		if !replaceable(sources[field]) {
			kept = append(kept, field)
			return false
		}
		return true
	}
	if use(catalog.FieldTitle, m.Title != "") {
		edit.Title = &m.Title
	}
	if use(catalog.FieldDescription, m.Description != "") {
		edit.Description = &m.Description
	}
	if use(catalog.FieldLanguage, m.Language != "") {
		edit.Language = &m.Language
	}
	if use(catalog.FieldPublished, m.Published != "") {
		edit.Published = &m.Published
	}
	if use(catalog.FieldPublisher, m.Publisher != "") {
		edit.Publisher = &m.Publisher
	}
	if use(catalog.FieldSeries, m.Series != "") {
		edit.Series = &catalog.SeriesPlace{Name: m.Series, Index: m.SeriesIndex}
	}
	if use(catalog.FieldTags, len(m.Tags) > 0) {
		edit.Tags = &m.Tags
	}
	if use(catalog.FieldIdentifiers, len(m.Identifiers) > 0) {
		edit.Identifiers = &m.Identifiers
	}
	if use(catalog.FieldContributors, len(m.Contributors) > 0) {
		credits := m.Contributors
		others, err := q.ListBookContributors(ctx, id)
		if err != nil {
			return "", "", nil, err
		}
		for _, o := range others {
			if o.Role != catalog.RoleAuthor {
				credits = append(credits, catalog.NewContributor{Name: o.Name, SortName: o.SortName, Role: o.Role})
			}
		}
		edit.Contributors = &credits
	}
	if use(catalog.FieldCover, m.Cover != "") {
		if key, ok := s.cover(root, m.Cover); ok {
			edit.Cover = &key
		}
	}

	before, err := q.BookDescribedDigest(ctx, id)
	if err != nil {
		return "", "", nil, err
	}
	if edit.ChangesValues() {
		if err := s.editor.EditTx(ctx, tx, scope, id, edit); err != nil {
			var invalid catalog.EditError
			if errors.As(err, &invalid) {
				return OutcomeFailed, invalid.Error(), nil, nil
			}
			return "", "", nil, err
		}
	}
	after, err := q.BookDescribedDigest(ctx, id)
	if err != nil {
		return "", "", nil, err
	}
	changed := before != after
	if m.Reading.Rating > 0 || m.Reading.Status != "" {
		read, err := catalog.ImportReadingTx(ctx, tx, imp.UserID, id, m.Reading)
		if err != nil {
			return "", "", nil, err
		}
		changed = changed || read
	}
	if len(kept) > 0 {
		message = "Kept what is set in GOtome: " + joinFields(kept) + "."
	}
	if changed {
		return OutcomeChanged, message, &id, nil
	}
	return OutcomeUnchanged, message, &id, nil
}

func joinFields(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ", "
		}
		out += f
	}
	return out
}

// cover reads a cover file of the library, which must lie inside it, and
// stores it. A cover that is missing or unreadable is left out, not an error.
func (s *Service) cover(root, rel string) (string, bool) {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", false
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCover {
		return "", false
	}
	f, err := os.Open(full)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCover+1))
	if err != nil || len(data) > maxCover {
		return "", false
	}
	key, err := s.covers.Put(data)
	if err != nil {
		s.log.Warn("calibre import: cover", "path", rel, "error", err)
		return "", false
	}
	return key, true
}

// Args is the job that imports a Calibre library. It names the library, so
// that the jobs page shows it only to those who see it.
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
