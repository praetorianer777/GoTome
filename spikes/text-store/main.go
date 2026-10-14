// Command text-store measures what a book's chunks take in Postgres, with
// pglz and with lz4, for docs/decisions/text-store.md (#244). zstd.py
// measures a store of GOtome's own and tantivy/ Tantivy directly. See
// README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `Usage: text-store <command> [flags]

Commands:
  extract  Read a sample of a library's EPUBs into chunks
  pg       Load the chunks into Postgres, index them and measure
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
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}
