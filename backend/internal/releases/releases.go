// Package releases follows authors and series for their new books (#69).
// People follow a subject; the providers that list works are asked about
// each subject once a day, and what they list becomes releases, one per
// title however many providers list it. A release found later than a
// provider's first answer is news: its followers are told when it is
// announced and, once its day comes, that it is out, each person once and
// only while no library they see holds the book.
package releases

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

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// What can be followed.
const (
	KindAuthor = metadata.WorksAuthor
	KindSeries = metadata.WorksSeries
)

// Kinds are what can be followed, in the order they are offered.
var Kinds = []string{KindAuthor, KindSeries}

// The stages a release is told at.
const (
	stageAnnounced = "announced"
	stageOut       = "released"
)

const (
	// pollEvery is how often a subject is asked about.
	pollEvery = 24 * time.Hour
	// recentlyOut is how long ago a book may have come out for its first
	// listing still to be news.
	recentlyOut = 90 * 24 * time.Hour
)

var (
	// ErrNotFound is a tracker that is not the caller's, or none at all.
	ErrNotFound = errors.New("no such tracker")
	// ErrBadName is a name that is empty once compared.
	ErrBadName = errors.New("a name to follow is needed")
)

// Lister asks the providers for the books of an author or a series; it is
// metadata.Service.
type Lister interface {
	WorksOf(ctx context.Context, kind, name string) ([]metadata.Answer, error)
}

// Queue enqueues the poll.
type Queue interface {
	Insert(ctx context.Context, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service follows authors and series.
type Service struct {
	pool  *pgxpool.Pool
	works Lister
	log   *slog.Logger
	// Events tells the followers; nil tells nobody.
	Events *notify.Events
	Queue  Queue
}

// NewService returns the Service; Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, works Lister, log *slog.Logger) *Service {
	return &Service{pool: pool, works: works, log: log}
}

// Tracker is a subject a person follows.
type Tracker struct {
	ID        uuid.UUID
	Kind      string
	Name      string
	CreatedAt time.Time
	// PolledAt is when the providers were last asked about it.
	PolledAt *time.Time
}

// subjectKey is what a name is compared by: an author's in the order the
// providers write names, so that "Cornwell, Bernard" is Bernard Cornwell.
func subjectKey(kind, name string) string {
	if kind == KindAuthor {
		name = metadata.NaturalName(name)
	}
	return catalog.Key(name)
}

// Follow has the user follow an author or a series by its name, and asks
// the providers about it at once if nobody followed it before. Following
// it again is the same tracker.
func (s *Service) Follow(ctx context.Context, user uuid.UUID, kind, name string) (Tracker, error) {
	name = strings.TrimSpace(name)
	if !slices.Contains(Kinds, kind) {
		return Tracker{}, fmt.Errorf("no kind %q to follow", kind)
	}
	key := subjectKey(kind, name)
	if key == "" {
		return Tracker{}, ErrBadName
	}
	var t Tracker
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		subject, err := q.PutReleaseSubject(ctx, sqlc.PutReleaseSubjectParams{Kind: kind, Name: name, NameKey: key})
		if err != nil {
			return err
		}
		row, err := q.PutTracker(ctx, sqlc.PutTrackerParams{UserID: user, SubjectID: subject.ID})
		if err != nil {
			return err
		}
		t = Tracker{ID: row.ID, Kind: subject.Kind, Name: subject.Name, CreatedAt: row.CreatedAt, PolledAt: subject.PolledAt}
		if subject.PolledAt == nil && s.Queue != nil {
			_, err = s.Queue.InsertTx(ctx, tx, PollArgs{}, pollOpts)
		}
		return err
	})
	return t, err
}

// Unfollow ends one of the user's trackers. A subject nobody follows any
// more goes, with its releases.
func (s *Service) Unfollow(ctx context.Context, user, id uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteTracker(ctx, sqlc.DeleteTrackerParams{ID: id, UserID: user})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// Trackers are what the user follows, authors first, by name.
func (s *Service) Trackers(ctx context.Context, user uuid.UUID) ([]Tracker, error) {
	rows, err := sqlc.New(s.pool).ListTrackers(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]Tracker, len(rows))
	for i, r := range rows {
		out[i] = Tracker{ID: r.ID, Kind: r.Kind, Name: r.Name, CreatedAt: r.CreatedAt, PolledAt: r.PolledAt}
	}
	return out, nil
}

// Release is a book of a subject the viewer follows.
type Release struct {
	ID          uuid.UUID
	Title       string
	Authors     []string
	Series      *string
	SeriesIndex *float64
	// Date is the first day of the span Precision names: a day, a month or
	// a year. Both are nil where no provider knows when.
	Date      *time.Time
	Precision *string
	Provider  string
	CoverURL  *string
	// SubjectKind and SubjectName are what the viewer follows that lists it.
	SubjectKind string
	SubjectName string
	// InLibrary is a book of a library the viewer sees that is this one.
	InLibrary bool
}

// Releases are the books of what the viewer follows: upcoming ones, the
// soonest first, or those out within the last year, the newest first.
func (s *Service) Releases(ctx context.Context, scope library.Scope, upcoming bool) ([]Release, error) {
	rows, err := sqlc.New(s.pool).ListReleases(ctx, sqlc.ListReleasesParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Upcoming: upcoming,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Release, len(rows))
	for i, r := range rows {
		out[i] = Release{
			ID: r.ID, Title: r.Title, Authors: r.Authors, Series: r.Series, SeriesIndex: r.SeriesIndex,
			Date: r.ReleaseDate, Precision: r.Precision, Provider: r.Provider, CoverURL: r.CoverUrl,
			SubjectKind: r.SubjectKind, SubjectName: r.SubjectName, InLibrary: r.InLibrary,
		}
	}
	return out, nil
}

// Poll asks the providers about the subjects not asked about for a day,
// the longest waiting first, until the deadline, and then tells of the
// releases whose day has come. It reports whether no subject is left.
func (s *Service) Poll(ctx context.Context, deadline time.Time) (complete bool, err error) {
	q := sqlc.New(s.pool)
	for {
		if time.Now().After(deadline) {
			return false, nil
		}
		subject, err := q.NextReleaseSubject(ctx, time.Now().Add(-pollEvery))
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return false, err
		}
		if err := s.pollSubject(ctx, subject); err != nil {
			return false, err
		}
	}
	return true, s.tellDue(ctx)
}

func (s *Service) pollSubject(ctx context.Context, subject sqlc.NextReleaseSubjectRow) error {
	answers, err := s.works.WorksOf(ctx, subject.Kind, subject.Name)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A provider that failed is asked again tomorrow; the others count.
	if err != nil {
		s.log.Warn("a provider did not list the works", "subject", subject.Name, "error", err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		for _, a := range answers {
			first, err := q.PutReleasePoll(ctx, sqlc.PutReleasePollParams{SubjectID: subject.ID, Provider: a.Provider})
			if err != nil {
				return err
			}
			for _, r := range a.Records {
				p, ok := releaseOf(subject.ID, r)
				if !ok {
					continue
				}
				p.Backlog = first
				row, err := q.PutRelease(ctx, p)
				if err != nil {
					return err
				}
				if !row.Inserted || row.Backlog {
					continue
				}
				if stage := stageOf(row.ReleaseDate, row.Precision, today); stage != "" {
					if err := s.tellTx(ctx, tx, row.ID, stage, row.FirstSeenAt, p.Title, p.Authors, subject.Name, row.ReleaseDate, row.Precision); err != nil {
						return err
					}
				}
			}
		}
		return q.MarkReleaseSubjectPolled(ctx, subject.ID)
	})
}

// tellDue tells the followers of releases whose day has come that they are
// out: those who followed before that day.
func (s *Service) tellDue(ctx context.Context) error {
	due, err := sqlc.New(s.pool).ListDueReleases(ctx)
	if err != nil {
		return err
	}
	day := "day"
	for _, r := range due {
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			return s.tellTx(ctx, tx, r.ID, stageOut, *r.ReleaseDate, r.Title, r.Authors, r.Subject, r.ReleaseDate, &day)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// tellTx records who is told of the release at the stage and queues
// their notification, in the transaction that found it.
func (s *Service) tellTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, stage string, followedBefore time.Time,
	title string, authors []string, subject string, date *time.Time, precision *string) error {
	users, err := sqlc.New(tx).QueueReleaseNotices(ctx, sqlc.QueueReleaseNoticesParams{
		Stage: stage, ReleaseID: id, FollowedBefore: followedBefore, SeesAllRoles: seesAllRoles(),
	})
	if err != nil || len(users) == 0 {
		return err
	}
	data := map[string]any{"title": title, "authors": strings.Join(authors, ", "), "subject": subject}
	if date != nil && precision != nil {
		data["date"] = date.Format(time.DateOnly)
		data["precision"] = *precision
	}
	kind := notify.KindReleaseAnnounced
	if stage == stageOut {
		kind = notify.KindReleaseOut
	}
	return s.Events.QueueTx(ctx, tx, notify.Event{Kind: kind, Data: data, Link: "/releases", Recipients: users})
}

func seesAllRoles() []string {
	var out []string
	for _, r := range []string{auth.RoleAdmin, auth.RoleEditor, auth.RoleReader} {
		if auth.Allows(r, auth.StorageManage) {
			out = append(out, r)
		}
	}
	return out
}

// stageOf is what a release newly listed is news as: announced while its
// day, month or year is still to come, out if that span ended within
// recentlyOut, and nothing for an older book or one without a date.
func stageOf(date *time.Time, precision *string, today time.Time) string {
	if date == nil || precision == nil {
		return ""
	}
	end := date.AddDate(0, 0, 1)
	switch *precision {
	case "month":
		end = date.AddDate(0, 1, 0)
	case "year":
		end = date.AddDate(1, 0, 0)
	}
	switch {
	case end.After(today.AddDate(0, 0, 1)):
		return stageAnnounced
	case !end.Before(today.Add(-recentlyOut)):
		return stageOut
	}
	return ""
}

// releaseOf is a provider's record as a release of the subject; a record
// without a title is none.
func releaseOf(subject uuid.UUID, r metadata.Record) (sqlc.PutReleaseParams, bool) {
	title := strings.TrimSpace(r.Title)
	key := titleKey(title)
	if key == "" {
		return sqlc.PutReleaseParams{}, false
	}
	p := sqlc.PutReleaseParams{
		SubjectID: subject, DedupeKey: key, Title: title, Authors: []string{}, AuthorKeys: []string{}, Isbns: []string{},
		Provider: r.Provider, ProviderID: r.ID, SeriesIndex: r.SeriesIndex,
	}
	for _, c := range r.Contributors {
		if c.Role != catalog.RoleAuthor {
			continue
		}
		p.Authors = append(p.Authors, c.Name)
		for _, k := range metadata.NameKeys(c.Name) {
			if k != "" && !slices.Contains(p.AuthorKeys, k) {
				p.AuthorKeys = append(p.AuthorKeys, k)
			}
		}
	}
	for _, id := range r.Identifiers {
		if id.Type == catalog.IDISBN {
			p.Isbns = append(p.Isbns, id.Value)
		}
	}
	if r.Series != "" {
		p.Series = &r.Series
	}
	if r.CoverURL != "" {
		p.CoverUrl = &r.CoverURL
	}
	p.ReleaseDate, p.Precision = publishedDate(r.Published)
	if placeholderDay(p.ReleaseDate, p.Precision, time.Now()) {
		year := "year"
		p.Precision = &year
	}
	return p, true
}

// placeholderDay is a first of January more than a year ahead, which
// providers give a book they know no day for (Hardcover: 2030-01-01):
// it says the year at most.
func placeholderDay(date *time.Time, precision *string, now time.Time) bool {
	return date != nil && precision != nil && *precision == "day" &&
		date.Month() == time.January && date.Day() == 1 && date.After(now.AddDate(1, 0, 0))
}

// titleKey compares titles without what providers add after a colon or in
// brackets: a subtitle, or the series and its number.
func titleKey(title string) string {
	if i := strings.IndexAny(title, ":(["); i > 0 {
		title = title[:i]
	}
	return catalog.Key(title)
}

// publishedDate reads a record's Published, "2010", "2010-08" or
// "2010-08-31", as the first day of that span and its precision.
func publishedDate(s string) (*time.Time, *string) {
	for _, l := range []struct{ layout, precision string }{
		{time.DateOnly, "day"}, {"2006-01", "month"}, {"2006", "year"},
	} {
		if t, err := time.Parse(l.layout, s); err == nil {
			precision := l.precision
			return &t, &precision
		}
	}
	return nil, nil
}
