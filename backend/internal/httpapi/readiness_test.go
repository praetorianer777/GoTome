package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func readyTestServer(t *testing.T, rd *Readiness) http.Handler {
	t.Helper()
	return (&Server{Log: slog.New(slog.DiscardHandler), DB: fakeDB{}, Ready: rd}).Routes()
}

func allFine(rd *Readiness) {
	for _, n := range checkOrder {
		rd.Set(n, func(context.Context) error { return nil })
	}
	rd.Migrated()
}

func TestReadyNamesOnlyTheFailingComponent(t *testing.T) {
	for _, name := range []string{CheckDatabase, CheckSearchIndex, CheckJobs, CheckDataDir} {
		rd := NewReadiness()
		allFine(rd)
		rd.Set(name, func(context.Context) error { return errors.New(errSecret.Error()) })
		rec := do(t, readyTestServer(t, rd), http.MethodGet, ReadyPath)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s failing: status = %d, want 503", name, rec.Code)
		}
		var got readyAnswer
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != "not_ready" || !slices.Equal(got.Failing, []string{name}) {
			t.Errorf("%s failing: answer = %+v", name, got)
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("%s failing: the answer holds the error: %s", name, rec.Body)
		}
	}
}

func TestReadyIsMigratingWhileTheSchemaIsBroughtUp(t *testing.T) {
	rd := NewReadiness()
	allFine(rd)
	rd.Migrating(3, 13)
	h := readyTestServer(t, rd)
	rec := do(t, h, http.MethodGet, ReadyPath)
	var got readyAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || got.Status != "migrating" || got.Applied == nil || *got.Applied != 3 || *got.Total != 13 {
		t.Errorf("status %d, answer %s", rec.Code, rec.Body)
	}
	if alive := do(t, h, http.MethodGet, HealthPath); alive.Code != http.StatusOK {
		t.Errorf("liveness while migrating: %d", alive.Code)
	}
}

func TestTheStartupRoutesAnswerHealthAndReadinessOnly(t *testing.T) {
	rd := NewReadiness()
	h := StartupRoutes(rd, slog.New(slog.DiscardHandler))
	if rec := do(t, h, http.MethodGet, HealthPath); rec.Code != http.StatusOK {
		t.Errorf("health: %d", rec.Code)
	}
	rec := do(t, h, http.MethodGet, ReadyPath)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"starting"`) {
		t.Errorf("ready: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/", "/api/v1/version"} {
		if rec := do(t, h, http.MethodGet, path); rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestReadinessResultsAreKeptForAFewSeconds(t *testing.T) {
	rd := NewReadiness()
	allFine(rd)
	now := time.Now()
	rd.now = func() time.Time { return now }
	calls := 0
	rd.Set(CheckJobs, func(context.Context) error { calls++; return nil })
	for range 5 {
		rd.Report(context.Background())
	}
	if calls != 1 {
		t.Errorf("the check ran %d times in a row, want 1", calls)
	}
	now = now.Add(readinessTTL)
	rd.Report(context.Background())
	if calls != 2 {
		t.Errorf("after the time: %d runs, want 2", calls)
	}
}

func TestDataDirCheckNeedsAWritableFolder(t *testing.T) {
	dir := t.TempDir()
	if err := DataDirCheck(dir)(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ready-*")); len(left) != 0 {
		t.Errorf("the probe left %v", left)
	}
	if err := DataDirCheck(filepath.Join(dir, "missing"))(context.Background()); err == nil {
		t.Error("a missing folder passed")
	}
	if os.Geteuid() != 0 {
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		if err := DataDirCheck(ro)(context.Background()); err == nil {
			t.Error("a read-only folder passed")
		}
	}
}
