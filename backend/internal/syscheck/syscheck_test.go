package syscheck

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/resources"
	"github.com/praetorianer777/gotome/backend/internal/similar"
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

func TestProvidersAreChecksOfTheEnabledOnesByTheirLastRequest(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	c := &Collector{Providers: func(context.Context) []metadata.ProviderState {
		return []metadata.ProviderState{
			{Name: "alpha", Enabled: true},
			{Name: "beta", Enabled: false, Last: &metadata.Outcome{At: at, Err: "ignored"}},
			{Name: "gamma", Enabled: true, Last: &metadata.Outcome{At: at}},
			{Name: "delta", Enabled: true, Last: &metadata.Outcome{At: at, Err: "HTTP 503"}},
		}
	}}
	got := c.Collect(context.Background()).Checks
	byName := map[string]Check{}
	var names []string
	for _, ch := range got {
		byName[ch.Name] = ch
		names = append(names, ch.Name)
	}
	if want := []string{"provider:alpha", "provider:delta", "provider:gamma"}; !slices.Equal(names, want) {
		t.Fatalf("%v, want %v", names, want)
	}
	if a := byName["provider:alpha"]; a.Status != OK || a.Message != "" || a.CheckedAt.IsZero() {
		t.Errorf("never asked: %+v", a)
	}
	if g := byName["provider:gamma"]; g.Status != OK || !g.CheckedAt.Equal(at) {
		t.Errorf("answered: %+v", g)
	}
	if d := byName["provider:delta"]; d.Status != Degraded || d.Message != "its last request failed: HTTP 503" || !d.CheckedAt.Equal(at) {
		t.Errorf("failed: %+v", d)
	}
}

func TestUncheckedIsDegradedNotFailingAndSaysWhy(t *testing.T) {
	before := time.Now()
	ch := unchecked("queue", errors.New("connection refused"))
	if ch.Name != "queue" || ch.Status != Degraded || ch.Message != "could not be checked: connection refused" || ch.CheckedAt.Before(before) {
		t.Errorf("%+v", ch)
	}
}

func TestAnEmbeddingCheckThatCannotBeMadeIsUnchecked(t *testing.T) {
	c := &Collector{Embedding: func(context.Context) (string, bool, similar.Readiness, error) {
		return "", false, similar.Readiness{}, errors.New("settings unreadable")
	}}
	got := c.Collect(context.Background()).Checks
	if len(got) != 1 || got[0].Name != "embedding" || got[0].Status != Degraded || !strings.Contains(got[0].Message, "settings unreadable") {
		t.Errorf("%+v", got)
	}
}

func TestAMachineThatCannotBeCheckedIsUncheckedForDiskAndMemory(t *testing.T) {
	r := resources.NewService(nil)
	r.Embedding = func(context.Context) (embed.Spec, bool, error) { return embed.Spec{}, false, errors.New("no settings") }
	got := (&Collector{Resources: r}).Collect(context.Background()).Checks
	if len(got) != 2 || got[0].Name != "disk" || got[1].Name != "memory" {
		t.Fatalf("%+v", got)
	}
	for _, ch := range got {
		if ch.Status != Degraded || !strings.Contains(ch.Message, "no settings") {
			t.Errorf("%+v", ch)
		}
	}
}

func TestRunStepsEveryIntervalAndStopsWithItsContext(t *testing.T) {
	var mu sync.Mutex
	steps := 0
	c := &Collector{Readiness: func(context.Context) []Check {
		mu.Lock()
		steps++
		mu.Unlock()
		return []Check{{Name: "jobs", Status: OK}}
	}}
	w := &Watcher{Collector: c, Every: time.Millisecond, Log: slog.New(slog.DiscardHandler)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		mu.Lock()
		n := steps
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d steps", n)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	mu.Lock()
	after := steps
	mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if steps != after {
		t.Errorf("stepped after it was stopped: %d then %d", after, steps)
	}
}

func TestRunDoesNotStepWhenItsContextIsAlreadyEnded(t *testing.T) {
	steps := 0
	c := &Collector{Readiness: func(context.Context) []Check { steps++; return nil }}
	w := &Watcher{Collector: c, Every: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx)
	if steps != 0 {
		t.Errorf("%d steps", steps)
	}
}
