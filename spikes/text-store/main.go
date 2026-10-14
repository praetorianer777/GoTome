// Command text-store measures what a book's chunks take in Postgres, with
// pglz and with lz4, for docs/decisions/text-store.md (#244), and in
// Manticore Search over its HTTP API (#268). zstd.py measures a store of
// GOtome's own and tantivy/ Tantivy directly. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

const usage = `Usage: text-store <command> [flags]

Commands:
  extract  Read a sample of a library's EPUBs into chunks
  pg       Load the chunks into Postgres, index them and measure
  manticore-load    Load the chunks into Manticore and index them
  manticore-search  Measure Manticore's index, searches and changes
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "text-store:", err)
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
		dir := fs.String("dir", "/library", "the library to sample")
		every := fs.Int("every", 24, "take every nth EPUB in name order")
		out := fs.String("out", "/work/.cache/text-store/chunks.jsonl.gz", "where the chunks go")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return extract(*dir, *every, *out)
	case "pg":
		fs := flag.NewFlagSet("pg", flag.ContinueOnError)
		dsn := fs.String("dsn", "", "the database")
		chunks := fs.String("chunks", "/work/.cache/text-store/chunks.jsonl.gz", "what extract wrote")
		compression := fs.String("compression", "pglz", "TOAST compression: pglz or lz4")
		queries := fs.String("queries", "queries.json", "the searches to time")
		out := fs.String("out", "", "where the result goes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runPG(ctx, *dsn, *chunks, *compression, *queries, *out)
	case "manticore-load":
		fs := flag.NewFlagSet("manticore-load", flag.ContinueOnError)
		base := fs.String("url", "http://manticore:9308", "Manticore's HTTP API")
		chunks := fs.String("chunks", "/work/.cache/text-store/chunks.jsonl.gz", "what extract wrote")
		mem := fs.String("rt-mem-limit", "128M", "each table's RAM chunk")
		writers := fs.Int("writers", 4, "bulk requests at once")
		phase := fs.String("phase", "", "where to say what is being done, for bench.sh's sampler")
		out := fs.String("out", "", "where the result goes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runManticoreLoad(ctx, *base, *chunks, *mem, *writers, *phase, *out)
	case "manticore-search":
		fs := flag.NewFlagSet("manticore-search", flag.ContinueOnError)
		base := fs.String("url", "http://manticore:9308", "Manticore's HTTP API")
		chunks := fs.String("chunks", "/work/.cache/text-store/chunks.jsonl.gz", "what extract wrote")
		queries := fs.String("queries", "queries.json", "the searches to time")
		load := fs.String("load", "", "what manticore-load wrote")
		files := fs.String("files", "", "the data directory's files, as \"size name\" lines")
		samples := fs.String("samples", "", "bench.sh's memory samples")
		phase := fs.String("phase", "", "where to say what is being done, for bench.sh's sampler")
		runs := fs.Int("runs", 20, "timed runs of each query")
		windows := fs.String("windows", "1000,10", "hits to take, comma separated")
		restart := fs.Float64("restart-seconds", 0, "how long the restart before took, as bench.sh measured it")
		out := fs.String("out", "", "where the result goes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var ws []int
		for w := range strings.SplitSeq(*windows, ",") {
			n, err := strconv.Atoi(w)
			if err != nil {
				return err
			}
			ws = append(ws, n)
		}
		return runManticoreSearch(ctx, *restart, *base, *chunks, *queries, *load, *files, *samples, *phase, *runs, ws, *out)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}
