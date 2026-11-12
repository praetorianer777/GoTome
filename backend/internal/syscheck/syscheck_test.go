package syscheck

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestAFailureIsToldOnceAfterTenMinutesAndAgainAfterRecovery(t *testing.T) {
	failing := true
	c := &Collector{Readiness: func(context.Context) []Check {
		if failing {
			return []Check{{Name: "jobs", Status: Failing, Message: "stopped"}}
		}
		return []Check{{Name: "jobs", Status: OK}}
	}}
	var told []string
	w := &Watcher{Collector: c, Log: slog.New(slog.DiscardHandler), Tell: func(_ context.Context, c Check) error {
		told = append(told, c.Name)
		return nil
	}}
	start := time.Now()
	ctx := context.Background()

	w.Step(ctx, start)
	w.Step(ctx, start.Add(9*time.Minute))
	if len(told) != 0 {
		t.Fatalf("told after nine minutes: %v", told)
	}
	w.Step(ctx, start.Add(10*time.Minute))
	w.Step(ctx, start.Add(30*time.Minute))
	if len(told) != 1 {
		t.Fatalf("told %v, want once", told)
	}

	failing = false
	w.Step(ctx, start.Add(31*time.Minute))
	failing = true
	w.Step(ctx, start.Add(32*time.Minute))
	w.Step(ctx, start.Add(43*time.Minute))
	if len(told) != 2 {
		t.Errorf("told %v, want again after a recovery", told)
	}
}

func TestChecksComeInTheOrderTheDetailsListThem(t *testing.T) {
	c := &Collector{
		Readiness: func(context.Context) []Check {
			return []Check{{Name: "data_dir"}, {Name: "database"}}
		},
	}
	got := c.Collect(context.Background()).Checks
	if len(got) != 2 || got[0].Name != "database" {
		t.Errorf("order: %v", got)
	}
}
