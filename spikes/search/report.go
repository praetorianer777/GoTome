package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var engineNames = map[string]string{
	"pgsearch": "pg_search",
	"fts":      "Postgres FTS + pg_trgm",
	"bleve":    "Bleve",
}

// report prints the results in dir as the tables of the decision record.
func report(dir string, w io.Writer) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	var results []Result
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var r Result
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		results = append(results, r)
	}
	order := map[string]int{"pgsearch": 0, "fts": 1, "bleve": 2}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Books != results[j].Books {
			return results[i].Books < results[j].Books
		}
		return order[results[i].Engine] < order[results[j].Engine]
	})

	fmt.Fprintln(w, "| Engine | Books | Chunks | Text | Build | Index on disk | Total on disk | Peak anon, build | Peak anon, queries | Rebuild |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range results {
		index, total := diskUse(r)
		fmt.Fprintf(w, "| %s | %d | %d | %.0f MB | %s | %s | %s | %s | %s | %s |\n",
			engineNames[r.Engine], r.Books, r.Chunks, r.TextMB, duration(r.Build["total"]),
			mb(index), mb(total), mb(anon(r, "build")), mb(anon(r, "queries")), rebuild(r))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Query latency in milliseconds, p50 / p95 (max), top 10 chunks:")
	fmt.Fprintln(w)
	fmt.Fprint(w, "| Engine | Books |")
	for _, k := range Kinds {
		fmt.Fprintf(w, " %s |", k)
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, "|---|---:|")
	for range Kinds {
		fmt.Fprint(w, "---:|")
	}
	fmt.Fprintln(w)
	for _, r := range results {
		fmt.Fprintf(w, "| %s | %d |", engineNames[r.Engine], r.Books)
		for _, k := range Kinds {
			l, ok := r.Latency[k]
			if !ok {
				fmt.Fprint(w, " – |")
				continue
			}
			fmt.Fprintf(w, " %s / %s (%s) |", ms(l.P50), ms(l.P95), ms(l.Max))
		}
		fmt.Fprintln(w)
	}
	for _, r := range results {
		if len(r.Errors) == 0 {
			continue
		}
		fmt.Fprintf(w, "\nErrors of %s at %d books:\n\n", engineNames[r.Engine], r.Books)
		for q, e := range r.Errors {
			fmt.Fprintf(w, "- %s: %s\n", q, e)
		}
	}
	return nil
}

// diskUse is the index alone and everything the engine stores, text
// included.
func diskUse(r Result) (index, total int64) {
	switch r.Engine {
	case "bleve":
		for _, v := range r.Sizes {
			index += v
		}
		return index, index
	case "pgsearch":
		index = r.Sizes["chunks_bm25"]
	case "fts":
		index = r.Sizes["chunks_tsv"] + r.Sizes["words"] + r.Sizes["tsvector column"]
	}
	total = r.Sizes["table"]
	for k, v := range r.Sizes {
		if strings.HasPrefix(k, "chunks_") || k == "words" {
			total += v
		}
	}
	return index, total
}

func anon(r Result, phase string) int64 {
	m, ok := r.Memory[phase].(map[string]any)
	if !ok {
		return 0
	}
	v, _ := m["anon"].(float64)
	return int64(v)
}

func rebuild(r Result) string {
	if r.Rebuild == 0 {
		return "full build"
	}
	return duration(r.Rebuild)
}

func mb(n int64) string {
	if n >= 10<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
}

func ms(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func duration(s float64) string {
	if s >= 120 {
		return fmt.Sprintf("%.0f min", s/60)
	}
	return fmt.Sprintf("%.0f s", s)
}
