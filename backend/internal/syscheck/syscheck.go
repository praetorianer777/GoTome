// Package syscheck gathers how each part of a running GOtome is doing, for the
// administrators' details behind /readyz (#407) and for the notice that a part
// has been failing for a while.
package syscheck

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/resources"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// The status of a component. Only a failing one makes GOtome not ready, and
// only readiness checks can fail; the rest is degraded at worst.
const (
	OK       = "ok"
	Degraded = "degraded"
	Failing  = "failing"
)

// Statuses are every Status, for the API document.
var Statuses = []string{OK, Degraded, Failing}

// Units of a Value.
const (
	UnitMillis  = "ms"
	UnitSeconds = "s"
	UnitCount   = "count"
	UnitBytes   = "bytes"
)

// Check is how one component is doing.
type Check struct {
	// Name is the component: database, migrations, search_index, jobs,
	// data_dir, embedding, provider:<name>, outbox, queue, scans, disk,
	// memory.
	Name   string `json:"name"`
	Status string `json:"status"`
	// Message is a short line for an administrator; empty when all is well.
	Message string `json:"message"`
	// Value is a measurement where one makes sense, in Unit.
	Value     *float64  `json:"value,omitempty"`
	Unit      string    `json:"unit,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Report is every component's check.
type Report struct {
	Checks []Check `json:"checks"`
}

func value(v float64, unit string) (*float64, string) { return &v, unit }

// Collector gathers the checks. Every field but Pool may be nil, and the
// component is then left out.
type Collector struct {
	Pool *pgxpool.Pool
	// Readiness are the five checks /readyz makes.
	Readiness func(ctx context.Context) []Check
	// Embedding says what similar.Service.Runtime does.
	Embedding func(ctx context.Context) (model string, enabled bool, ready similar.Readiness, err error)
	// Providers lists the metadata providers with their last answers.
	Providers func(ctx context.Context) []metadata.ProviderState
	Resources *resources.Service
}

// Stuck and lag limits.
const (
	// OutboxLate is how long the oldest change may wait for the index.
	OutboxLate = 10 * time.Minute
	// StuckJob is how long a job may run before it counts as stuck; the
	// scan and the passes snooze long before.
	StuckJob = 2 * time.Hour
)

// Collect asks every component, side by side, each within a few seconds. A
// component that cannot be asked is degraded, not failing.
func (c *Collector) Collect(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var (
		mu     sync.Mutex
		checks []Check
		wg     sync.WaitGroup
	)
	add := func(cs ...Check) {
		mu.Lock()
		checks = append(checks, cs...)
		mu.Unlock()
	}
	run := func(f func() []Check) { wg.Go(func() { add(f()...) }) }
	if c.Readiness != nil {
		run(func() []Check { return c.Readiness(ctx) })
	}
	if c.Pool != nil {
		run(func() []Check { return []Check{c.outbox(ctx)} })
		run(func() []Check { return []Check{c.queue(ctx)} })
		run(func() []Check { return []Check{c.scans(ctx)} })
	}
	if c.Embedding != nil {
		run(func() []Check { return []Check{c.embedding(ctx)} })
	}
	if c.Providers != nil {
		run(func() []Check { return c.providers(ctx) })
	}
	if c.Resources != nil {
		run(func() []Check { return c.machine(ctx) })
	}
	wg.Wait()
	rank := func(name string) int {
		for i, n := range order {
			if n == name || strings.HasPrefix(name, n+":") {
				return i
			}
		}
		return len(order)
	}
	slices.SortStableFunc(checks, func(a, b Check) int {
		return cmp.Or(cmp.Compare(rank(a.Name), rank(b.Name)), cmp.Compare(a.Name, b.Name))
	})
	return Report{Checks: checks}
}

var order = []string{"database", "migrations", "search_index", "jobs", "data_dir", "embedding", "provider", "outbox", "queue", "scans", "disk", "memory"}

func unchecked(name string, err error) Check {
	return Check{Name: name, Status: Degraded, Message: "could not be checked: " + err.Error(), CheckedAt: time.Now()}
}

func (c *Collector) outbox(ctx context.Context) Check {
	row, err := sqlc.New(c.Pool).OutboxLag(ctx)
	if err != nil {
		return unchecked("outbox", err)
	}
	age := time.Since(row.Oldest)
	if row.Pending == 0 {
		age = 0
	}
	ch := Check{Name: "outbox", Status: OK, CheckedAt: time.Now()}
	ch.Value, ch.Unit = value(age.Seconds(), UnitSeconds)
	if age > OutboxLate {
		ch.Status = Degraded
		ch.Message = fmt.Sprintf("%d changes wait for the search index, the oldest for %s", row.Pending, age.Round(time.Second))
	}
	return ch
}

const queueQuery = `
SELECT count(*) FILTER (WHERE state = 'retryable')::int,
       count(*) FILTER (WHERE state = 'discarded' AND finalized_at > now() - interval '1 day')::int,
       count(*) FILTER (WHERE state = 'running' AND attempted_at < now() - $1::interval)::int
FROM river_job
WHERE state IN ('retryable', 'discarded', 'running')`

// queue counts the jobs that failed or hang. River's tables are not sqlc's.
func (c *Collector) queue(ctx context.Context) Check {
	var retrying, discarded, stuck int
	err := c.Pool.QueryRow(ctx, queueQuery, fmt.Sprintf("%d seconds", int(StuckJob.Seconds()))).Scan(&retrying, &discarded, &stuck)
	if err != nil {
		return unchecked("queue", err)
	}
	ch := Check{Name: "queue", Status: OK, CheckedAt: time.Now()}
	ch.Value, ch.Unit = value(float64(retrying+discarded+stuck), UnitCount)
	if retrying+discarded+stuck > 0 {
		ch.Status = Degraded
		ch.Message = fmt.Sprintf("%d jobs are being retried, %d were given up in the last day, %d run longer than %s", retrying, discarded, stuck, StuckJob)
	}
	return ch
}

func (c *Collector) scans(ctx context.Context) Check {
	n, err := sqlc.New(c.Pool).CountLibrariesWithFailedScan(ctx)
	if err != nil {
		return unchecked("scans", err)
	}
	ch := Check{Name: "scans", Status: OK, CheckedAt: time.Now()}
	ch.Value, ch.Unit = value(float64(n), UnitCount)
	if n > 0 {
		ch.Status = Degraded
		ch.Message = fmt.Sprintf("the last scan of %d libraries failed", n)
	}
	return ch
}

func (c *Collector) embedding(ctx context.Context) Check {
	model, on, ready, err := c.Embedding(ctx)
	if err != nil {
		return unchecked("embedding", err)
	}
	ch := Check{Name: "embedding", Status: OK, CheckedAt: time.Now()}
	switch {
	case !on:
		ch.Message = "off"
	case ready.State == similar.ReadyHere || ready.State == similar.ReadyAfterDownload:
	default:
		ch.Status = Degraded
		ch.Message = fmt.Sprintf("%s cannot run: %s", model, ready.State)
	}
	return ch
}

func (c *Collector) providers(ctx context.Context) []Check {
	var out []Check
	for _, p := range c.Providers(ctx) {
		if !p.Enabled {
			continue
		}
		ch := Check{Name: "provider:" + p.Name, Status: OK, CheckedAt: time.Now()}
		if p.Last != nil {
			ch.CheckedAt = p.Last.At
			if p.Last.Err != "" {
				ch.Status = Degraded
				ch.Message = "its last request failed: " + p.Last.Err
			}
		}
		out = append(out, ch)
	}
	return out
}

func (c *Collector) machine(ctx context.Context) []Check {
	h, err := c.Resources.Check(ctx)
	if err != nil {
		return []Check{unchecked("disk", err), unchecked("memory", err)}
	}
	now := time.Now()
	disk := Check{Name: "disk", Status: OK, CheckedAt: now}
	disk.Value, disk.Unit = value(float64(h.Disk.FreeBytes), UnitBytes)
	memory := Check{Name: "memory", Status: OK, CheckedAt: now}
	memory.Value, memory.Unit = value(float64(h.Memory.MachineBytes), UnitBytes)
	for _, w := range h.Warnings {
		target := &memory
		if w.Kind == resources.KindDisk {
			target = &disk
		}
		target.Status = Degraded
		if target.Message != "" {
			target.Message += "; "
		}
		target.Message += w.Kind
	}
	return []Check{disk, memory}
}

// failingFor is how long a component must fail before administrators hear.
const failingFor = 10 * time.Minute

// Watcher tells, once, of a component that has failed for failingFor, and
// again only after it has recovered.
type Watcher struct {
	Collector *Collector
	// Tell is called for a component to be reported.
	Tell  func(ctx context.Context, c Check) error
	Every time.Duration
	Log   *slog.Logger
	since map[string]time.Time
	told  map[string]bool
}

// Step looks once, at the time given.
func (w *Watcher) Step(ctx context.Context, now time.Time) {
	if w.since == nil {
		w.since, w.told = map[string]time.Time{}, map[string]bool{}
	}
	seen := map[string]bool{}
	for _, c := range w.Collector.Collect(ctx).Checks {
		if c.Status != Failing {
			continue
		}
		seen[c.Name] = true
		if _, ok := w.since[c.Name]; !ok {
			w.since[c.Name] = now
		}
		if w.told[c.Name] || now.Sub(w.since[c.Name]) < failingFor {
			continue
		}
		if err := w.Tell(ctx, c); err != nil {
			w.Log.Warn("could not tell of a failing component", "component", c.Name, "error", err)
			continue
		}
		w.told[c.Name] = true
	}
	for name := range w.since {
		if !seen[name] {
			delete(w.since, name)
			delete(w.told, name)
		}
	}
}

// Run steps every Every until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.Every):
		}
		w.Step(ctx, time.Now())
	}
}
