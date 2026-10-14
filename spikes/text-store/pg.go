package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema is book_chunks as migrations 00021 and 00022 make it, without the
// foreign keys to tables the spike does not have.
var schema = []string{
	"CREATE EXTENSION IF NOT EXISTS pg_search CASCADE",
	"DROP TABLE IF EXISTS book_chunks",
	`CREATE TABLE book_chunks (
		id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		book_id    uuid NOT NULL,
		library_id uuid NOT NULL,
		file_id    uuid NOT NULL,
		position   integer NOT NULL CHECK (position >= 0),
		chapter    text NOT NULL DEFAULT '',
		page_from  integer CHECK (page_from > 0),
		page_to    integer CHECK (page_to >= page_from),
		char_offset integer NOT NULL CHECK (char_offset >= 0),
		lang       text NOT NULL,
		body_en    text,
		body_de    text,
		body_xx    text,
		CHECK (num_nonnulls(body_en, body_de, body_xx) = 1),
		UNIQUE (book_id, position)
	)`,
	"CREATE INDEX book_chunks_file_idx ON book_chunks (file_id)",
}

const bm25 = `CREATE INDEX book_chunks_bm25 ON book_chunks USING bm25 (
	id,
	book_id,
	library_id,
	(body_en::pdb.simple('stemmer=english')),
	(body_de::pdb.simple('stemmer=german')),
	(body_xx::pdb.simple)
) WITH (key_field = 'id')`

var columns = []string{"body_en", "body_de", "body_xx"}

// The app's window of best chunks, and the snippets of one page of results.
const (
	window       = 1000
	snippetHits  = 20
	snippetChars = 200
)

func bookUUID(id int) string    { return fmt.Sprintf("00000000-0000-4000-8000-%012d", id) }
func fileUUID(id int) string    { return fmt.Sprintf("00000000-0000-4000-9000-%012d", id) }
func libraryUUID(id int) string { return fmt.Sprintf("00000000-0000-4000-a000-%012d", id) }

// PGResult is what one load of Postgres measured.
type PGResult struct {
	Compression  string           `json:"compression"`
	Books        int              `json:"books"`
	Chunks       int              `json:"chunks"`
	TextBytes    int64            `json:"textBytes"`
	StoredBytes  int64            `json:"storedBytes"`
	Sizes        map[string]int64 `json:"sizes"`
	LoadSeconds  float64          `json:"loadSeconds"`
	IndexSeconds float64          `json:"indexSeconds"`
	Latency      []Latency        `json:"latencyMs"`
}

// Query is one search of queries.json: all words, or a phrase, in any of
// the three columns, in both libraries or the first.
type Query struct {
	Kind     string `json:"kind"`
	Words    string `json:"words,omitempty"`
	Phrase   string `json:"phrase,omitempty"`
	Library  bool   `json:"library,omitempty"`
	Snippets bool   `json:"snippets,omitempty"`
}

// Latency is one query's time over runs after a warm-up.
type Latency struct {
	Kind  string  `json:"kind"`
	Query string  `json:"query"`
	Hits  int     `json:"hits"`
	P50   float64 `json:"p50"`
	Max   float64 `json:"max"`
}

const runs = 5

func runPG(ctx context.Context, dsn, chunks, compression, queries, out string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := exec(ctx, pool, schema...); err != nil {
		return err
	}
	if compression != "pglz" {
		for _, c := range append([]string{"chapter"}, columns...) {
			if err := exec(ctx, pool, fmt.Sprintf("ALTER TABLE book_chunks ALTER COLUMN %s SET COMPRESSION %s", c, compression)); err != nil {
				return err
			}
		}
	}
	res := PGResult{Compression: compression}

	start := time.Now()
	cols := []string{"book_id", "library_id", "file_id", "position", "chapter", "page_from", "page_to", "char_offset", "lang", "body_en", "body_de", "body_xx"}
	var batch [][]any
	flush := func() error {
		if _, err := pool.CopyFrom(ctx, pgx.Identifier{"book_chunks"}, cols, pgx.CopyFromRows(batch)); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	err = readBooks(chunks, func(b Book) error {
		res.Books++
		for i, c := range b.Chunks {
			res.Chunks++
			var from, to any
			if c.PageFrom > 0 {
				from, to = c.PageFrom, c.PageTo
			}
			body := []any{nil, nil, nil}
			switch c.Lang {
			case "en":
				body[0] = c.Text
			case "de":
				body[1] = c.Text
			default:
				body[2] = c.Text
			}
			batch = append(batch, append([]any{bookUUID(b.ID), libraryUUID(b.Library), fileUUID(b.ID), i, c.Chapter, from, to, c.Offset, c.Lang}, body...))
		}
		if len(batch) >= 5000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	res.LoadSeconds = time.Since(start).Seconds()
	fmt.Printf("%s: %d books, %d chunks loaded in %.0f s\n", compression, res.Books, res.Chunks, res.LoadSeconds)

	start = time.Now()
	if err := exec(ctx, pool, bm25); err != nil {
		return err
	}
	res.IndexSeconds = time.Since(start).Seconds()
	fmt.Printf("%s: BM25 index built in %.0f s\n", compression, res.IndexSeconds)
	if err := exec(ctx, pool, "VACUUM ANALYZE book_chunks", "CHECKPOINT"); err != nil {
		return err
	}

	body := "coalesce(body_en, body_de, body_xx)"
	if err := pool.QueryRow(ctx, "SELECT sum(octet_length("+body+")), sum(pg_column_size("+body+")) FROM book_chunks").
		Scan(&res.TextBytes, &res.StoredBytes); err != nil {
		return err
	}
	res.Sizes = map[string]int64{}
	sizes := map[string]string{
		"heap":       "pg_relation_size('book_chunks')",
		"toast":      "pg_total_relation_size((SELECT reltoastrelid FROM pg_class WHERE relname = 'book_chunks'))",
		"pkey":       "pg_relation_size('book_chunks_pkey')",
		"unique":     "pg_relation_size('book_chunks_book_id_position_key')",
		"fileIndex":  "pg_relation_size('book_chunks_file_idx')",
		"bm25":       "pg_relation_size('book_chunks_bm25')",
		"total":      "pg_total_relation_size('book_chunks')",
		"tableTotal": "pg_table_size('book_chunks')",
	}
	for k, expr := range sizes {
		var n int64
		if err := pool.QueryRow(ctx, "SELECT "+expr).Scan(&n); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		res.Sizes[k] = n
	}
	for k, v := range res.Sizes {
		fmt.Printf("  %-10s %8.1f MB\n", k, float64(v)/1e6)
	}

	qs, err := loadQueries(queries)
	if err != nil {
		return err
	}
	for _, q := range qs {
		l, err := timePG(ctx, pool, q)
		if err != nil {
			return fmt.Errorf("%s %q: %w", q.Kind, q.Words+q.Phrase, err)
		}
		fmt.Printf("  %-8s %-24q %5d hits  p50 %7.1f ms  max %7.1f ms\n", l.Kind, l.Query, l.Hits, l.P50, l.Max)
		res.Latency = append(res.Latency, l)
	}
	return writeJSON(out, res)
}

func loadQueries(path string) ([]Query, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qs []Query
	return qs, json.Unmarshal(data, &qs)
}

// match is the app's condition (internal/search): every word, or the
// phrase, in one of the columns.
func match(q Query) (string, string) {
	op, text := "&&&", q.Words
	if q.Phrase != "" {
		op, text = "###", q.Phrase
	}
	var alts []string
	for _, c := range columns {
		alts = append(alts, c+" "+op+" $1")
	}
	return "(" + strings.Join(alts, " OR ") + ")", text
}

func timePG(ctx context.Context, pool *pgxpool.Pool, q Query) (Latency, error) {
	cond, text := match(q)
	libs := []string{libraryUUID(0), libraryUUID(1)}
	if q.Library {
		libs = libs[:1]
	}
	search := `SELECT id FROM book_chunks WHERE ` + cond + ` AND library_id = ANY($2::uuid[])
		ORDER BY pdb.score(id) DESC LIMIT ` + fmt.Sprint(window)
	var snippets []string
	for _, c := range columns {
		snippets = append(snippets, fmt.Sprintf("NULLIF(pdb.snippet(%s, '\x02', '\x03', %d), '')", c, snippetChars))
	}
	passages := `SELECT id, coalesce(` + strings.Join(snippets, ", ") + `, '') FROM book_chunks
		WHERE ` + cond + ` AND id = ANY($2::bigint[])`
	l := Latency{Kind: q.Kind, Query: text}
	var times []float64
	for i := range runs + 1 {
		start := time.Now()
		rows, err := pool.Query(ctx, search, text, libs)
		if err != nil {
			return l, err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return l, err
		}
		if q.Snippets && len(ids) > 0 {
			top := ids[:min(len(ids), snippetHits)]
			rows, err := pool.Query(ctx, passages, text, top)
			if err != nil {
				return l, err
			}
			n := 0
			for rows.Next() {
				n++
			}
			if err := rows.Err(); err != nil {
				return l, err
			}
		}
		l.Hits = len(ids)
		if i > 0 {
			times = append(times, float64(time.Since(start).Microseconds())/1000)
		}
	}
	slices.Sort(times)
	l.P50, l.Max = times[len(times)/2], times[len(times)-1]
	return l, nil
}

func exec(ctx context.Context, pool *pgxpool.Pool, stmts ...string) error {
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", strings.SplitN(strings.TrimSpace(s), "\n", 2)[0], err)
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
