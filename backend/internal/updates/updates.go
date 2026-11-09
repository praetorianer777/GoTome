// Package updates learns from GitHub when a newer GOtome is out and tells
// the administrators once per version. It only says so: nothing is
// downloaded or installed.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

const (
	// LatestURL is where GitHub names the latest release that is neither
	// a draft nor a pre-release.
	LatestURL = "https://api.github.com/repos/praetorianer777/GoTome/releases/latest"
	// ReleasePage is where a release's notes are, once the tag is added.
	ReleasePage = "https://github.com/praetorianer777/GoTome/releases/tag/"

	// askEvery is how often GitHub is asked after an answer, retryEvery
	// after a failure.
	askEvery   = 24 * time.Hour
	retryEvery = time.Hour
	timeout    = 20 * time.Second
	maxBody    = 2 << 20
	maxError   = 300
)

// NewClient is the client GitHub is asked with.
func NewClient() *http.Client {
	return outbound.NewClient(outbound.Options{Timeout: timeout})
}

// Service asks GitHub and keeps the answer.
type Service struct {
	Pool     *pgxpool.Pool
	Settings interface {
		Text(ctx context.Context, key string) (string, error)
	}
	Events *notify.Events
	HTTP   *http.Client
	Log    *slog.Logger
	// URL is the address of the latest release; tests name their own.
	URL string
	// Offline asks nobody.
	Offline bool
	// Current is the running version.
	Current string
	Now     func() time.Time
}

// NewService returns a Service for the running build.
func NewService(pool *pgxpool.Pool, store *settings.Store, events *notify.Events, log *slog.Logger) *Service {
	return &Service{
		Pool: pool, Settings: store, Events: events, HTTP: NewClient(), Log: log,
		URL: LatestURL, Current: version.Current().Version, Now: time.Now,
	}
}

// ReleaseStatus is what the web app shows administrators.
type ReleaseStatus struct {
	// Current is the running version.
	Current string `json:"current"`
	// Enabled is whether GitHub is asked at all: not with updates.check
	// off or GOTOME_OFFLINE.
	Enabled bool `json:"enabled"`
	// Latest is the newest release GitHub named, when it has.
	Latest      string     `json:"latest,omitempty"`
	URL         string     `json:"url,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	// Security is whether the latest release's notes have a Security
	// section.
	Security bool `json:"security"`
	// CheckedAt is when GitHub last answered.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// Available is whether Latest is newer than the running version.
	Available bool `json:"available"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) enabled(ctx context.Context) (bool, error) {
	if s.Offline {
		return false, nil
	}
	v, err := s.Settings.Text(ctx, settings.UpdatesCheck)
	return v == "on", err
}

// Status says what is known of the latest release.
func (s *Service) Status(ctx context.Context) (ReleaseStatus, error) {
	on, err := s.enabled(ctx)
	if err != nil {
		return ReleaseStatus{}, err
	}
	st := ReleaseStatus{Current: s.Current, Enabled: on}
	row, err := sqlc.New(s.Pool).GetUpdateCheck(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return ReleaseStatus{}, err
	}
	if row.LatestVersion == nil {
		return st, nil
	}
	st.Latest, st.PublishedAt, st.Security, st.CheckedAt = *row.LatestVersion, row.PublishedAt, row.Security, row.CheckedAt
	st.URL = ReleasePage + "v" + st.Latest
	st.Available = Newer(st.Latest, s.Current)
	return st, nil
}

// Newer is whether version a is a release after version b. Anything that
// is not three numbers, such as a development build, is never newer nor
// older.
func Newer(a, b string) bool {
	x, ok := parse(a)
	if !ok {
		return false
	}
	y, ok := parse(b)
	if !ok {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 6 {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

type githubRelease struct {
	Tag         string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
}

var securitySection = regexp.MustCompile(`(?mi)^#{2,4}[ \t]+security[ \t]*$`)

func (s *Service) fetch(ctx context.Context) (githubRelease, error) {
	var rel githubRelease
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("User-Agent", "GOtome/"+s.Current+" (+https://github.com/praetorianer777/GoTome)")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return rel, err
	}
	if len(body) > maxBody {
		return rel, errors.New("GitHub's answer is too large")
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return rel, fmt.Errorf("GitHub's answer is not a release: %w", err)
	}
	if rel.Draft || rel.Prerelease {
		return rel, errors.New("the latest release is not final")
	}
	if _, ok := parse(rel.Tag); !ok {
		return rel, fmt.Errorf("the latest release is tagged %q", rel.Tag)
	}
	return rel, nil
}

// due is whether GitHub should be asked now.
func due(row sqlc.UpdateCheck, now time.Time) bool {
	if row.AttemptedAt == nil {
		return true
	}
	wait := askEvery
	if row.Error != nil {
		wait = retryEvery
	}
	return now.Sub(*row.AttemptedAt) >= wait
}

// Check asks GitHub if an answer is due, keeps it, and tells the
// administrators of a version they have not been told of. A failed request
// keeps the last answer. It reports whether GitHub was asked.
func (s *Service) Check(ctx context.Context) (bool, error) {
	on, err := s.enabled(ctx)
	if err != nil || !on {
		return false, err
	}
	now := s.now()
	row, err := sqlc.New(s.Pool).GetUpdateCheck(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if err == nil && !due(row, now) {
		return false, nil
	}
	rel, ferr := s.fetch(ctx)
	if ferr != nil {
		return true, s.fail(ctx, row, ferr, now)
	}
	return true, s.record(ctx, rel, now)
}

func (s *Service) fail(ctx context.Context, prev sqlc.UpdateCheck, cause error, now time.Time) error {
	msg := cause.Error()
	if len(msg) > maxError {
		msg = msg[:maxError]
	}
	if prev.Error == nil || *prev.Error != msg {
		s.Log.Warn("could not check for a new GOtome release", "error", msg)
	}
	return db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.EnsureUpdateCheck(ctx); err != nil {
			return err
		}
		return q.RecordUpdateFailure(ctx, sqlc.RecordUpdateFailureParams{Now: now, Error: msg})
	})
}

func (s *Service) record(ctx context.Context, rel githubRelease, now time.Time) error {
	return db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.EnsureUpdateCheck(ctx); err != nil {
			return err
		}
		prev, err := q.LockUpdateCheck(ctx)
		if err != nil {
			return err
		}
		latest := strings.TrimPrefix(rel.Tag, "v")
		security := securitySection.MatchString(rel.Body)
		err = q.RecordUpdateAnswer(ctx, sqlc.RecordUpdateAnswerParams{
			LatestVersion: latest, Url: ReleasePage + rel.Tag, PublishedAt: rel.PublishedAt, Security: security, Now: now,
		})
		if err != nil {
			return err
		}
		if !Newer(latest, s.Current) || (prev.NotifiedVersion != nil && *prev.NotifiedVersion == latest) {
			return nil
		}
		err = s.Events.QueueTx(ctx, tx, notify.Event{
			Kind: notify.KindUpdateAvailable, Link: "/overview",
			Data: map[string]any{"version": latest, "current": s.Current, "security": security, "url": ReleasePage + rel.Tag},
		})
		if err != nil {
			return err
		}
		return q.MarkUpdateNotified(ctx, latest)
	})
}
