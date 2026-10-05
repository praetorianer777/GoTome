package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FTS is Postgres's own full-text search: a tsvector per chunk, stemmed in
// the chunk's language, under a GIN index, ranked by ts_rank_cd. Typos are
// repaired before the search: each word of the query is replaced by the
// indexed words that are like it, found in a vocabulary table through
// pg_trgm. The vocabulary holds the indexed forms, so the expanded query
// highlights with ts_headline like any other.
type FTS struct {
	pool *pgxpool.Pool
}

// config is the text search configuration of a chunk's language.
const config = `CASE lang WHEN 'en' THEN 'english'::regconfig WHEN 'de' THEN 'german'::regconfig ELSE 'simple'::regconfig END`

func (e *FTS) Build(ctx context.Context, src Source) (Steps, error) {
	steps := Steps{}
	if err := exec(ctx, e.pool,
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"DROP TABLE IF EXISTS chunks",
		"DROP TABLE IF EXISTS words",
		`CREATE TABLE chunks (
			id bigint PRIMARY KEY,
			book_id int NOT NULL,
			library_id int NOT NULL,
			lang text NOT NULL,
			body text NOT NULL,
			tsv tsvector GENERATED ALWAYS AS (to_tsvector(`+config+`, body)) STORED
		)`,
	); err != nil {
		return nil, err
	}
	t := time.Now()
	err := copyRows(ctx, e.pool, "chunks", []string{"id", "book_id", "library_id", "lang", "body"}, src, func(r Row) []any {
		return []any{r.ID, r.Book, r.Library, r.Lang, r.Text}
	})
	if err != nil {
		return nil, err
	}
	steps["load"] = since(t)
	t = time.Now()
	if err := exec(ctx, e.pool,
		"CREATE INDEX chunks_tsv ON chunks USING gin (tsv)",
		"CREATE INDEX chunks_library ON chunks (library_id)",
	); err != nil {
		return nil, err
	}
	steps["index"] = since(t)
	t = time.Now()
	if err := buildVocabulary(ctx, e.pool, "tsv"); err != nil {
		return nil, err
	}
	steps["vocabulary"] = since(t)
	t = time.Now()
	if err := exec(ctx, e.pool, "VACUUM ANALYZE chunks"); err != nil {
		return nil, err
	}
	steps["vacuum"] = since(t)
	return steps, nil
}

func (e *FTS) Sizes(ctx context.Context) (map[string]int64, error) {
	out, err := relationSizes(ctx, e.pool, "chunks", "chunks_tsv", "chunks_library", "chunks_pkey", "words")
	if err != nil {
		return nil, err
	}
	// The tsvectors live in the table; their share is what this engine
	// stores beyond the text.
	var tsv int64
	err = e.pool.QueryRow(ctx, "SELECT sum(pg_column_size(tsv)) FROM chunks").Scan(&tsv)
	out["tsvector column"] = tsv
	return out, err
}

// tsquery is the query in all three configurations, any of which may match.
func tsquery(fn string) string {
	return fmt.Sprintf("(%[1]s('english', $1) || %[1]s('german', $1) || %[1]s('simple', $1))", fn)
}

func (e *FTS) sql(ctx context.Context, q Query) (string, []any, error) {
	query := tsquery("websearch_to_tsquery")
	text := q.Text
	switch q.Kind {
	case Phrase:
		text = `"` + q.Text + `"`
	case Fuzzy, Repaired:
		expanded, err := e.expand(ctx, q.Text)
		if err != nil {
			return "", nil, err
		}
		query, text = "to_tsquery('simple', $1)", expanded
	}
	args := []any{text}
	where := "tsv @@ q"
	if q.Kind == Filtered {
		where += " AND library_id = ANY($2)"
		args = append(args, FilterLibraries)
	}
	top := fmt.Sprintf(`SELECT id, book_id, ts_rank_cd(tsv, q) AS rank, lang, body, q
		FROM chunks, (SELECT %s) AS x(q) WHERE %s ORDER BY rank DESC LIMIT %d`, query, where, TopK)
	if q.Kind.snippets() {
		return fmt.Sprintf("SELECT id, book_id, rank, ts_headline(%s, body, q) FROM (%s) t ORDER BY rank DESC", config, top), args, nil
	}
	return fmt.Sprintf("SELECT id, book_id, rank FROM (%s) t ORDER BY rank DESC", top), args, nil
}

// expand turns each word into the indexed words like it, as a tsquery of
// alternatives per word.
func (e *FTS) expand(ctx context.Context, text string) (string, error) {
	var parts []string
	for w := range strings.FieldsSeq(text) {
		rows, err := e.pool.Query(ctx, `
			WITH probe AS (
				SELECT DISTINCT unnest(ARRAY[
					lower($1),
					coalesce((SELECT lexeme FROM unnest(to_tsvector('english', $1)) LIMIT 1), lower($1)),
					coalesce((SELECT lexeme FROM unnest(to_tsvector('german', $1)) LIMIT 1), lower($1))
				]) AS p
			)
			SELECT DISTINCT word FROM (
				SELECT w.word FROM probe, LATERAL (
					SELECT word FROM words WHERE word % probe.p ORDER BY word <-> probe.p LIMIT 5
				) w
			) t`, w)
		if err != nil {
			return "", err
		}
		var alts []string
		for rows.Next() {
			var word string
			if err := rows.Scan(&word); err != nil {
				rows.Close()
				return "", err
			}
			alts = append(alts, "'"+strings.ReplaceAll(word, "'", "''")+"'")
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
		if len(alts) == 0 {
			alts = []string{"'" + strings.ReplaceAll(strings.ToLower(w), "'", "''") + "'"}
		}
		parts = append(parts, "("+strings.Join(alts, " | ")+")")
	}
	return strings.Join(parts, " & "), nil
}

func (e *FTS) Search(ctx context.Context, q Query) ([]Hit, error) {
	sql, args, err := e.sql(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, q.Kind.snippets())
}

func (e *FTS) Plan(ctx context.Context, q Query) (string, error) {
	sql, args, err := e.sql(ctx, q)
	if err != nil {
		return "", err
	}
	return explain(ctx, e.pool, sql, args...)
}

func (e *FTS) Rebuild(ctx context.Context) (time.Duration, error) {
	t := time.Now()
	err := exec(ctx, e.pool, "REINDEX INDEX chunks_tsv")
	return time.Since(t), err
}

func (e *FTS) Close() { e.pool.Close() }
