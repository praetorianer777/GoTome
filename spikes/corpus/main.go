// Command corpus builds the benchmark corpus for the search and embedding
// spikes: public-domain EPUBs from Project Gutenberg, and a replicated set at
// the scale the product is meant to handle.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const usage = `Usage: corpus <command> [flags]

Commands:
  fetch      Download the base corpus from Project Gutenberg
  replicate  Multiply the base corpus into a larger synthetic one
  stats      Print the composition of a corpus directory

Run "corpus <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	switch args[0] {
	case "fetch":
		return runFetch(ctx, args[1:])
	case "replicate":
		return runReplicate(ctx, args[1:])
	case "stats":
		return runStats(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	dir := fs.String("dir", ".cache/corpus/base", "directory the EPUBs are written to")
	langs := fs.String("langs", DefaultLanguages, "books per language, as code:count pairs")
	delay := fs.Duration("delay", DefaultDelay, "pause between requests; Project Gutenberg asks for two seconds")
	harvest := fs.String("harvest", HarvestURL, "harvest endpoint listing the files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	quotas, err := ParseLanguages(*langs)
	if err != nil {
		return err
	}
	if *delay < DefaultDelay && *harvest == HarvestURL {
		return fmt.Errorf("-delay %s is below the %s Project Gutenberg asks of robots", *delay, DefaultDelay)
	}
	f := &Fetcher{
		Dir:     *dir,
		Harvest: *harvest,
		Delay:   *delay,
		Client:  newHTTPClient(),
		Log:     os.Stderr,
	}
	if err := f.Fetch(ctx, quotas); err != nil {
		return err
	}
	return printStats(os.Stdout, *dir)
}

func runReplicate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("replicate", flag.ContinueOnError)
	from := fs.String("from", ".cache/corpus/base", "base corpus directory")
	dir := fs.String("dir", ".cache/corpus/50k", "directory the copies are written to")
	count := fs.Int("count", 50000, "number of books to produce")
	if err := fs.Parse(args); err != nil {
		return err
	}
	started := time.Now()
	n, err := Replicate(ctx, *from, *dir, *count, os.Stderr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d books in %s\n", n, time.Since(started).Round(time.Second))
	return printStats(os.Stdout, *dir)
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	dir := fs.String("dir", ".cache/corpus/base", "corpus directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return printStats(os.Stdout, *dir)
}
