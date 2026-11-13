// Package discover finds books beyond the libraries from a person's taste
// (#393): other works of the authors they rated highest, the next volumes
// and the gaps of the series they read, and books of the genres they read
// most. Providers are asked only through metadata.Service, and only about
// authors, series and subjects, never about the person or their library;
// what comes back is left out where a library the person sees holds it,
// where it is on their wishlist or in their history, or where they said
// they are not interested, and ranked by how near their taste it is.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// The reasons a book is offered, as discover_candidates.reason_kind.
const (
	ReasonAuthor     = "author"
	ReasonSeriesNext = "series_next"
	ReasonSeriesGap  = "series_gap"
	ReasonSeriesMore = "series_more"
	ReasonGenre      = "genre"
)

// What the page says Discover is doing.
const (
	StateOff     = "off"
	StateOffline = "offline"
	StateNever   = "never"
	StateRunning = "running"
	StateReady   = "ready"
)

const (
	// runEvery is how old a person's run is before the next one starts.
	runEvery = 24 * time.Hour
	// knownFor is how recent what following knows of a subject must be for
	// Discover not to ask the providers again.
	knownFor = 48 * time.Hour
)

var (
	// ErrOff is Discover switched off by the administrator.
	ErrOff = errors.New("discover is switched off")
	// ErrOffline is a server that asks no provider.
	ErrOffline = errors.New("this server asks no provider")
	// ErrNotFound is a candidate that is not the person's, or none at all.
	ErrNotFound = errors.New("no such candidate")
)

// Source asks the providers; it is metadata.Service.
type Source interface {
	WorksOf(ctx context.Context, kind, name string) ([]metadata.Answer, error)
	SubjectsOf(ctx context.Context, subject string) ([]metadata.Answer, error)
}

// Taster knows what a person likes; it is similar.Service.
type Taster interface {
	Profile(ctx context.Context, scope library.Scope) (similar.Profile, bool, error)
	Resemble(ctx context.Context, p similar.Profile, texts []string) ([]*similar.Resemblance, error)
}

// Queue enqueues the run.
type Queue interface {
	Insert(ctx context.Context, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service finds the books.
type Service struct {
	pool   *pgxpool.Pool
	source Source
	taste  Taster
	log    *slog.Logger
	// Enabled says whether the administrator switched Discover on
	// (recommendations.discover); nil, it is off.
	Enabled func(ctx context.Context) bool
	// Offline asks no provider (GOTOME_OFFLINE).
	Offline bool
	Queue   Queue
	// AsksPerPass bounds how many subjects one pass asks the providers
	// about, over all people; zero is no bound but the deadline.
	AsksPerPass int
	// Now is the time; nil, the clock.
	Now func() time.Time
}

// NewService returns the Service; Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, source Source, taste Taster, log *slog.Logger) *Service {
	return &Service{pool: pool, source: source, taste: taste, log: log}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) enabled(ctx context.Context) bool { return s.Enabled != nil && s.Enabled(ctx) }

// Item is a book offered to a person.
type Item struct {
	ID     uuid.UUID
	Record metadata.Record
	Score  float64
	Reason Reason
}

// Reason says why a book is offered.
type Reason struct {
	Kind string
	// Name is the author, the series or the genre's key.
	Name string
	// Stars is the best rating the person gave the author's books.
	Stars int
	// Because are books of theirs that this one is near.
	Because []string
}

// View is what the page shows.
type View struct {
	State string
	// FinishedAt is when the last run ended.
	FinishedAt *time.Time
	Items      []Item
	// Progress is set while the state is running.
	Progress *Progress
}

// Activity says what a running run is doing.
const (
	// ActivityWorking: a pass is asking the providers now.
	ActivityWorking = "working"
	// ActivityWaiting: the run goes on at the next pass.
	ActivityWaiting = "waiting"
)

// Count is how many subjects of a kind were asked, of how many planned.
type Count struct{ Asked, Planned int }

// Progress is how far the caller's own run is. Planned is false until a
// pass has worked out what to ask.
type Progress struct {
	// StartedAt is nil while the run is asked for and no pass has begun it.
	StartedAt *time.Time
	Planned   bool
	Authors   Count
	Series    Count
	Genres    Count
	Activity  string
	// NextPassAt is about when a waiting run is worked again; nil when it
	// is due at once or not known.
	NextPassAt *time.Time
	BackingOff []metadata.BusyError
}

// backoffs is what a Source may tell besides its answers.
type backoffs interface{ Backoffs() []metadata.BusyError }

// List is what the scope's person is offered, the best first. Books they
// have since wished for, read, got a library to hold or dismissed are
// taken out now, not at the next run.
func (s *Service) List(ctx context.Context, scope library.Scope) (View, error) {
	view := View{Items: []Item{}}
	switch {
	case !s.enabled(ctx):
		view.State = StateOff
		return view, nil
	case s.Offline:
		view.State = StateOffline
		return view, nil
	}
	q := sqlc.New(s.pool)
	run, err := q.GetDiscoverRun(ctx, scope.Viewer)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		view.State = StateNever
		return view, nil
	case err != nil:
		return view, err
	}
	view.State = StateReady
	if run.FinishedAt == nil || run.Requested {
		view.State = StateRunning
	}
	if run.FinishedAt != nil && !run.Requested {
		view.FinishedAt = run.FinishedAt
	}
	if view.State == StateRunning {
		if view.Progress, err = s.progress(ctx, run); err != nil {
			return view, err
		}
	}
	rows, err := q.ListDiscoverCandidates(ctx, scope.Viewer)
	if err != nil || len(rows) == 0 {
		return view, err
	}
	kept, gone, err := s.withoutHeld(ctx, scope, rows)
	if err != nil {
		return view, err
	}
	if len(gone) > 0 {
		if err := q.DeleteDiscoverCandidates(ctx, sqlc.DeleteDiscoverCandidatesParams{UserID: scope.Viewer, Ids: gone}); err != nil {
			return view, err
		}
	}
	for _, r := range kept {
		var rec metadata.Record
		if err := json.Unmarshal(r.Record, &rec); err != nil {
			return view, fmt.Errorf("candidate %s: %w", r.ID, err)
		}
		view.Items = append(view.Items, Item{
			ID: r.ID, Record: rec, Score: r.Score,
			Reason: Reason{Kind: r.ReasonKind, Name: r.ReasonName, Stars: int(r.ReasonStars), Because: r.Because},
		})
	}
	return view, nil
}

// withoutHeld splits the candidates into those the person has a use for
// and the IDs of those they have none for.
func (s *Service) withoutHeld(ctx context.Context, scope library.Scope, rows []sqlc.ListDiscoverCandidatesRow) ([]sqlc.ListDiscoverCandidatesRow, []uuid.UUID, error) {
	keys := make([]keyed, len(rows))
	for i, r := range rows {
		keys[i] = keyed{titles: r.TitleKeys, authors: r.AuthorKeys, isbns: r.Isbns}
	}
	held, err := s.held(ctx, scope, keys)
	if err != nil {
		return nil, nil, err
	}
	var kept []sqlc.ListDiscoverCandidatesRow
	var gone []uuid.UUID
	for i, r := range rows {
		if _, ok := held[i]; ok {
			gone = append(gone, r.ID)
			continue
		}
		kept = append(kept, r)
	}
	return kept, gone, nil
}

// Request asks for a run now. It reports ErrOff and ErrOffline where none
// can be made. A run in progress goes on.
func (s *Service) Request(ctx context.Context, user uuid.UUID) error {
	switch {
	case !s.enabled(ctx):
		return ErrOff
	case s.Offline:
		return ErrOffline
	}
	if err := sqlc.New(s.pool).RequestDiscoverRun(ctx, user); err != nil {
		return err
	}
	if s.Queue == nil {
		return nil
	}
	_, err := s.Queue.Insert(ctx, RunArgs{}, runOpts)
	return err
}

// Dismiss says the person is not interested in a candidate: it goes, and
// every book of the same title and an author in common with it.
func (s *Service) Dismiss(ctx context.Context, user, id uuid.UUID) error {
	q := sqlc.New(s.pool)
	c, err := q.GetDiscoverCandidate(ctx, sqlc.GetDiscoverCandidateParams{ID: id, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := q.PutDiscoverDismissal(ctx, sqlc.PutDiscoverDismissalParams{UserID: user, TitleKeys: c.TitleKeys, AuthorKeys: c.AuthorKeys}); err != nil {
		return err
	}
	return q.DeleteDiscoverCandidates(ctx, sqlc.DeleteDiscoverCandidatesParams{UserID: user, Ids: []uuid.UUID{id}})
}

// keyed is what a candidate is compared by.
type keyed struct {
	titles, authors, isbns []string
}

// held says, by the candidates' places in the list, which the person has no
// use for, and why: ListDiscoverHeld.
func (s *Service) held(ctx context.Context, scope library.Scope, cands []keyed) (map[int]string, error) {
	var titles, authors, isbns []string
	for i, c := range cands {
		for _, k := range c.titles {
			titles = append(titles, fmt.Sprintf("%d|%s", i, k))
		}
		for _, k := range c.authors {
			authors = append(authors, fmt.Sprintf("%d|%s", i, k))
		}
		for _, k := range c.isbns {
			isbns = append(isbns, fmt.Sprintf("%d|%s", i, k))
		}
	}
	out := map[int]string{}
	if len(titles) == 0 && len(isbns) == 0 {
		return out, nil
	}
	rows, err := sqlc.New(s.pool).ListDiscoverHeld(ctx, sqlc.ListDiscoverHeldParams{
		Titles: nonNil(titles), Authors: nonNil(authors), Isbns: nonNil(isbns), Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[int(r.Idx)] = r.Reason
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// keysOf are the keys a record is compared by: its title's key, and that
// of the title before a colon or bracket, as releases keep it; its authors'
// keys in both orders of their names and as a person; its ISBNs.
func keysOf(r metadata.Record) keyed {
	var k keyed
	add := func(list *[]string, v string) {
		if v == "" || strings.Contains(v, "|") {
			return
		}
		for _, have := range *list {
			if have == v {
				return
			}
		}
		*list = append(*list, v)
	}
	add(&k.titles, titleKey(r.Title))
	title := r.Title
	if i := strings.IndexAny(title, ":(["); i > 0 {
		title = title[:i]
	}
	add(&k.titles, titleKey(title))
	for _, c := range r.Contributors {
		if c.Role != "" && c.Role != "author" {
			continue
		}
		for _, key := range metadata.NameKeys(c.Name) {
			add(&k.authors, key)
		}
		add(&k.authors, personKey(c.Name))
	}
	for _, id := range r.Identifiers {
		if id.Type == "isbn" {
			add(&k.isbns, id.Value)
		}
	}
	return k
}

// progress works out how far a run is from what its passes stored, with
// two small queries and no plan; nobody is asked.
func (s *Service) progress(ctx context.Context, run sqlc.DiscoverRun) (*Progress, error) {
	p := &Progress{Activity: ActivityWaiting}
	if run.FinishedAt == nil {
		p.StartedAt = &run.StartedAt
		p.Planned = run.Planned
		asked, err := sqlc.New(s.pool).CountDiscoverAsked(ctx, sqlc.CountDiscoverAskedParams{UserID: run.UserID, AskedAt: run.StartedAt})
		if err != nil {
			return nil, err
		}
		got := map[string]int{}
		for _, a := range asked {
			got[a.Kind] = int(a.Asked)
		}
		p.Authors = Count{min(got[kindAuthor], int(run.PlannedAuthors)), int(run.PlannedAuthors)}
		p.Series = Count{min(got[kindSeries], int(run.PlannedSeries)), int(run.PlannedSeries)}
		p.Genres = Count{min(got[kindGenre], int(run.PlannedGenres)), int(run.PlannedGenres)}
	}
	if run.PassUntil != nil && run.PassUntil.After(s.now()) {
		p.Activity = ActivityWorking
	} else {
		next, err := s.nextPass(ctx)
		if err != nil {
			return nil, err
		}
		p.NextPassAt = next
	}
	if b, ok := s.source.(backoffs); ok {
		p.BackingOff = b.Backoffs()
	}
	return p, nil
}

// nextPass is about when the job that works the runs is next due: now, or
// not known, as nil. A pass working for someone else ends within passBudget.
func (s *Service) nextPass(ctx context.Context) (*time.Time, error) {
	var state string
	var scheduled time.Time
	var attempted *time.Time
	err := s.pool.QueryRow(ctx, `
SELECT state::text, scheduled_at, attempted_at FROM river_job
WHERE kind = $1 AND state IN ('available', 'scheduled', 'retryable', 'running')
ORDER BY CASE state WHEN 'running' THEN 0 ELSE 1 END, scheduled_at LIMIT 1`, runKind).Scan(&state, &scheduled, &attempted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	at := scheduled
	if state == "running" && attempted != nil {
		at = attempted.Add(passBudget)
	}
	if !at.After(s.now()) {
		return nil, nil
	}
	return &at, nil
}
