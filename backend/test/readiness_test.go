//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

func TestReadyzSaysMigratingWhileMigrationsRun(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewEmpty(t)
	ready := httpapi.NewReadiness()
	ready.Set(httpapi.CheckDatabase, pool.Ping)
	h := httpapi.StartupRoutes(ready, slog.New(slog.DiscardHandler))

	answer := func() (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, httpapi.ReadyPath, nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return rec.Code, body
	}

	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := db.MigrateWithProgress(ctx, pool, func(applied, total int) {
			ready.Migrating(applied, total)
			if applied == 1 {
				close(held)
				<-release
			}
		})
		done <- err
	}()
	<-held

	code, body := answer()
	if code != http.StatusServiceUnavailable || body["status"] != "migrating" {
		t.Errorf("while migrating: %d %v", code, body)
	}
	if applied, total := body["migrationsApplied"], body["migrationsTotal"]; applied != float64(1) || total.(float64) < 2 {
		t.Errorf("progress = %v of %v", applied, total)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ready.Migrated()
	for _, n := range []string{httpapi.CheckSearchIndex, httpapi.CheckJobs, httpapi.CheckDataDir} {
		ready.Set(n, func(context.Context) error { return nil })
	}
	if code, body := answer(); code != http.StatusOK || body["status"] != "ready" {
		t.Errorf("after migrating: %d %v", code, body)
	}
}

func (a *app) readyz() (int, map[string]any, string) {
	a.t.Helper()
	resp, err := http.Get(a.url + httpapi.ReadyPath)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		a.t.Fatalf("/readyz: %s", raw)
	}
	return resp.StatusCode, body, string(raw)
}

func TestReadyzNamesTheOneComponentThatFails(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	if code, body, raw := a.readyz(); code != 200 || body["status"] != "ready" {
		t.Fatalf("a healthy app: %d %s", code, raw)
	}

	dead := dbtest.NewEmpty(t)
	dead.Close()
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(context.Context) error{
		httpapi.CheckDatabase:    dead.Ping,
		httpapi.CheckSearchIndex: func(context.Context) error { return errors.New("/data/search/tantivy is gone") },
		httpapi.CheckJobs:        func(context.Context) error { return errors.New("the job queue has stopped") },
		httpapi.CheckDataDir:     httpapi.DataDirCheck(readOnly),
	}
	for name, check := range cases {
		healthy := map[string]func(context.Context) error{
			httpapi.CheckDatabase: a.pool.Ping, httpapi.CheckSearchIndex: a.text.Check,
			httpapi.CheckJobs: func(context.Context) error { return nil }, httpapi.CheckDataDir: func(context.Context) error { return nil },
		}
		for n, c := range healthy {
			a.ready.Set(n, c)
		}
		a.ready.Set(name, check)
		code, body, raw := a.readyz()
		if code != 503 || body["status"] != "not_ready" {
			t.Errorf("%s failing: %d %s", name, code, raw)
		}
		if failing, _ := body["failing"].([]any); len(failing) != 1 || failing[0] != name {
			t.Errorf("%s failing: the answer names %v", name, body["failing"])
		}
		for _, leak := range []string{"/data", "tantivy", "stopped", "password", readOnly} {
			if strings.Contains(raw, leak) {
				t.Errorf("%s failing: the public answer holds %q: %s", name, leak, raw)
			}
		}
	}
}

func TestReadyzAnswersFromResultsKeptForAFewSeconds(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	var asked atomic.Int32
	a.ready.Set(httpapi.CheckJobs, func(context.Context) error { asked.Add(1); return nil })
	for range 10 {
		if code, _, _ := a.readyz(); code != 200 {
			t.Fatalf("status %d", code)
		}
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("ten probes asked the jobs %d times, want once", n)
	}
}

func TestSystemChecksAreForAdministrators(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	a.ready.Set(httpapi.CheckJobs, func(context.Context) error { return errors.New("the job queue has stopped") })

	status, out, _ := a.call(admin, http.MethodGet, "/system/checks", nil)
	if status != 200 {
		t.Fatalf("checks: %d %v", status, out)
	}
	byName := map[string]map[string]any{}
	for _, c := range out["checks"].([]any) {
		c := c.(map[string]any)
		byName[c["name"].(string)] = c
	}
	for _, name := range []string{"database", "migrations", "search_index", "jobs", "data_dir", "embedding", "outbox", "queue", "scans", "disk", "memory"} {
		if byName[name] == nil {
			t.Errorf("no check for %s in %v", name, byName)
		}
	}
	if byName["database"]["status"] != "ok" || byName["database"]["unit"] != "ms" {
		t.Errorf("database: %v", byName["database"])
	}
	if jobs := byName["jobs"]; jobs["status"] != "failing" || jobs["message"] != "the job queue has stopped" {
		t.Errorf("jobs: %v", jobs)
	}

	if status, _, _ := a.call(editor, http.MethodGet, "/system/checks", nil); status != 403 {
		t.Errorf("an editor: %d", status)
	}
	if status, _, _ := a.call(http.DefaultClient, http.MethodGet, "/system/checks", nil); status != 401 {
		t.Errorf("nobody: %d", status)
	}
}

func TestACheckSeesTheOutboxAndTheFailedJobs(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	ctx := context.Background()
	if _, err := a.pool.Exec(ctx, `INSERT INTO fulltext_outbox (created_at) VALUES (now() - interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	_, out, _ := a.call(admin, http.MethodGet, "/system/checks", nil)
	for _, c := range out["checks"].([]any) {
		c := c.(map[string]any)
		if c["name"] == "outbox" && (c["status"] != "degraded" || c["value"].(float64) < 3000) {
			t.Errorf("an hour-old change in the outbox: %v", c)
		}
	}
}
