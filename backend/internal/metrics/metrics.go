// Package metrics writes GOtome's numbers in the Prometheus text format
// (#418): counts of what the libraries hold, the job queues, the HTTP
// requests by route pattern, the database pool and the full-text index's
// backlog. It holds no name, title or path of anything in a library: the
// only labels are fixed words (queues, job kinds, states, route patterns of
// the route table, status classes).
package metrics

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Buckets are the upper bounds of the latency histogram, in seconds.
var Buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// cacheFor is how long the counts read from the database are reused: a
// scrape every few seconds then costs the database one set of counting
// queries a quarter minute.
const cacheFor = 15 * time.Second

type requestKey struct {
	Route, Method, Class string
}

type histogram struct {
	buckets []uint64
	count   uint64
	sum     float64
}

// Service collects and writes the metrics.
type Service struct {
	Pool    *pgxpool.Pool
	Version string

	mu       sync.Mutex
	requests map[requestKey]*histogram

	cacheMu sync.Mutex
	cached  *snapshot
	cachedA time.Time
	now     func() time.Time
}

// Observe records one answered request. Route is the pattern the route
// table matched, never the path asked for; empty for a path no route took.
func (s *Service) Observe(route, method string, status int, took time.Duration) {
	if s == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}
	k := requestKey{Route: route, Method: method, Class: strconv.Itoa(status/100) + "xx"}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requests == nil {
		s.requests = map[requestKey]*histogram{}
	}
	h := s.requests[k]
	if h == nil {
		h = &histogram{buckets: make([]uint64, len(Buckets))}
		s.requests[k] = h
	}
	secs := took.Seconds()
	for i, b := range Buckets {
		if secs <= b {
			h.buckets[i]++
		}
	}
	h.count++
	h.sum += secs
}

type labelled struct {
	labels []string
	value  float64
}

// snapshot is what one set of counting queries found.
type snapshot struct {
	books, users, disabledUsers, libraries float64
	files                                  []labelled
	jobs                                   []labelled
	failedJobs                             []labelled
	outbox                                 float64
	scans                                  []labelled
	lastScan                               float64
	hooks                                  []labelled
}

func (s *Service) read(ctx context.Context) (*snapshot, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	if s.cached != nil && now().Sub(s.cachedA) < cacheFor {
		return s.cached, nil
	}
	snap, err := s.count(ctx)
	if err != nil {
		return nil, err
	}
	s.cached, s.cachedA = snap, now()
	return snap, nil
}

func (s *Service) count(ctx context.Context) (*snapshot, error) {
	var snap snapshot
	one := func(dst *float64, sql string) error {
		var n int64
		if err := s.Pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			return err
		}
		*dst = float64(n)
		return nil
	}
	group := func(dst *[]labelled, sql string) error {
		rows, err := s.Pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			var n int64
			if err := rows.Scan(&a, &b, &n); err != nil {
				return err
			}
			*dst = append(*dst, labelled{[]string{a, b}, float64(n)})
		}
		return rows.Err()
	}
	steps := []func() error{
		func() error {
			return one(&snap.books, `SELECT count(*) FROM books WHERE deleted_at IS NULL AND merged_into_id IS NULL AND NOT placeholder`)
		},
		func() error { return one(&snap.users, `SELECT count(*) FROM users WHERE disabled_at IS NULL`) },
		func() error {
			return one(&snap.disabledUsers, `SELECT count(*) FROM users WHERE disabled_at IS NOT NULL`)
		},
		func() error { return one(&snap.libraries, `SELECT count(*) FROM libraries`) },
		func() error {
			return group(&snap.files, `SELECT extract_state, CASE WHEN missing_at IS NULL THEN 'present' ELSE 'missing' END, count(*)
				FROM book_files WHERE trashed_at IS NULL GROUP BY 1, 2`)
		},
		func() error {
			return group(&snap.jobs, `SELECT queue, state::text, count(*) FROM river_job
				WHERE state IN ('available', 'running', 'retryable', 'scheduled') GROUP BY 1, 2`)
		},
		func() error {
			return group(&snap.failedJobs, `SELECT queue, kind, count(*) FROM river_job WHERE state = 'discarded' GROUP BY 1, 2`)
		},
		func() error { return one(&snap.outbox, `SELECT count(*) FROM fulltext_outbox`) },
		func() error {
			rows, err := s.Pool.Query(ctx, `SELECT state, count(*) FROM library_scans GROUP BY 1`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var st string
				var n int64
				if err := rows.Scan(&st, &n); err != nil {
					return err
				}
				snap.scans = append(snap.scans, labelled{[]string{st}, float64(n)})
			}
			return rows.Err()
		},
		func() error {
			return one(&snap.lastScan, `SELECT coalesce((SELECT extract(epoch FROM finished_at - started_at)::bigint
				FROM library_scans WHERE state = 'done' AND started_at IS NOT NULL AND finished_at IS NOT NULL
				ORDER BY finished_at DESC LIMIT 1), 0)`)
		},
		func() error {
			rows, err := s.Pool.Query(ctx, `SELECT status, count(*) FROM webhook_deliveries GROUP BY 1`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var st string
				var n int64
				if err := rows.Scan(&st, &n); err != nil {
					return err
				}
				snap.hooks = append(snap.hooks, labelled{[]string{st}, float64(n)})
			}
			return rows.Err()
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return &snap, nil
}

type writer struct {
	w   io.Writer
	err error
}

func (w *writer) printf(format string, a ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintf(w.w, format, a...)
	}
}

func (w *writer) head(name, kind, help string) {
	w.printf("# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func (w *writer) sample(name string, value float64, labels ...string) {
	w.printf("%s%s %s\n", name, labelText(labels), number(value))
}

func labelText(pairs []string) string {
	if len(pairs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(pairs[i] + `="` + escape(pairs[i+1]) + `"`)
	}
	b.WriteByte('}')
	return b.String()
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func number(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// PoolStats is what the database pool reports.
type PoolStats struct {
	Acquired, Idle, Total, Max int32
	AcquireCount               int64
	EmptyAcquireCount          int64
	CanceledAcquireCount       int64
	AcquireDuration            time.Duration
}

// Write writes every metric in the Prometheus text format.
func (s *Service) Write(ctx context.Context, out io.Writer) error {
	snap, err := s.read(ctx)
	if err != nil {
		return err
	}
	w := &writer{w: out}

	w.head("gotome_build_info", "gauge", "The running version, as a label.")
	w.sample("gotome_build_info", 1, "version", s.Version)

	w.head("gotome_books", "gauge", "Books in the libraries, placeholders and merged or deleted books left out.")
	w.sample("gotome_books", snap.books)
	w.head("gotome_libraries", "gauge", "Libraries.")
	w.sample("gotome_libraries", snap.libraries)
	w.head("gotome_users", "gauge", "Accounts, by whether they are active.")
	w.sample("gotome_users", snap.users, "state", "active")
	w.sample("gotome_users", snap.disabledUsers, "state", "disabled")
	w.head("gotome_files", "gauge", "Book files not in the trash, by extraction state and whether the scan finds them.")
	for _, l := range sorted(snap.files) {
		w.sample("gotome_files", l.value, "extract_state", l.labels[0], "presence", l.labels[1])
	}

	w.head("gotome_jobs", "gauge", "Jobs waiting or running, by queue and state.")
	for _, l := range sorted(snap.jobs) {
		w.sample("gotome_jobs", l.value, "queue", l.labels[0], "state", l.labels[1])
	}
	w.head("gotome_jobs_failed", "gauge", "Jobs that failed for good and have not yet been cleaned away, by queue and kind.")
	for _, l := range sorted(snap.failedJobs) {
		w.sample("gotome_jobs_failed", l.value, "queue", l.labels[0], "kind", l.labels[1])
	}

	w.head("gotome_search_outbox_rows", "gauge", "Changes to chunks the full-text index has not applied yet.")
	w.sample("gotome_search_outbox_rows", snap.outbox)

	w.head("gotome_scans", "gauge", "Library scans on record, by state.")
	for _, l := range sorted(snap.scans) {
		w.sample("gotome_scans", l.value, "state", l.labels[0])
	}
	w.head("gotome_last_scan_duration_seconds", "gauge", "How long the most recently finished scan took.")
	w.sample("gotome_last_scan_duration_seconds", snap.lastScan)

	w.head("gotome_webhook_deliveries", "gauge", "Webhook deliveries on record, by state.")
	for _, l := range sorted(snap.hooks) {
		w.sample("gotome_webhook_deliveries", l.value, "state", l.labels[0])
	}

	if s.Pool != nil {
		st := s.Pool.Stat()
		p := PoolStats{Acquired: st.AcquiredConns(), Idle: st.IdleConns(), Total: st.TotalConns(), Max: st.MaxConns(),
			AcquireCount: st.AcquireCount(), EmptyAcquireCount: st.EmptyAcquireCount(),
			CanceledAcquireCount: st.CanceledAcquireCount(), AcquireDuration: st.AcquireDuration()}
		writePool(w, p)
	}

	s.writeRequests(w)
	return w.err
}

func sorted(l []labelled) []labelled {
	out := slices.Clone(l)
	slices.SortFunc(out, func(a, b labelled) int { return slices.Compare(a.labels, b.labels) })
	return out
}

func writePool(w *writer, p PoolStats) {
	w.head("gotome_db_pool_connections", "gauge", "Database pool connections, by state.")
	w.sample("gotome_db_pool_connections", float64(p.Acquired), "state", "acquired")
	w.sample("gotome_db_pool_connections", float64(p.Idle), "state", "idle")
	w.head("gotome_db_pool_connections_total", "gauge", "Connections the pool holds.")
	w.sample("gotome_db_pool_connections_total", float64(p.Total))
	w.head("gotome_db_pool_connections_max", "gauge", "The most connections the pool opens.")
	w.sample("gotome_db_pool_connections_max", float64(p.Max))
	w.head("gotome_db_pool_acquires_total", "counter", "Times a connection was taken from the pool.")
	w.sample("gotome_db_pool_acquires_total", float64(p.AcquireCount))
	w.head("gotome_db_pool_empty_acquires_total", "counter", "Times taking a connection had to wait for one.")
	w.sample("gotome_db_pool_empty_acquires_total", float64(p.EmptyAcquireCount))
	w.head("gotome_db_pool_canceled_acquires_total", "counter", "Times waiting for a connection was given up.")
	w.sample("gotome_db_pool_canceled_acquires_total", float64(p.CanceledAcquireCount))
	w.head("gotome_db_pool_acquire_seconds_total", "counter", "Time spent waiting for connections.")
	w.sample("gotome_db_pool_acquire_seconds_total", p.AcquireDuration.Seconds())
}

func (s *Service) writeRequests(w *writer) {
	s.mu.Lock()
	keys := make([]requestKey, 0, len(s.requests))
	copies := map[requestKey]histogram{}
	for k, h := range s.requests {
		keys = append(keys, k)
		copies[k] = histogram{buckets: slices.Clone(h.buckets), count: h.count, sum: h.sum}
	}
	s.mu.Unlock()
	slices.SortFunc(keys, func(a, b requestKey) int {
		return slices.Compare([]string{a.Route, a.Method, a.Class}, []string{b.Route, b.Method, b.Class})
	})

	w.head("gotome_http_requests_total", "counter", "HTTP requests answered, by route pattern, method and status class.")
	for _, k := range keys {
		w.sample("gotome_http_requests_total", float64(copies[k].count), "route", k.Route, "method", k.Method, "class", k.Class)
	}
	w.head("gotome_http_request_duration_seconds", "histogram", "How long requests took, by route pattern, method and status class.")
	for _, k := range keys {
		h := copies[k]
		for i, b := range Buckets {
			w.sample("gotome_http_request_duration_seconds_bucket", float64(h.buckets[i]),
				"route", k.Route, "method", k.Method, "class", k.Class, "le", number(b))
		}
		w.sample("gotome_http_request_duration_seconds_bucket", float64(h.count),
			"route", k.Route, "method", k.Method, "class", k.Class, "le", "+Inf")
		w.sample("gotome_http_request_duration_seconds_sum", h.sum, "route", k.Route, "method", k.Method, "class", k.Class)
		w.sample("gotome_http_request_duration_seconds_count", float64(h.count), "route", k.Route, "method", k.Method, "class", k.Class)
	}
}
