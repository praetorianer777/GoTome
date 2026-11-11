//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// isbnFor is a valid ISBN-13 for the nth book.
func isbnFor(n int) string {
	digits := fmt.Sprintf("978%09d", n)
	sum := 0
	for i, c := range digits {
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return digits + strconv.Itoa((10-sum%10)%10)
}

// lampProvider knows the book "Lamp N" and, beside it, another book by
// someone else whose title is "Lamp".
type lampProvider struct{}

func (lampProvider) Name() string            { return "lamps" }
func (lampProvider) Limits() metadata.Limits { return metadata.Limits{} }

func (lampProvider) Lookup(context.Context, metadata.Web, catalog.Identifier) ([]metadata.Record, error) {
	return nil, metadata.ErrNotFound
}

func (lampProvider) Search(_ context.Context, _ metadata.Web, q metadata.Query) ([]metadata.Record, error) {
	_, number, _ := strings.Cut(q.Title, " ")
	n, err := strconv.Atoi(number)
	if err != nil {
		return nil, metadata.ErrNotFound
	}
	own, _ := catalog.NormalizeIdentifier(catalog.IDISBN, isbnFor(n))
	other, _ := catalog.NormalizeIdentifier(catalog.IDISBN, isbnFor(100000+n))
	return []metadata.Record{
		{ID: "other", Title: "Lamp", Language: "en", Identifiers: []catalog.Identifier{other},
			Contributors: []catalog.NewContributor{{Name: "Someone Else", Role: catalog.RoleAuthor}}},
		{ID: "own", Title: q.Title, Language: "en", Identifiers: []catalog.Identifier{own},
			Contributors: []catalog.NewContributor{{Name: q.Authors[0], Role: catalog.RoleAuthor}}},
	}, nil
}

type recordingQueue struct {
	mu   sync.Mutex
	runs []metadata.RunArgs
}

func (q *recordingQueue) Insert(_ context.Context, args river.JobArgs, _ jobs.InsertOpts) (jobs.Inserted, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.runs = append(q.runs, args.(metadata.RunArgs))
	return jobs.Inserted{}, nil
}

func (a *app) sendRaw(c *http.Client, method, path string, body []byte) (int, []byte) {
	a.t.Helper()
	req, err := http.NewRequest(method, a.url+httpapi.APIPrefix+path, bytes.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func TestTheMatchScoreIsLearnedMeasuredExportedAndImported(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	ctx := context.Background()
	for n := 1; n <= 100; n++ {
		isbn, _ := catalog.NormalizeIdentifier(catalog.IDISBN, isbnFor(n))
		if _, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: fmt.Sprintf("Lamp %d", n), Language: "en",
			Contributors: []catalog.NewContributor{{Name: "Ada Quill"}}, Identifiers: []catalog.Identifier{isbn},
		}); err != nil {
			t.Fatal(err)
		}
	}
	queue := &recordingQueue{}
	svc := metadata.NewService(a.pool, []metadata.Provider{lampProvider{}}, metadata.Options{
		AllowPrivate: true,
		Weights:      func(ctx context.Context) string { v, _ := a.settings.Text(ctx, settings.ScoreWeights); return v },
	})
	svc.Queue = queue
	a.server.Metadata = svc

	if status, _, _ := a.call(reader, http.MethodGet, "/metadata/scoring", nil); status != http.StatusForbidden {
		t.Errorf("a reader sees the scoring: %d", status)
	}
	status, body, _ := a.call(admin, http.MethodGet, "/metadata/scoring", nil)
	if status != 200 || body["using"] != "handSet" || body["booksWithIsbn"] != float64(100) || body["learned"] != nil {
		t.Fatalf("at the start: %d %v", status, body)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/metadata/scoring/export", nil); status != http.StatusNotFound {
		t.Errorf("an export before a learning: %d", status)
	}

	if status, _, _ := a.call(admin, http.MethodPost, "/metadata/scoring/learn", map[string]any{"books": 100}); status != http.StatusAccepted {
		t.Fatalf("learn: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/metadata/scoring/learn", map[string]any{"books": 100}); status != http.StatusConflict {
		t.Errorf("learning twice at once: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/metadata/scoring/learn", map[string]any{"books": 100000}); status != http.StatusUnprocessableEntity {
		t.Errorf("too many books: %d", status)
	}
	if len(queue.runs) != 1 || queue.runs[0].Run != "learn" || queue.runs[0].Books != 100 {
		t.Fatalf("queued %+v", queue.runs)
	}
	if err := svc.Run(ctx, "learn", 100); err != nil {
		t.Fatal(err)
	}

	_, body, _ = a.call(admin, http.MethodGet, "/metadata/scoring", nil)
	if body["using"] != "handSet" {
		t.Errorf("learning switched the weights by itself: %v", body["using"])
	}
	// Learned weights are used only once someone chooses them.
	if status, _, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{"metadata.scoreWeights": "learned"}}); status != http.StatusOK {
		t.Fatalf("choosing the learned weights: %d", status)
	}
	_, body, _ = a.call(admin, http.MethodGet, "/metadata/scoring", nil)
	learned, _ := body["learned"].(map[string]any)
	run, _ := body["learn"].(map[string]any)
	report, _ := body["learnReport"].(map[string]any)
	if body["using"] != "learned" || learned["books"] != float64(100) || run["state"] != "done" || run["done"] != float64(100) || report == nil {
		t.Fatalf("after learning: %v", body)
	}
	if report["right"] != float64(100) || report["candidates"] != float64(200) {
		t.Errorf("the report: %v", report)
	}
	held, _ := report["learned"].(map[string]any)
	if held == nil || held["all"].(map[string]any)["right"].(float64) < 1 {
		t.Errorf("the held-out books: %v", report["learned"])
	}

	status, exported := a.sendRaw(admin, http.MethodGet, "/metadata/scoring/export", nil)
	if status != 200 || !strings.Contains(string(exported), `"featureVersion": 1`) {
		t.Fatalf("export: %d %s", status, exported)
	}
	for _, private := range []string{"Lamp", "Ada", "Quill", "Someone", "978", lib.String()} {
		if strings.Contains(string(exported), private) {
			t.Errorf("the export names %q:\n%s", private, exported)
		}
	}

	if status, _ := a.sendRaw(admin, http.MethodPut, "/metadata/scoring/import", bytes.Replace(exported, []byte(`"featureVersion": 1`), []byte(`"featureVersion": 2`), 1)); status != http.StatusUnprocessableEntity {
		t.Errorf("another version of the features: %d", status)
	}
	if status, _ := a.sendRaw(admin, http.MethodPut, "/metadata/scoring/import", []byte(`{"title": "Emma"}`)); status != http.StatusUnprocessableEntity {
		t.Errorf("not a file of weights: %d", status)
	}
	if status, _ := a.sendRaw(admin, http.MethodPut, "/metadata/scoring/import", exported); status != http.StatusNoContent {
		t.Fatalf("import: %d", status)
	}

	if status, _, _ := a.call(admin, http.MethodPost, "/metadata/scoring/measure", map[string]any{"books": 50}); status != http.StatusAccepted {
		t.Fatalf("measure: %d", status)
	}
	if err := svc.Run(ctx, "measure", 50); err != nil {
		t.Fatal(err)
	}
	_, body, _ = a.call(admin, http.MethodGet, "/metadata/scoring", nil)
	measured, _ := body["measureReport"].(map[string]any)
	variants, _ := measured["variants"].([]any)
	var names []string
	for _, v := range variants {
		names = append(names, v.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "handSet,learned,imported" || measured["sample"] != float64(50) {
		t.Fatalf("the measure compares %v: %v", names, measured)
	}
	if body["imported"] == nil {
		t.Errorf("the imported weights are not listed: %v", body)
	}

	if status, _, _ := a.call(admin, http.MethodDelete, "/metadata/scoring/import", nil); status != http.StatusNoContent {
		t.Errorf("forgetting the import: %d", status)
	}
	_, body, _ = a.call(admin, http.MethodGet, "/metadata/scoring", nil)
	if body["imported"] != nil {
		t.Errorf("the import stays: %v", body["imported"])
	}

	if status, _, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{"metadata.scoreWeights": "nonsense"}}); status != http.StatusUnprocessableEntity {
		t.Errorf("an unknown choice of weights: %d", status)
	}
}

func TestALibraryInGermanRanksWithTheShippedWeightsOnceLearnedOnesAreChosen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Regal", "shared"))
	ctx := context.Background()
	for _, language := range []string{"de", "de-AT", "de", "en"} {
		if _, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
			LibraryID: lib, Title: "Ein Buch " + uuid.NewString(), Language: language,
			Contributors: []catalog.NewContributor{{Name: "Ada Quill"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	a.server.Metadata = metadata.NewService(a.pool, []metadata.Provider{lampProvider{}}, metadata.Options{
		AllowPrivate: true,
		Weights:      func(ctx context.Context) string { v, _ := a.settings.Text(ctx, settings.ScoreWeights); return v },
	})
	using := func() string {
		_, body, _ := a.call(admin, http.MethodGet, "/metadata/scoring", nil)
		s, _ := body["using"].(string)
		return s
	}
	if got := using(); got != "handSet" {
		t.Errorf("before anyone chooses, a library ranks with %q", got)
	}
	if status, _, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.ScoreWeights: "learned"}}); status != 200 {
		t.Fatalf("choosing the learned weights: %d", status)
	}
	if got := using(); got != "profile:german" {
		t.Errorf("a library mostly in German, learned weights chosen, ranks with %q", got)
	}
	if status, _, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.ScoreWeights: "handSet"}}); status != 200 {
		t.Fatalf("setting the weights: %d", status)
	}
	if got := using(); got != "handSet" {
		t.Errorf("the hand-set weights, when chosen: %q", got)
	}
}
