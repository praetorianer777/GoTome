//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/discover"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// subjectShelf lists the books of a subject from a test server.
type subjectShelf struct{ base string }

func (subjectShelf) Name() string            { return "shelf" }
func (subjectShelf) Limits() metadata.Limits { return metadata.Limits{} }
func (subjectShelf) Lookup(context.Context, metadata.Web, catalog.Identifier) ([]metadata.Record, error) {
	return nil, nil
}
func (subjectShelf) Search(context.Context, metadata.Web, metadata.Query) ([]metadata.Record, error) {
	return nil, nil
}
func (p subjectShelf) BySubject(ctx context.Context, web metadata.Web, subject string) ([]metadata.Record, error) {
	_, err := web.Get(ctx, metadata.Request{URL: p.base + "/subjects/" + subject})
	return nil, err
}

func TestDiscoverProgressNamesTheProviderThatAskedToBeLeftAlone(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "300")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	meta := metadata.NewService(d.pool, []metadata.Provider{subjectShelf{base: srv.URL}}, metadata.Options{AllowPrivate: true})
	d.service = discover.NewService(d.pool, meta, nil, slog.New(slog.DiscardHandler))
	d.service.Enabled = func(context.Context) bool { return true }
	d.server.Discover = d.service

	reader, _ := d.signedIn("reader", "reader")
	progress := func() map[string]any {
		d.t.Helper()
		_, out, _ := d.call(reader, http.MethodGet, "/me/discover", nil)
		p, _ := out["progress"].(map[string]any)
		return p
	}
	if status, _, _ := d.call(reader, http.MethodPost, "/me/discover/run", nil); status != 202 {
		t.Fatalf("run: %d", status)
	}
	if backing, _ := progress()["backingOff"].([]any); len(backing) != 0 {
		t.Errorf("before any request: %v", backing)
	}

	if _, err := meta.SubjectsOf(context.Background(), "sea"); err == nil {
		t.Fatal("a provider answering 429 gave no error")
	}
	backing, _ := progress()["backingOff"].([]any)
	if len(backing) != 1 {
		t.Fatalf("backingOff = %v", backing)
	}
	b := backing[0].(map[string]any)
	until, err := time.Parse(time.RFC3339, b["until"].(string))
	if b["provider"] != "shelf" || err != nil || time.Until(until) < 4*time.Minute || time.Until(until) > 6*time.Minute {
		t.Errorf("backingOff = %v", b)
	}
}

// insertions is a job queue that keeps what it was given.
type insertions struct {
	mu   sync.Mutex
	args []river.JobArgs
}

func (q *insertions) Insert(_ context.Context, args river.JobArgs, _ jobs.InsertOpts) (jobs.Inserted, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.args = append(q.args, args)
	return jobs.Inserted{}, nil
}

func (q *insertions) learns() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.args)
}

func TestTheScoreIsLearnedWhenDueAndNotOtherwise(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	ctx := context.Background()
	on := false
	queue := &insertions{}
	svc := metadata.NewService(a.pool, nil, metadata.Options{LearnScore: func(context.Context) bool { return on }})
	svc.Queue = queue
	due := func(want int, why string) {
		t.Helper()
		if err := svc.LearnIfDue(ctx); err != nil {
			t.Fatalf("%s: %v", why, err)
		}
		if got := queue.learns(); got != want {
			t.Fatalf("%s: %d learnings queued, want %d", why, got, want)
		}
	}
	seed := func(n int) {
		t.Helper()
		if _, err := a.pool.Exec(ctx, `
			WITH made AS (
				INSERT INTO books (library_id, title, sort_title, title_key)
				SELECT $1, 'Learner ' || g, 'Learner ' || g, 'learner ' || g FROM generate_series(1, $2) g
				RETURNING id)
			INSERT INTO book_identifiers (book_id, type, value)
			SELECT id, 'isbn', '978' || lpad(row_number() OVER ()::text, 10, '0') FROM made`, lib, n); err != nil {
			t.Fatal(err)
		}
	}

	seed(metadata.LearnFrom)
	due(0, "the setting is off")
	on = true
	if _, err := a.pool.Exec(ctx, `DELETE FROM book_identifiers WHERE value IN (SELECT value FROM book_identifiers LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	due(0, "one book short")
	seed(1)
	due(1, "enough books and nothing learned")
	due(1, "a learning already waiting")
	if args, ok := queue.args[0].(metadata.RunArgs); !ok || args.Run != "learn" || args.Books != metadata.DefaultLearnBooks {
		t.Errorf("queued %+v", queue.args[0])
	}

	finished := func(age string) {
		t.Helper()
		if _, err := a.pool.Exec(ctx, `UPDATE match_runs SET state = 'done', finished_at = now() - $1::interval WHERE kind = 'learn'`, age); err != nil {
			t.Fatal(err)
		}
	}
	finished("1 day")
	due(1, "learned yesterday")
	finished("8 days")
	due(2, "learned over a week ago")
}
