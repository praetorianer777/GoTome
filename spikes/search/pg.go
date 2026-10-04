package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// copyBatch is how many rows one COPY sends; smaller batches let a full
// load show its progress.
const copyBatch = 20000

// copyRows loads the rows into table through COPY, each row's text into the
// column of its language.
func copyRows(ctx context.Context, pool *pgxpool.Pool, table string, cols []string, src Source, values func(Row) []any) error {
	batch := make([][]any, 0, copyBatch)
	var total int
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := pool.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(batch)); err != nil {
			return err
		}
		total += len(batch)
		if total%(copyBatch*25) == 0 {
			fmt.Printf("  %d rows loaded\n", total)
		}
		batch = batch[:0]
		return nil
	}
	err := src(func(r Row) error {
		batch = append(batch, values(r))
		if len(batch) == copyBatch {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return flush()
}

func exec(ctx context.Context, pool *pgxpool.Pool, stmts ...string) error {
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", strings.SplitN(strings.TrimSpace(s), "\n", 2)[0], err)
		}
	}
	return nil
}

// relationSizes reports the table with its TOAST, and each named relation.
func relationSizes(ctx context.Context, pool *pgxpool.Pool, table string, rels ...string) (map[string]int64, error) {
	out := map[string]int64{}
	var n int64
	if err := pool.QueryRow(ctx, "SELECT pg_table_size($1::regclass)", table).Scan(&n); err != nil {
		return nil, err
	}
	out["table"] = n
	for _, r := range rels {
		if err := pool.QueryRow(ctx, "SELECT pg_total_relation_size($1::regclass)", r).Scan(&n); err != nil {
			return nil, err
		}
		out[r] = n
	}
	return out, nil
}

func explain(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (string, error) {
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) "+sql, args...)
	if err != nil {
		return "", err
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return strings.Join(lines, "\n"), err
}

func scanHits(rows pgx.Rows, snippets bool) ([]Hit, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Hit, error) {
		var h Hit
		var score float32
		var err error
		if snippets {
			err = r.Scan(&h.ID, &h.Book, &score, &h.Snippet)
		} else {
			err = r.Scan(&h.ID, &h.Book, &score)
		}
		h.Score = float64(score)
		return h, err
	})
}

// vocabularyStride is how many book IDs one statement of buildVocabulary
// reads: one copy of the corpus. ts_stat keeps every distinct word of what
// it reads in memory, and over all 50,000 books at once the backend did not
// fit in 2 GB.
const vocabularyStride = copyStride

// buildVocabulary fills the table words with every word the tsvector
// expression yields and the number of chunks it is in, a range of books at a
// time, as the app would keep it current chunk by chunk, and indexes it for
// pg_trgm.
func buildVocabulary(ctx context.Context, pool *pgxpool.Pool, tsvector string) error {
	if err := exec(ctx, pool,
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"DROP TABLE IF EXISTS words",
		"CREATE TABLE words (word text PRIMARY KEY, ndoc int NOT NULL)",
	); err != nil {
		return err
	}
	var last int
	if err := pool.QueryRow(ctx, "SELECT coalesce(max(book_id), 0) FROM chunks").Scan(&last); err != nil {
		return err
	}
	for lo := 0; lo <= last; lo += vocabularyStride {
		stat := fmt.Sprintf("SELECT %s FROM chunks WHERE book_id >= %d AND book_id < %d", tsvector, lo, lo+vocabularyStride)
		if _, err := pool.Exec(ctx, `INSERT INTO words SELECT word, ndoc FROM ts_stat($1)
			ON CONFLICT (word) DO UPDATE SET ndoc = words.ndoc + excluded.ndoc`, stat); err != nil {
			return fmt.Errorf("vocabulary of books from %d: %w", lo, err)
		}
	}
	return exec(ctx, pool,
		"CREATE INDEX words_trgm ON words USING gin (word gin_trgm_ops)",
		"VACUUM ANALYZE words",
	)
}
