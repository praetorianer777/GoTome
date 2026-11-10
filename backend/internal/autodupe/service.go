package autodupe

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reconcile"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// Rule is the rule of the runs this package makes.
const Rule = "auto-duplicates"

var (
	// ErrBusy is a run of the rules that has not finished.
	ErrBusy = errors.New("a run of the duplicate rules is going")
	// ErrClasses is no class, or one that is not handled by the rules.
	ErrClasses = errors.New("choose classes the rules handle")
)

// Queue is what runs jobs.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Texts reads settings.
type Texts interface {
	Text(ctx context.Context, key string) (string, error)
}

// Service finds the pairs of the classes and resolves them.
type Service struct {
	pool     *pgxpool.Pool
	books    *catalog.Service
	log      *slog.Logger
	Ingest   *ingest.Service
	Curation *curation.Service
	Queue    Queue
	Settings Texts
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool, books *catalog.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, books: books, log: log}
}

const (
	pageSize  = 500
	batchSize = 50
	// passBudget is how long a job works before it snoozes.
	passBudget  = 25 * time.Second
	jobAttempts = 5
)

// Pair is an open pair with its class.
type Pair struct {
	ID        uuid.UUID
	A, B      uuid.UUID
	LibraryID uuid.UUID
	Class     dedupe.Classification
	FoundAt   time.Time
	Evidence  []dedupe.Found
}

func pairOf(r sqlc.ListAutoPairsRow) Pair {
	p := Pair{ID: r.ID, A: r.BookA, B: r.BookB, LibraryID: r.LibraryID, FoundAt: r.FoundAt}
	for i, k := range r.Kinds {
		p.Evidence = append(p.Evidence, dedupe.Found{Kind: k, Detail: r.Details[i]})
	}
	p.Class = dedupe.Classify(p.Evidence)
	return p
}

// Pairs calls fn for each open pair of one library (or every library the
// scope sees) with evidence of shared text, after the pair given, in the
// order of their IDs, in pages, until fn says to stop.
func (s *Service) Pairs(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, after *uuid.UUID, fn func(Pair) (bool, error)) error {
	q := sqlc.New(s.pool)
	for {
		rows, err := q.ListAutoPairs(ctx, sqlc.ListAutoPairsParams{
			Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID, AfterID: after, MaxRows: pageSize,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			more, err := fn(pairOf(r))
			if err != nil || !more {
				return err
			}
			id := r.ID
			after = &id
		}
		if len(rows) < pageSize {
			return nil
		}
	}
}

// Counts says, per class the rules handle, how many open pairs the scope
// sees.
func (s *Service) Counts(ctx context.Context, scope library.Scope) (map[string]int, error) {
	out := map[string]int{}
	for _, c := range dedupe.AutoClasses {
		out[c] = 0
	}
	err := s.Pairs(ctx, scope, nil, nil, func(p Pair) (bool, error) {
		if _, ok := out[p.Class.Class]; ok {
			out[p.Class.Class]++
		}
		return true, nil
	})
	return out, err
}

// Started is a run that was queued.
type Started struct {
	RunID uuid.UUID
	// Pairs is how many open pairs of the classes it starts with.
	Pairs int
}

func checkClasses(classes []string) error {
	if len(classes) == 0 {
		return ErrClasses
	}
	for _, c := range classes {
		if !slices.Contains(dedupe.AutoClasses, c) {
			return ErrClasses
		}
	}
	return nil
}

// Start queues a run of the classes over the open pairs the scope sees, a
// dry run that records what it would do and changes nothing, or the real
// one.
func (s *Service) Start(ctx context.Context, scope library.Scope, classes []string, dryRun bool) (Started, error) {
	if err := checkClasses(classes); err != nil {
		return Started{}, err
	}
	counts, err := s.Counts(ctx, scope)
	if err != nil {
		return Started{}, err
	}
	out := Started{}
	for _, c := range classes {
		out.Pairs += counts[c]
	}
	by := scope.Viewer
	out.RunID, err = s.queue(ctx, classes, dryRun, &by, &by, scope.SeesAll)
	return out, err
}

func (s *Service) queue(ctx context.Context, classes []string, dryRun bool, startedBy, viewer *uuid.UUID, seesAll bool) (uuid.UUID, error) {
	var id uuid.UUID
	rule := Rule
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		// One run at a time: two would merge the same books.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, runLock); err != nil {
			return err
		}
		busy, err := q.HasActiveDuplicatesRun(ctx)
		if err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		id, err = q.CreateDuplicatesRun(ctx, sqlc.CreateDuplicatesRunParams{
			Rule: &rule, StartedBy: startedBy, DryRun: dryRun, Classes: classes, ScopeViewer: viewer, ScopeSeesAll: seesAll,
		})
		if err != nil {
			return err
		}
		_, err = s.Queue.InsertTx(ctx, tx, RunArgs{RunID: id}, jobs.InsertOpts{
			Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts,
		})
		return err
	})
	return id, err
}

// Daily queues a run of the classes the settings name over every library,
// as no person's: the pairs found since the last one are handled with the
// rest. It does nothing while no class is set or a run is going.
func (s *Service) Daily(ctx context.Context) error {
	classes, err := s.Classes(ctx)
	if err != nil || len(classes) == 0 {
		return err
	}
	admin, err := sqlc.New(s.pool).FirstActiveAdmin(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.queue(ctx, classes, false, nil, &admin, true); err != nil && !errors.Is(err, ErrBusy) {
		return err
	}
	return nil
}

// Classes are the classes the settings have the rules handle by themselves.
func (s *Service) Classes(ctx context.Context) ([]string, error) {
	v, err := s.Settings.Text(ctx, settings.DuplicateClasses)
	if err != nil {
		return nil, err
	}
	var out []string
	for c := range strings.SplitSeq(v, ",") {
		if c = strings.TrimSpace(c); c != "" && slices.Contains(dedupe.AutoClasses, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// MergeAgreeing is the setting that merges what agrees of books that differ.
// runLock is the advisory lock that makes queueing a run one at a time.
const runLock = 466_001

const MergeAgreeing = "mergeAgreeing"

// Work goes through the open pairs of the run's classes, for a while, and
// says whether it got to the end. What it has done is in the run's actions
// and its place in the run, so a pass cut off goes on where it was.
func (s *Service) Work(ctx context.Context, runID uuid.UUID) (bool, error) {
	q := sqlc.New(s.pool)
	run, err := q.GetDuplicatesRunToWork(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && run.State != curation.StatePlanned && run.State != curation.StateRunning {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := q.StartDuplicatesRun(ctx, runID); err != nil {
		return false, err
	}
	scope := library.Scope{SeesAll: run.ScopeSeesAll}
	if run.ScopeViewer != nil {
		scope.Viewer = *run.ScopeViewer
	} else if !scope.SeesAll {
		return true, s.finish(ctx, runID)
	}
	onConflict, err := s.Settings.Text(ctx, settings.DuplicateConflict)
	if err != nil {
		return false, err
	}
	ctx = curation.WithRun(ctx, runID, true)
	started := time.Now()
	after := run.ResumeAfter
	for time.Since(started) < passBudget {
		var batch []Pair
		err := s.Pairs(ctx, scope, nil, after, func(p Pair) (bool, error) {
			batch = append(batch, p)
			return len(batch) < batchSize, nil
		})
		if err != nil {
			return false, err
		}
		if len(batch) == 0 {
			return true, s.finish(ctx, runID)
		}
		skipped := 0
		for _, p := range batch {
			if !slices.Contains(run.Classes, p.Class.Class) {
				continue
			}
			left, err := s.handle(ctx, scope, runID, run.DryRun, onConflict == MergeAgreeing, p)
			if err != nil {
				if ctx.Err() != nil {
					return false, err
				}
				s.log.Warn("duplicates: a pair could not be resolved", "pair", p.ID, "class", p.Class.Class, "error", err)
				left = true
			}
			if left {
				skipped++
			}
		}
		last := batch[len(batch)-1].ID
		after = &last
		if err := q.AdvanceDuplicatesRun(ctx, sqlc.AdvanceDuplicatesRunParams{ID: runID, AfterID: after, Skipped: int32(skipped)}); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (s *Service) finish(ctx context.Context, runID uuid.UUID) error {
	q := sqlc.New(s.pool)
	if err := q.FinishDuplicatesRun(ctx, runID); err != nil {
		return err
	}
	return q.DropEmptySystemRun(ctx, runID)
}

// Action is what the rules do with a pair.
type Action string

// The actions of a plan.
const (
	// Merge makes the pair one book.
	Merge Action = "merge"
	// Link keeps both books and relates them.
	Link Action = "link"
	// Leave keeps the pair open for a person.
	Leave Action = "leave"
)

// Plan is what the rules would do with a pair, and why.
type Plan struct {
	Pair   Pair
	Action Action
	// Keep and Gone are the survivor and the book merged into it; for a link,
	// the book that holds the other and the one held.
	Keep, Gone catalog.Book
	// Why is why Keep survives.
	Why    string
	Fields Fields
	// Copies are the files to trash.
	Copies []uuid.UUID
	// Kind is the relation a link records.
	Kind string
	// Left says, for Leave, why in a word for a count; Detail with the
	// fields, for a person.
	Left, Detail string
	Reason       string
}

// Reasons a pair is left for a person.
const (
	LeftRelated    = "already related"
	LeftAudio      = "an audiobook"
	LeftConflict   = "fields differ"
	LeftNotInReach = "not in reach"
)

func (s *Service) leaving(p Pair, left, detail string) Plan {
	return Plan{Pair: p, Action: Leave, Left: left, Detail: detail, Reason: "left for a person: " + detail}
}

// PlanFor works out what the rules do with an open pair, changing nothing.
// A pair whose books are gone, or that the scope no longer sees, has no plan
// (ok is false).
func (s *Service) PlanFor(ctx context.Context, scope library.Scope, mergeAgreeing bool, p Pair) (plan Plan, ok bool, err error) {
	if _, err := sqlc.New(s.pool).GetPairForRun(ctx, p.ID); errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, false, nil
	} else if err != nil {
		return Plan{}, false, err
	}
	a, err := s.books.Get(ctx, scope, p.A)
	if errors.Is(err, catalog.ErrNotFound) {
		return s.leaving(p, LeftNotInReach, "a book of the pair is not in reach"), true, nil
	} else if err != nil {
		return Plan{}, false, err
	}
	b, err := s.books.Get(ctx, scope, p.B)
	if errors.Is(err, catalog.ErrNotFound) {
		return s.leaving(p, LeftNotInReach, "a book of the pair is not in reach"), true, nil
	} else if err != nil {
		return Plan{}, false, err
	}
	if a.ID != p.A || b.ID != p.B {
		return Plan{}, false, nil
	}
	if dedupe.Links(p.Class.Class) {
		return s.planLink(ctx, p, a, b)
	}
	return s.planMerge(ctx, mergeAgreeing, p, a, b)
}

func (s *Service) planLink(ctx context.Context, p Pair, a, b catalog.Book) (Plan, bool, error) {
	related, err := sqlc.New(s.pool).HasBookRelation(ctx, sqlc.HasBookRelationParams{One: p.A, Other: p.B})
	if err != nil {
		return Plan{}, false, err
	}
	if related {
		return s.leaving(p, LeftRelated, "the two are related already"), true, nil
	}
	plan := Plan{Pair: p, Action: Link, Keep: a, Gone: b, Kind: dedupe.RelationContains}
	if p.Class.Holder == "b" {
		plan.Keep, plan.Gone, plan.Kind = b, a, dedupe.RelationContainedIn
	}
	plan.Reason = Evidence(p.Class.Class, p.Class.Overlap) + "; kept both and linked, one holds the other's text"
	return plan, true, nil
}

func (s *Service) factsOf(ctx context.Context, books ...catalog.Book) (map[uuid.UUID]Facts, bool, error) {
	ids := make([]uuid.UUID, len(books))
	for i, b := range books {
		ids[i] = b.ID
	}
	rows, err := sqlc.New(s.pool).ListPairFacts(ctx, ids)
	if err != nil {
		return nil, false, err
	}
	out := map[uuid.UUID]Facts{}
	audio := false
	for _, r := range rows {
		out[r.ID] = Facts{ID: r.ID, HasState: r.HasState != nil && *r.HasState, HasManual: r.HasManual != nil && *r.HasManual, Formats: r.Formats}
		audio = audio || r.HasAudio
	}
	for _, b := range books {
		f := out[b.ID]
		f.Filled = Filled(b)
		out[b.ID] = f
	}
	return out, audio, nil
}

func (s *Service) planMerge(ctx context.Context, mergeAgreeing bool, p Pair, a, b catalog.Book) (Plan, bool, error) {
	facts, audio, err := s.factsOf(ctx, a, b)
	if err != nil {
		return Plan{}, false, err
	}
	if audio {
		return s.leaving(p, LeftAudio, "an audiobook may be another recording"), true, nil
	}
	plan := Plan{Pair: p, Action: Merge, Keep: a, Gone: b}
	firstWins, why := Survivor(facts[a.ID], facts[b.ID])
	if !firstWins {
		plan.Keep, plan.Gone = b, a
	}
	plan.Why = why
	usage, err := s.usage(ctx, p.LibraryID, plan.Keep, plan.Gone)
	if err != nil {
		return Plan{}, false, err
	}
	res := reconcile.Reconcile(DescriptionOf(plan.Keep), DescriptionOf(plan.Gone), reconcile.Options{Usage: usage})
	plan.Fields = Merging(res)
	if len(plan.Fields.Conflicts) > 0 && !mergeAgreeing {
		left := s.leaving(p, LeftConflict, "the books differ in "+fieldNames(plan.Fields.Conflicts))
		left.Fields = plan.Fields
		return left, true, nil
	}
	if plan.Copies, err = s.copies(ctx, plan.Keep, plan.Gone); err != nil {
		return Plan{}, false, err
	}
	plan.Reason = Reason(Evidence(p.Class.Class, p.Class.Overlap), plan.Why, plan.Fields, len(plan.Copies))
	return plan, true, nil
}

// handle resolves one pair, or records it as left for a person; it says
// whether the pair stays open.
func (s *Service) handle(ctx context.Context, scope library.Scope, runID uuid.UUID, dry, mergeAgreeing bool, p Pair) (left bool, err error) {
	plan, ok, err := s.PlanFor(ctx, scope, mergeAgreeing, p)
	if err != nil || !ok {
		return false, err
	}
	rule := p.Class.Class
	if dry {
		kind, books := curation.ActionSkip, []uuid.UUID{p.A, p.B}
		switch plan.Action {
		case Merge:
			kind, books = curation.ActionMerge, []uuid.UUID{plan.Keep.ID, plan.Gone.ID}
			if len(plan.Copies) > 0 {
				kind = curation.ActionReplace
			}
		case Link:
			kind, books = curation.ActionLink, []uuid.UUID{plan.Keep.ID, plan.Gone.ID}
		}
		if err := s.Curation.RecordPlan(ctx, runID, p.LibraryID, kind, rule, plan.Reason, books...); err != nil {
			return false, err
		}
		return plan.Action == Leave, nil
	}
	switch plan.Action {
	case Merge:
		return false, s.doMerge(ctx, scope, plan)
	case Link:
		return false, s.doLink(ctx, scope, plan)
	}
	return true, nil
}

func (s *Service) doMerge(ctx context.Context, scope library.Scope, plan Plan) error {
	m := ingest.AutoMerge{
		Into: plan.Keep.ID, From: plan.Gone.ID, Take: plan.Fields.Take, Union: plan.Fields.Union,
		Trash: plan.Copies, Rule: plan.Pair.Class.Class, Reason: plan.Reason,
	}
	err := s.Ingest.MergeAutomatically(ctx, scope, m)
	if len(plan.Copies) > 0 && (errors.Is(err, ingest.ErrReadOnly) || errors.Is(err, ingest.ErrFolderReadOnly)) {
		m.Trash = nil
		m.Reason = Reason(Evidence(plan.Pair.Class.Class, plan.Pair.Class.Overlap), plan.Why, plan.Fields, 0) +
			"; the folder cannot be written to, so both files stay"
		err = s.Ingest.MergeAutomatically(ctx, scope, m)
	}
	if errors.Is(err, catalog.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) doLink(ctx context.Context, scope library.Scope, plan Plan) error {
	p := plan.Pair
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{p.A, p.B}); err != nil {
			return err
		}
		pending, err := s.Curation.Begin(ctx, tx, curation.SetAll, plan.Keep.ID, plan.Gone.ID)
		if err != nil {
			return err
		}
		if err := q.RelateBooks(ctx, sqlc.RelateBooksParams{One: p.A, Other: p.B, Kind: plan.Kind}); err != nil {
			return err
		}
		if err := q.SetPairState(ctx, sqlc.SetPairStateParams{ID: p.ID, State: dedupe.StateKeptBoth}); err != nil {
			return err
		}
		return pending.Done(ctx, tx, curation.Record{
			RunKind: curation.KindDuplicates, By: scope.Viewer, Kind: curation.ActionLink, Rule: p.Class.Class, Reason: plan.Reason,
		})
	})
}

// copies are the files of the two books to trash, none where the library
// cannot be written to.
func (s *Service) copies(ctx context.Context, keep, gone catalog.Book) ([]uuid.UUID, error) {
	q := sqlc.New(s.pool)
	writable, err := q.IsLibraryWritable(ctx, keep.LibraryID)
	if err != nil || !writable {
		return nil, err
	}
	rows, err := q.ListLivePairFiles(ctx, []uuid.UUID{keep.ID, gone.ID})
	if err != nil {
		return nil, err
	}
	files := make([]File, len(rows))
	for i, r := range rows {
		files[i] = File{ID: r.ID, Book: r.BookID, Kind: r.Kind, Format: r.Format, Modified: r.ModifiedAt, Findings: len(r.EreaderFindings)}
	}
	return Copies(files), nil
}

// usage counts the books of the library that spell the authors and series
// of the two as they do.
func (s *Service) usage(ctx context.Context, libraryID uuid.UUID, books ...catalog.Book) (map[string]int, error) {
	var names []string
	for _, b := range books {
		for _, c := range b.Contributors {
			names = append(names, c.Name)
		}
		if b.Series != "" {
			names = append(names, b.Series)
		}
	}
	rows, err := sqlc.New(s.pool).CountNameUsage(ctx, sqlc.CountNameUsageParams{LibraryID: libraryID, Names: names})
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.Name] += int(r.Books)
	}
	return out, nil
}

const (
	runKind   = "duplicates.run"
	dailyKind = "duplicates.daily"
	// DailyInterval is how often new pairs are handled when classes are set.
	DailyInterval = 24 * time.Hour
)

// RunArgs is the job that works through a run.
type RunArgs struct {
	RunID uuid.UUID `json:"runId"`
}

func (RunArgs) Kind() string { return runKind }

// RunWorker runs RunArgs.
type RunWorker struct {
	river.WorkerDefaults[RunArgs]
	Service *Service
}

func (w *RunWorker) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	done, err := w.Service.Work(ctx, job.Args.RunID)
	if err != nil {
		return err
	}
	if !done {
		return river.JobSnooze(time.Second)
	}
	return nil
}

// DailyArgs is the job that queues the day's run.
type DailyArgs struct{}

func (DailyArgs) Kind() string { return dailyKind }

// DailyWorker runs DailyArgs.
type DailyWorker struct {
	river.WorkerDefaults[DailyArgs]
	Service *Service
}

func (w *DailyWorker) Work(ctx context.Context, _ *river.Job[DailyArgs]) error {
	return w.Service.Daily(ctx)
}
