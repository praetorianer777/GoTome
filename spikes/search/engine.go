package main

import (
	"context"
	"errors"
	"time"
)

// errUnsupported is what an engine answers to a kind of query it has no
// use for; that kind is left out of its results.
var errUnsupported = errors.New("not supported by this engine")

// Row is one chunk as it is indexed.
type Row struct {
	ID      int64
	Book    int
	Library int32
	Lang    string
	Text    string
}

// Source hands the engine every row of the corpus at the chosen scale.
type Source func(fn func(Row) error) error

// Engine is one candidate.
type Engine interface {
	// Build loads the rows and builds the index, reporting how long each
	// step took.
	Build(ctx context.Context, src Source) (Steps, error)
	// Sizes reports what the data and the index take on disk, in bytes.
	Sizes(ctx context.Context) (map[string]int64, error)
	Search(ctx context.Context, q Query) ([]Hit, error)
	// Plan describes how the engine runs a query, for the report.
	Plan(ctx context.Context, q Query) (string, error)
	// Rebuild builds the index again from the stored data, which an
	// upgrade of the engine may require.
	Rebuild(ctx context.Context) (time.Duration, error)
	Close()
}

// Steps are the durations of a build, by name, in seconds.
type Steps map[string]float64

func since(t time.Time) float64 { return time.Since(t).Seconds() }

// textColumn is the column a chunk's text goes into: stemmed for English
// and German, as written for every other language.
func textColumn(lang string) string {
	switch lang {
	case "en":
		return "body_en"
	case "de":
		return "body_de"
	default:
		return "body_xx"
	}
}
