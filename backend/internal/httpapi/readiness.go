package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// The components /readyz asks about. Their names are all the public answer
// says of a failure.
const (
	CheckDatabase    = "database"
	CheckMigrations  = "migrations"
	CheckSearchIndex = "search_index"
	CheckJobs        = "jobs"
	CheckDataDir     = "data_dir"
)

var checkOrder = []string{CheckDatabase, CheckMigrations, CheckSearchIndex, CheckJobs, CheckDataDir}

const (
	// checkTimeout bounds each check, so the whole answer comes well within
	// a probe's own deadline.
	checkTimeout = time.Second
	// readinessTTL is how long a set of results answers: a monitor polling
	// every second loads nothing.
	readinessTTL = 3 * time.Second
	// dataDirEvery is how often the data folder is written to.
	dataDirEvery = 30 * time.Second
)

// Phases of the start, before and after the schema is up to date.
const (
	PhaseStarting  = "starting"
	PhaseMigrating = "migrating"
	PhaseServing   = "serving"
)

// CheckResult is what one check found, kept for the administrators' details.
type CheckResult struct {
	Name      string
	OK        bool
	Err       error
	Latency   time.Duration
	CheckedAt time.Time
}

// ReadinessReport is the state of every component at one time.
type ReadinessReport struct {
	Phase   string
	Applied int
	Total   int
	Results []CheckResult
}

// Ready says whether every component holds.
func (r ReadinessReport) Ready() bool {
	for _, c := range r.Results {
		if !c.OK {
			return false
		}
	}
	return true
}

// Failing names the components that do not.
func (r ReadinessReport) Failing() []string {
	var names []string
	for _, c := range r.Results {
		if !c.OK {
			names = append(names, c.Name)
		}
	}
	return names
}

// Readiness is what /readyz answers from. The process serves it as soon as it
// listens, before its components exist: one that has not been set yet fails.
type Readiness struct {
	mu      sync.Mutex
	checks  map[string]func(context.Context) error
	phase   string
	applied int
	total   int
	cached  []CheckResult
	at      time.Time
	now     func() time.Time
}

// NewReadiness starts in the phase before the database is open.
func NewReadiness() *Readiness {
	return &Readiness{checks: map[string]func(context.Context) error{}, phase: PhaseStarting, now: time.Now}
}

// Set registers how a component is checked.
func (r *Readiness) Set(name string, check func(context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks[name] = check
	r.cached = nil
}

// Migrating records that the schema is being brought up to date: applied of
// total migrations are done.
func (r *Readiness) Migrating(applied, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase, r.applied, r.total = PhaseMigrating, applied, total
}

// Migrated records that the schema is up to date.
func (r *Readiness) Migrated() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = PhaseServing
}

// Report runs the checks, at most once in readinessTTL.
func (r *Readiness) Report(ctx context.Context) ReadinessReport {
	r.mu.Lock()
	phase, applied, total := r.phase, r.applied, r.total
	cached := r.cached
	if cached == nil || r.now().Sub(r.at) >= readinessTTL {
		// Held across the run on purpose: concurrent probes share one set
		// of queries.
		cached = r.run(ctx)
		r.cached, r.at = cached, r.now()
	}
	r.mu.Unlock()

	results := make([]CheckResult, 0, len(cached)+1)
	results = append(results, cached[0])
	migrations := CheckResult{Name: CheckMigrations, OK: phase == PhaseServing, CheckedAt: r.now()}
	if !migrations.OK {
		migrations.Err = fmt.Errorf("%s", phase)
	}
	results = append(results, migrations)
	results = append(results, cached[1:]...)
	return ReadinessReport{Phase: phase, Applied: applied, Total: total, Results: results}
}

// run asks every component but the migrations, the database first, each in
// its own goroutine.
func (r *Readiness) run(ctx context.Context) []CheckResult {
	names := make([]string, 0, len(checkOrder)-1)
	for _, n := range checkOrder {
		if n != CheckMigrations {
			names = append(names, n)
		}
	}
	results := make([]CheckResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		check := r.checks[name]
		wg.Go(func() {
			res := CheckResult{Name: name, CheckedAt: time.Now()}
			if check == nil {
				res.Err = fmt.Errorf("not started")
			} else {
				cctx, cancel := context.WithTimeout(ctx, checkTimeout)
				defer cancel()
				started := time.Now()
				res.Err = check(cctx)
				res.Latency = time.Since(started)
			}
			res.OK = res.Err == nil
			results[i] = res
		})
	}
	wg.Wait()
	return results
}

// DataDirCheck writes a small file into dir to see that it can be written to,
// at most every dataDirEvery; between times it repeats its last answer.
func DataDirCheck(dir string) func(context.Context) error {
	var (
		mu   sync.Mutex
		last error
		at   time.Time
	)
	return func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if !at.IsZero() && time.Since(at) < dataDirEvery {
			return last
		}
		last, at = probeWritable(dir), time.Now()
		return last
	}
}

func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".ready-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	rerr := os.Remove(name)
	for _, err := range []error{werr, cerr, rerr} {
		if err != nil {
			return err
		}
	}
	return nil
}

type readyAnswer struct {
	Status  string   `json:"status"`
	Failing []string `json:"failing,omitempty"`
	// Applied and Total count migrations while they run.
	Applied *int `json:"migrationsApplied,omitempty"`
	Total   *int `json:"migrationsTotal,omitempty"`
}

func readyHandler(rd *Readiness, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		report := rd.Report(r.Context())
		if report.Ready() {
			writeJSON(w, r, http.StatusOK, readyAnswer{Status: "ready"})
			return
		}
		answer := readyAnswer{Status: "not_ready", Failing: report.Failing()}
		switch report.Phase {
		case PhaseStarting:
			answer.Status = PhaseStarting
		case PhaseMigrating:
			answer.Status = PhaseMigrating
			answer.Applied, answer.Total = &report.Applied, &report.Total
		}
		for _, c := range report.Results {
			if !c.OK && c.Name != CheckMigrations {
				loggerFrom(r.Context()).Warn("not ready", "component", c.Name, "error", c.Err)
			}
		}
		writeJSON(w, r, http.StatusServiceUnavailable, answer)
	}
}

// StartupRoutes is what the process answers while it starts: liveness and
// readiness, and 503 for everything else. The full routes replace it once
// every service exists, so that a long migration is a process that is alive
// and says what it is doing.
func StartupRoutes(rd *Readiness, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, securityHeaders, logging(log), recovery)
	r.Get(HealthPath, health)
	r.Get(ReadyPath, readyHandler(rd, log))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, ErrUnavailable("GOtome is starting."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, ErrUnavailable("GOtome is starting."))
	})
	return r
}
