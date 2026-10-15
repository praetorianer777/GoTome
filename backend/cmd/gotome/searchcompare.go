package main

import (
	"context"
	"flag"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/search"
)

// searchCompare asks pg_search and the index in the app the same queries,
// as someone who sees every library, and prints how far their first pages
// agree and how long each took (#265). It opens the index for searching
// only, beside the running server, which holds its writer.
func searchCompare(args []string) error {
	flags := flag.NewFlagSet("search-compare", flag.ContinueOnError)
	limit := flags.Int("n", 20, "how many books of each answer to compare")
	if err := flags.Parse(args); err != nil {
		return err
	}
	queries := search.CompareQueries
	if flags.NArg() > 0 {
		queries = flags.Args()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if _, err := fulltext.Load(cfg.Fulltext); err != nil {
		return err
	}
	index, err := fulltext.Open(fulltext.Dir(cfg.DataDir), 0)
	if err != nil {
		return err
	}
	defer index.Close()
	pool, err := openDatabase(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	books := catalog.NewService(pool)
	results := search.Compare(ctx, search.NewPGSearch(pool, books), search.NewTantivySearch(pool, books, index),
		library.Scope{SeesAll: true}, queries, *limit)
	fmt.Printf("%-24s %9s %9s %7s %10s %10s\n", "query", "pg_search", "tantivy", "shared", "pg ms", "tantivy ms")
	for _, c := range results {
		if c.ErrorA != nil || c.ErrorB != nil {
			fmt.Printf("%-24s pg_search: %v; tantivy: %v\n", c.Query, c.ErrorA, c.ErrorB)
			continue
		}
		fmt.Printf("%-24s %9d %9d %7d %10.1f %10.1f\n", c.Query, len(c.A), len(c.B), c.Shared,
			float64(c.TimeA.Microseconds())/1000, float64(c.TimeB.Microseconds())/1000)
	}
	return nil
}
