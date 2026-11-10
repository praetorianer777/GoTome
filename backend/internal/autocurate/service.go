package autocurate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reconcile"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

const (
	// budget is how long one run of a job works before it hands on.
	budget = 30 * time.Second
	// batch is how many books with matches a pass reads at once.
	batch = 50
	// PlanBooks bounds how many books a count of what a rule would take
	// looks at, so that a page stays quick; a dry run sees them all.
	PlanBooks = 500
	// RuleAuto is the rule of the runs this package makes.
	RuleAuto = "auto"
	// SampleSize is how many matches of a group are shown before it is
	// accepted.
	SampleSize  = 3
	jobAttempts = 5
)

// Editor changes books as a person's edit does: ingest.Service.
type Editor interface {
	ChangeTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, c catalog.Change) (catalog.Changed, error)
	EditTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, e catalog.Edit) error
}

// Queue enqueues jobs: jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Settings is where the switches are: settings.Store.
type Settings interface {
	Text(ctx context.Context, key string) (string, error)
}

// Service makes the automatic runs.
type Service struct {
	pool     *pgxpool.Pool
	curation *curation.Service
	cleanup  *cleanup.Service
	editor   Editor
	settings Settings
	log      *slog.Logger
	// Queue is set once the job runner exists.
	Queue Queue
}

// NewService returns a Service. Its Queue must be set before a run is
// started.
func NewService(pool *pgxpool.Pool, cur *curation.Service, clean *cleanup.Service, editor Editor, s Settings, log *slog.Logger) *Service {
	return &Service{pool: pool, curation: cur, cleanup: clean, editor: editor, settings: s, log: log}
}

// ErrKinds is a request for a kind that is not applied by itself.
var ErrKinds = errors.New("a kind of clean-up that is not applied automatically")

// KindCount is how many suggestions of a kind a run would apply.
type KindCount struct {
	Kind        string
	Suggestions int
	Books       int
}

// CleanupCounts says, per kind, what an automatic run would apply for the
// scope's books.
func (s *Service) CleanupCounts(ctx context.Context, scope library.Scope) ([]KindCount, error) {
	var out []KindCount
	for _, kind := range cleanup.AutoKinds {
		changes, autos, err := s.cleanupChanges(ctx, scope, kind)
		if err != nil {
			return nil, err
		}
		out = append(out, KindCount{Kind: kind, Suggestions: len(autos), Books: len(changes)})
	}
	return out, nil
}

func (s *Service) cleanupChanges(ctx context.Context, scope library.Scope, kind string) ([]cleanup.AutoChange, []cleanup.Auto, error) {
	autos, err := s.cleanup.Automatic(ctx, scope, kind)
	if err != nil {
		return nil, nil, err
	}
	if kind == cleanup.KindAuthors {
		for i := range autos {
			autos[i].Suggested = PreferredSpelling(autos[i].Found)
		}
	}
	changes, err := s.cleanup.AutoChanges(ctx, scope, kind, autos)
	return changes, autos, err
}

// PreferredSpelling is the spelling of a person to keep among spellings
// that differ only in how they are written: reconcile's choice, which goes
// by how many books of the library write it so.
func PreferredSpelling(found []cleanup.Name) string {
	if len(found) == 0 {
		return ""
	}
	usage := map[string]int{}
	for _, n := range found {
		usage[reconcileForm(n.Name)] += n.Books
	}
	best := found[0].Name
	for _, n := range found[1:] {
		res := reconcile.Reconcile(
			reconcile.Description{Authors: []string{best}},
			reconcile.Description{Authors: []string{n.Name}},
			reconcile.Options{Usage: usage})
		if d, ok := res.Of(catalog.FieldContributors); ok && !d.Conflict && len(d.Value) == 1 {
			best = d.Value[0]
		}
	}
	return best
}

// reconcileForm is a name as reconcile normalises it before it counts it.
func reconcileForm(name string) string {
	d := reconcile.Reconcile(reconcile.Description{Authors: []string{name}}, reconcile.Description{}, reconcile.Options{})
	if a, ok := d.Of(catalog.FieldContributors); ok && len(a.Value) == 1 {
		return a.Value[0]
	}
	return name
}

// Started is a run that was queued.
type Started struct {
	RunID  uuid.UUID
	DryRun bool
}

// CleanupArgs is the job that works through an automatic clean-up run.
type CleanupArgs struct {
	RunID   uuid.UUID `json:"runId"`
	Kinds   []string  `json:"kinds"`
	Viewer  uuid.UUID `json:"viewer"`
	SeesAll bool      `json:"seesAll"`
	DryRun  bool      `json:"dryRun"`
}

const cleanupKind = "curation.auto_cleanup"

// Kind names the job in the queue. It is stored with every job, so it stays.
func (CleanupArgs) Kind() string { return cleanupKind }

// MatchesArgs is the job that works through an automatic run over the
// matches that wait. After is the last book of the pass before.
type MatchesArgs struct {
	RunID   uuid.UUID   `json:"runId"`
	Viewer  uuid.UUID   `json:"viewer"`
	SeesAll bool        `json:"seesAll"`
	DryRun  bool        `json:"dryRun"`
	Group   *MatchGroup `json:"group,omitempty"`
	After   uuid.UUID   `json:"after"`
}

const matchesKind = "curation.auto_matches"

// Kind names the job in the queue. It is stored with every job, so it stays.
func (MatchesArgs) Kind() string { return matchesKind }

// MatchGroup is the matches of one provider within a tenth of the score:
// Band 9 is 0.9 and over. A person accepts one as a whole.
type MatchGroup struct {
	Provider string `json:"provider"`
	Band     int    `json:"band"`
}

func band(score float64) int { return min(int(score*10), 9) }

// StartCleanup queues an automatic run over the given kinds for the books
// the scope sees. by is who asked, nil for a run on a schedule.
func (s *Service) StartCleanup(ctx context.Context, scope library.Scope, by *uuid.UUID, kinds []string, dryRun bool) (Started, error) {
	if len(kinds) == 0 {
		return Started{}, ErrKinds
	}
	for _, k := range kinds {
		if !slices.Contains(cleanup.AutoKinds, k) {
			return Started{}, ErrKinds
		}
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = s.curation.CreateRunTx(ctx, tx, curation.RunSpec{
			Kind: curation.KindCleanup, Rule: RuleAuto, By: by, Automatic: true, DryRun: dryRun,
		})
		if err != nil {
			return err
		}
		_, err = s.Queue.InsertTx(ctx, tx, CleanupArgs{
			RunID: id, Kinds: kinds, Viewer: scope.Viewer, SeesAll: scope.SeesAll, DryRun: dryRun,
		}, jobs.InsertOpts{Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts})
		return err
	})
	return Started{RunID: id, DryRun: dryRun}, err
}

// StartMatches queues an automatic run over the matches that wait, or over
// one group of them that a person accepted as a whole.
func (s *Service) StartMatches(ctx context.Context, scope library.Scope, by *uuid.UUID, group *MatchGroup, dryRun bool) (Started, error) {
	rule := RuleAuto
	if group != nil {
		rule = "matches.group"
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = s.curation.CreateRunTx(ctx, tx, curation.RunSpec{
			Kind: curation.KindMatches, Rule: rule, By: by, Automatic: true, DryRun: dryRun,
		})
		if err != nil {
			return err
		}
		_, err = s.Queue.InsertTx(ctx, tx, MatchesArgs{
			RunID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll, DryRun: dryRun, Group: group,
		}, jobs.InsertOpts{Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts})
		return err
	})
	return Started{RunID: id, DryRun: dryRun}, err
}

// RunCleanup works through a clean-up run for a while and reports whether
// it got to the end. What it did is no longer suggested, and a book with an
// action of the run is passed by, so a run cut off and run again does each
// book once.
func (s *Service) RunCleanup(ctx context.Context, a CleanupArgs) (bool, error) {
	scope := library.Scope{Viewer: a.Viewer, SeesAll: a.SeesAll}
	ctx = curation.WithRun(ctx, a.RunID, true)
	started := time.Now()
	for _, kind := range cleanup.AutoKinds {
		if !slices.Contains(a.Kinds, kind) {
			continue
		}
		changes, _, err := s.cleanupChanges(ctx, scope, kind)
		if err != nil {
			return false, err
		}
		for _, c := range changes {
			if time.Since(started) > budget {
				return false, nil
			}
			if err := s.cleanupBook(ctx, a, scope, c); err != nil {
				return false, err
			}
		}
	}
	return true, s.finish(ctx, a.RunID)
}

func (s *Service) finish(ctx context.Context, run uuid.UUID) error {
	if err := s.curation.FinishRun(ctx, run); err != nil {
		return err
	}
	return sqlc.New(s.pool).DeleteEmptyCurationRun(ctx, run)
}

// skippable is an error that is about one book and no reason to stop.
func skippable(err error) bool {
	var invalid catalog.EditError
	return errors.Is(err, catalog.ErrNotFound) || errors.As(err, &invalid)
}

func (s *Service) cleanupBook(ctx context.Context, a CleanupArgs, scope library.Scope, c cleanup.AutoChange) error {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := sqlc.New(tx).HoldLibrariesOfBooks(ctx, []uuid.UUID{c.BookID}); err != nil {
			return err
		}
		if seen, err := s.curation.HasBook(ctx, tx, a.RunID, c.BookID); err != nil || seen {
			return err
		}
		pending, err := s.curation.Begin(ctx, tx, curation.SetEdit, c.BookID)
		if err != nil {
			return err
		}
		reason := c.Reason
		if a.DryRun {
			book, err := catalog.GetTx(ctx, tx, scope, c.BookID)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(catalog.PreviewBook(book, c.Change), func(f catalog.FieldPreview) bool { return !f.Locked }) {
				return nil
			}
			reason = "Would change: " + reason
		} else {
			changed, err := s.editor.ChangeTx(ctx, tx, scope, c.BookID, c.Change)
			if err != nil {
				return err
			}
			if !changed.Values && !changed.Locks {
				return nil
			}
		}
		return pending.Done(ctx, tx, curation.Record{Kind: curation.ActionEdit, Rule: c.Rule, Reason: reason})
	})
	if err != nil && skippable(err) {
		s.log.Debug("automatic clean-up skipped a book", "book", c.BookID, "error", err)
		return nil
	}
	return err
}

// textLanguage is the language the book's text was detected in: the one most
// chunks agree on, when enough chunks say.
func textLanguage(ctx context.Context, q *sqlc.Queries, book uuid.UUID) (string, error) {
	rows, err := q.ListTextLanguages(ctx, book)
	if err != nil {
		return "", err
	}
	total := 0
	for _, r := range rows {
		total += int(r.Chunks)
	}
	if total < MinTextChunks || int(rows[0].Chunks)*5 < total*4 {
		return "", nil
	}
	return rows[0].Lang, nil
}

// MatchCounts is what a pass over the matches would do.
type MatchCounts struct {
	// Books is how many books were looked at, Capped that there are more.
	Books  int
	Capped bool
	// Accept is the matches whose fields would be taken, Settled those of
	// them taken whole, Dismiss the matches that would be dismissed.
	Accept  int
	Settled int
	Dismiss int
}

// MatchGroupInfo is a group of matches that waits for a person.
type MatchGroupInfo struct {
	MatchGroup
	Matches int
	Sample  []uuid.UUID
}

// Groups lists the pending matches of the scope's books by provider and
// band, each with a sample of the best fitting ones.
func (s *Service) Groups(ctx context.Context, scope library.Scope) ([]MatchGroupInfo, error) {
	q := sqlc.New(s.pool)
	rows, err := q.ListMatchGroups(ctx, sqlc.ListMatchGroupsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return nil, err
	}
	out := make([]MatchGroupInfo, 0, len(rows))
	for _, r := range rows {
		sample, err := q.ListMatchGroupSample(ctx, sqlc.ListMatchGroupSampleParams{
			Provider: r.Provider, Band: r.Band, Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxRows: SampleSize,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, MatchGroupInfo{MatchGroup: MatchGroup{Provider: r.Provider, Band: int(r.Band)}, Matches: int(r.Matches), Sample: sample})
	}
	return out, nil
}

// MatchPlan counts what an automatic pass would do over the first books
// with matches, without changing anything.
func (s *Service) MatchPlan(ctx context.Context, scope library.Scope) (MatchCounts, error) {
	var out MatchCounts
	q := sqlc.New(s.pool)
	after := uuid.Nil
	for out.Books < PlanBooks {
		ids, err := q.ListReviewBooksAfter(ctx, sqlc.ListReviewBooksAfterParams{
			AfterID: after, Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxBooks: int32(min(batch, PlanBooks-out.Books)),
		})
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			after = id
			out.Books++
			r, err := s.matchesOfBook(ctx, s.pool, nil, scope, id, nil)
			if err != nil {
				if skippable(err) {
					continue
				}
				return out, err
			}
			out.Accept += r.accepted
			out.Settled += r.settled
			out.Dismiss += r.dismissed
		}
		if len(ids) < batch {
			return out, nil
		}
	}
	out.Capped = true
	return out, nil
}

type bookResult struct {
	accepted, settled, dismissed int
	reasons                      []string
	rule                         string
}

func (r bookResult) did() bool { return r.accepted+r.dismissed+r.settled > 0 }

// matchesOfBook decides the pending matches of one book, best fitting
// first, and when tx is not nil makes the decisions: dismissals, the fields
// the rule takes, and the settling of a match taken whole. It counts them
// either way. The caller holds the book's library.
func (s *Service) matchesOfBook(ctx context.Context, conn sqlc.DBTX, tx pgx.Tx, scope library.Scope, id uuid.UUID, group *MatchGroup) (bookResult, error) {
	var res bookResult
	q := sqlc.New(conn)
	rows, err := q.ListPendingMatches(ctx, []uuid.UUID{id})
	if err != nil {
		return res, err
	}
	var all []enrich.Match
	for _, row := range rows {
		m, err := enrich.MatchOf(row)
		if err != nil {
			return res, err
		}
		all = append(all, m)
	}
	if len(all) == 0 {
		return res, nil
	}
	book, err := catalog.GetTx(ctx, conn, scope, id)
	if err != nil {
		return res, err
	}
	lang, err := textLanguage(ctx, q, id)
	if err != nil {
		return res, err
	}
	settledAlready := map[uuid.UUID]bool{}
	for _, m := range all {
		if settledAlready[m.ID] {
			continue
		}
		if group != nil && (m.Provider != group.Provider || band(m.Score) != group.Band) {
			continue
		}
		var others []enrich.Match
		for _, o := range all {
			if o.ID != m.ID && !settledAlready[o.ID] {
				others = append(others, o)
			}
		}
		out := Decide(Input{Book: book, TextLanguage: lang, Match: m, Others: others, Unagreed: group != nil})
		note := func(rule, reason string) {
			if res.rule == "" {
				res.rule = rule
			}
			res.reasons = append(res.reasons, m.Provider+" "+m.Record.ID+": "+reason)
		}
		switch {
		case out.Dismiss:
			res.dismissed++
			note(out.Rule, out.Reason)
			settledAlready[m.ID] = true
			if tx != nil {
				if err := q.SetMatchState(ctx, sqlc.SetMatchStateParams{ID: m.ID, State: enrich.StateDismissed}); err != nil {
					return res, err
				}
			}
		case out.Agreed != "" && (out.Edit.ChangesValues() || out.Settles()):
			if out.Edit.ChangesValues() {
				res.accepted++
				if tx != nil {
					if err := s.editor.EditTx(ctx, tx, scope, id, out.Edit); err != nil {
						return res, err
					}
					if book, err = catalog.GetTx(ctx, conn, scope, id); err != nil {
						return res, err
					}
				}
			}
			note(out.Rule, out.Reason)
			if out.Settles() {
				res.settled++
				settledAlready[m.ID] = true
				for _, o := range others {
					settledAlready[o.ID] = true
				}
				if tx != nil {
					if err := q.SetMatchState(ctx, sqlc.SetMatchStateParams{ID: m.ID, State: enrich.StateApplied}); err != nil {
						return res, err
					}
					if err := q.DismissOtherMatches(ctx, sqlc.DismissOtherMatchesParams{BookID: id, ID: m.ID}); err != nil {
						return res, err
					}
				}
			}
		}
	}
	return res, nil
}

// RunMatches works through the books with matches that wait, from the one
// after args.After, for a while. At the end of its time it queues the rest
// as a job of its own, so each pass starts where the last stopped.
func (s *Service) RunMatches(ctx context.Context, a MatchesArgs) error {
	scope := library.Scope{Viewer: a.Viewer, SeesAll: a.SeesAll}
	ctx = curation.WithRun(ctx, a.RunID, true)
	q := sqlc.New(s.pool)
	started := time.Now()
	after := a.After
	for {
		ids, err := q.ListReviewBooksAfter(ctx, sqlc.ListReviewBooksAfterParams{
			AfterID: after, Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxBooks: batch,
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return s.finish(ctx, a.RunID)
		}
		for _, id := range ids {
			if time.Since(started) > budget {
				next := a
				next.After = after
				return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
					_, err := s.Queue.InsertTx(ctx, tx, next, jobs.InsertOpts{Queue: jobs.QueueMetadata, MaxAttempts: jobAttempts})
					return err
				})
			}
			if err := s.matchBook(ctx, a, scope, id); err != nil {
				return err
			}
			after = id
		}
	}
}

func (s *Service) matchBook(ctx context.Context, a MatchesArgs, scope library.Scope, id uuid.UUID) error {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := sqlc.New(tx).HoldLibrariesOfBooks(ctx, []uuid.UUID{id}); err != nil {
			return err
		}
		if seen, err := s.curation.HasBook(ctx, tx, a.RunID, id); err != nil || seen {
			return err
		}
		pending, err := s.curation.Begin(ctx, tx, curation.SetMatch, id)
		if err != nil {
			return err
		}
		var write pgx.Tx
		if !a.DryRun {
			write = tx
		}
		res, err := s.matchesOfBook(ctx, tx, write, scope, id, a.Group)
		if err != nil || !res.did() {
			return err
		}
		reason := strings.Join(res.reasons, " ")
		if a.DryRun {
			reason = "Would: " + reason
		}
		return pending.Done(ctx, tx, curation.Record{Kind: curation.ActionEdit, Rule: res.rule, Reason: reason})
	})
	if err != nil && skippable(err) {
		s.log.Debug("automatic matches skipped a book", "book", id, "error", err)
		return nil
	}
	return err
}

// Worker runs CleanupArgs.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	Service *Service
}

func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[CleanupArgs]) error {
	done, err := w.Service.RunCleanup(ctx, job.Args)
	if err != nil {
		return err
	}
	if !done {
		return river.JobSnooze(time.Second)
	}
	return nil
}

// MatchesWorker runs MatchesArgs.
type MatchesWorker struct {
	river.WorkerDefaults[MatchesArgs]
	Service *Service
}

func (w *MatchesWorker) Work(ctx context.Context, job *river.Job[MatchesArgs]) error {
	return w.Service.RunMatches(ctx, job.Args)
}

// DailyArgs is the scheduled job that starts the runs the settings ask for.
type DailyArgs struct{}

const dailyKind = "curation.daily"

// Kind names the job in the queue. It is stored with every job, so it stays.
func (DailyArgs) Kind() string { return dailyKind }

// Periodic is the daily pass, also at start.
func Periodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, DailyArgs{}, jobs.QueueMetadata)
}

// DailyWorker runs DailyArgs.
type DailyWorker struct {
	river.WorkerDefaults[DailyArgs]
	Service *Service
}

func (w *DailyWorker) Work(ctx context.Context, _ *river.Job[DailyArgs]) error {
	return w.Service.Daily(ctx)
}

// Daily starts a run over every book for each automatic rule the settings
// have on: the kinds of clean-up named, and the matches.
func (s *Service) Daily(ctx context.Context) error {
	all := library.Scope{SeesAll: true}
	kinds, err := s.settings.Text(ctx, settings.CurationCleanupKinds)
	if err != nil {
		return err
	}
	var chosen []string
	for k := range strings.SplitSeq(kinds, ",") {
		if k = strings.TrimSpace(k); slices.Contains(cleanup.AutoKinds, k) {
			chosen = append(chosen, k)
		}
	}
	if len(chosen) > 0 {
		if _, err := s.StartCleanup(ctx, all, nil, chosen, false); err != nil {
			return fmt.Errorf("start automatic clean-up: %w", err)
		}
	}
	on, err := s.settings.Text(ctx, settings.CurationMatchesAuto)
	if err != nil {
		return err
	}
	if on == "on" {
		if _, err := s.StartMatches(ctx, all, nil, nil, false); err != nil {
			return fmt.Errorf("start automatic matches: %w", err)
		}
	}
	return nil
}
