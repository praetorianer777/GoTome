// Command search measures the candidates for GOtome's full-text search on
// the benchmark corpus (#14): ParadeDB's pg_search, Postgres's own full-text
// search with a trigram vocabulary for typos, and Bleve. See README.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const usage = `Usage: search <command> [flags]

Commands:
  extract  Read the corpus's EPUBs into chunks
  bench    Build one engine's index at one scale and measure it
  report   Print the results as Markdown tables

Run "search <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "search:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	switch args[0] {
	case "extract":
		fs := flag.NewFlagSet("extract", flag.ContinueOnError)
		dir := fs.String("dir", "/corpus/base", "the base corpus")
		out := fs.String("out", "/corpus/chunks.jsonl.gz", "where the chunks go")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return extract(*dir, *out)
	case "bench":
		return runBench(ctx, args[1:])
	case "report":
		fs := flag.NewFlagSet("report", flag.ContinueOnError)
		dir := fs.String("dir", "results", "the results directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return report(*dir, os.Stdout)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// Result is what one run measured.
type Result struct {
	Engine   string            `json:"engine"`
	Books    int               `json:"books"`
	Chunks   int               `json:"chunks"`
	TextMB   float64           `json:"textMB"`
	Build    Steps             `json:"buildSeconds"`
	Sizes    map[string]int64  `json:"sizes"`
	Memory   map[string]any    `json:"memory"`
	Latency  map[Kind]Latency  `json:"latencyMs"`
	Rebuild  float64           `json:"rebuildSeconds,omitempty"`
	Samples  []Sample          `json:"samples"`
	Plans    map[Kind]string   `json:"plans"`
	Started  time.Time         `json:"started"`
	Settings map[string]string `json:"settings,omitempty"`
	Errors   map[string]string `json:"errors,omitempty"`
}

// Latency summarises the timed runs of one kind of query.
type Latency struct {
	Runs int     `json:"runs"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Max  float64 `json:"max"`
}

// Sample is what a query found, for judging stemming, typo repair and
// snippets by eye.
type Sample struct {
	Kind  Kind     `json:"kind"`
	Query string   `json:"query"`
	Want  string   `json:"want"`
	Found int      `json:"found"`
	Top   []string `json:"top"`
}

// copyStride separates the book IDs of two copies of the corpus.
const copyStride = 10000

func runBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	engine := fs.String("engine", "", "pgsearch, fts or bleve")
	books := fs.Int("books", 0, "books to index, copying the corpus as often as needed; 0 is the corpus once")
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the file extract wrote")
	dsn := fs.String("dsn", "", "the database, for pgsearch and fts")
	dir := fs.String("index", "/index", "the index directory, for bleve")
	cgroup := fs.String("cgroup", "", "the cgroup directory of the container the engine runs in")
	repeat := fs.Int("repeat", 5, "timed runs of each query")
	out := fs.String("out", "", "the result file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	titles := map[int]string{}
	base := 0
	if err := readBooks(*chunks, func(b Book) error {
		titles[b.ID] = b.Title
		base++
		return nil
	}); err != nil {
		return err
	}
	target := *books
	if target == 0 {
		target = base
	}
	res := Result{Engine: *engine, Books: target, Started: time.Now().UTC(), Memory: map[string]any{}, Errors: map[string]string{}}
	src := func(fn func(Row) error) error {
		n, rows, chars := 0, 0, 0
		for copy := 0; n < target; copy++ {
			err := readBooks(*chunks, func(b Book) error {
				if n >= target {
					return nil
				}
				n++
				id := copy*copyStride + b.ID
				for i, text := range b.Chunks {
					rows++
					chars += len(text)
					if err := fn(Row{ID: int64(id)*copyStride + int64(i), Book: id, Library: int32(id % Libraries), Lang: b.Lang, Text: text}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		res.Chunks, res.TextMB = rows, float64(chars)/1e6
		return nil
	}

	var e Engine
	switch *engine {
	case "pgsearch", "fts":
		pool, err := pgxpool.New(ctx, *dsn)
		if err != nil {
			return err
		}
		if *engine == "pgsearch" {
			e = &PGSearch{pool: pool}
		} else {
			e = &FTS{pool: pool}
		}
		res.Settings = settings(ctx, pool)
	case "bleve":
		e = &Bleve{dir: *dir}
	default:
		return fmt.Errorf("unknown engine %q", *engine)
	}
	defer e.Close()

	fmt.Printf("%s: building %d books\n", *engine, target)
	mem := WatchMemory(*cgroup)
	t := time.Now()
	steps, err := e.Build(ctx, src)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	steps["total"] = since(t)
	res.Build = steps
	res.Memory["build"] = mem.Stop()
	fmt.Printf("%s: built in %.0f s\n", *engine, steps["total"])
	if res.Sizes, err = e.Sizes(ctx); err != nil {
		return fmt.Errorf("sizes: %w", err)
	}

	mem = WatchMemory(*cgroup)
	res.Latency, res.Samples = measure(ctx, e, *repeat, titles, res.Errors)
	res.Memory["queries"] = mem.Stop()
	res.Plans = map[Kind]string{}
	for _, k := range Kinds {
		i := slices.IndexFunc(Queries, func(q Query) bool { return q.Kind == k })
		plan, err := e.Plan(ctx, Queries[i])
		if errors.Is(err, errUnsupported) {
			continue
		}
		if err != nil {
			plan = "error: " + err.Error()
		}
		res.Plans[k] = plan
	}

	mem = WatchMemory(*cgroup)
	d, err := e.Rebuild(ctx)
	if err != nil {
		res.Errors["rebuild"] = err.Error()
	}
	res.Rebuild = d.Seconds()
	res.Memory["rebuild"] = mem.Stop()
	res.Memory["oomKills"] = OOMKills(*cgroup)

	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s: done, writing %s\n", *engine, *out)
	return os.WriteFile(*out, append(b, '\n'), 0o644)
}

// measure runs every query once to warm it up and then repeat times, and
// keeps what the first run found.
func measure(ctx context.Context, e Engine, repeat int, titles map[int]string, errs map[string]string) (map[Kind]Latency, []Sample) {
	times := map[Kind][]float64{}
	var samples []Sample
	for _, q := range Queries {
		qctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		hits, err := e.Search(qctx, q)
		cancel()
		if errors.Is(err, errUnsupported) {
			continue
		}
		if err != nil {
			errs[string(q.Kind)+": "+q.Text] = err.Error()
			continue
		}
		s := Sample{Kind: q.Kind, Query: q.Text, Want: q.Want, Found: len(hits)}
		for _, h := range hits[:min(3, len(hits))] {
			line := fmt.Sprintf("%s (book %d, %.2f)", titles[h.Book%copyStride], h.Book, h.Score)
			if h.Snippet != "" {
				line += ": " + h.Snippet
			}
			s.Top = append(s.Top, line)
		}
		samples = append(samples, s)
		for range repeat {
			qctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			t := time.Now()
			_, err := e.Search(qctx, q)
			cancel()
			if err != nil {
				errs[string(q.Kind)+": "+q.Text] = err.Error()
				break
			}
			times[q.Kind] = append(times[q.Kind], float64(time.Since(t).Microseconds())/1000)
		}
	}
	out := map[Kind]Latency{}
	for k, ts := range times {
		slices.Sort(ts)
		out[k] = Latency{Runs: len(ts), P50: percentile(ts, 0.50), P95: percentile(ts, 0.95), Max: ts[len(ts)-1]}
	}
	return out, samples
}

// percentile of sorted values, nearest rank.
func percentile(sorted []float64, p float64) float64 {
	i := int(float64(len(sorted))*p+0.5) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

func settings(ctx context.Context, pool *pgxpool.Pool) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"shared_buffers", "work_mem", "maintenance_work_mem", "max_parallel_maintenance_workers", "max_parallel_workers_per_gather", "server_version"} {
		var v string
		if err := pool.QueryRow(ctx, "SELECT current_setting($1)", name).Scan(&v); err == nil {
			out[name] = v
		}
	}
	var v string
	if err := pool.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname = 'pg_search'").Scan(&v); err == nil {
		out["pg_search"] = v
	}
	return out
}
