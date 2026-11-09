//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
