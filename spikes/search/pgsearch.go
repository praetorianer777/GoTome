package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGSearch is ParadeDB's pg_search: a BM25 index (Tantivy) inside Postgres.
// Each language's text is a column of its own with its own stemmer, so a
// chunk is stemmed once, in its own language, and a query asks all three.
// Typos are repaired two ways: by pg_search's own fuzzy match (Fuzzy), and
// by looking the words up in a vocabulary first, as FTS does (Repaired).
type PGSearch struct {
	pool *pgxpool.Pool
}

func (e *PGSearch) Build(ctx context.Context, src Source) (Steps, error) {
	steps := Steps{}
	if err := exec(ctx, e.pool,
		"DROP TABLE IF EXISTS chunks",
		`CREATE TABLE chunks (
			id bigint PRIMARY KEY,
			book_id int NOT NULL,
			library_id int NOT NULL,
			lang text NOT NULL,
			body_en text,
			body_de text,
			body_xx text
		)`,
	); err != nil {
		return nil, err
	}
	t := time.Now()
	err := copyRows(ctx, e.pool, "chunks", []string{"id", "book_id", "library_id", "lang", "body_en", "body_de", "body_xx"}, src, func(r Row) []any {
		v := []any{r.ID, r.Book, r.Library, r.Lang, nil, nil, nil}
		switch textColumn(r.Lang) {
		case "body_en":
			v[4] = r.Text
		case "body_de":
			v[5] = r.Text
		default:
			v[6] = r.Text
		}
		return v
	})
	if err != nil {
		return nil, err
	}
	steps["load"] = since(t)
	t = time.Now()
	if err := exec(ctx, e.pool, createBM25); err != nil {
		return nil, err
	}
	steps["index"] = since(t)
	t = time.Now()
	if err := exec(ctx, e.pool, "VACUUM ANALYZE chunks"); err != nil {
		return nil, err
	}
	steps["vacuum"] = since(t)
	t = time.Now()
	if err := buildVocabulary(ctx, e.pool, "to_tsvector('simple', coalesce(body_en, body_de, body_xx))"); err != nil {
		return nil, err
	}
	steps["vocabulary"] = since(t)
	return steps, nil
}

const createBM25 = `CREATE INDEX chunks_bm25 ON chunks USING bm25 (
	id,
	library_id,
	(body_en::pdb.simple('stemmer=english')),
	(body_de::pdb.simple('stemmer=german')),
	(body_xx::pdb.simple)
) WITH (key_field = 'id')`

func (e *PGSearch) Sizes(ctx context.Context) (map[string]int64, error) {
	return relationSizes(ctx, e.pool, "chunks", "chunks_bm25", "chunks_pkey", "words")
}

// match is the condition of a query of the kind on all three columns.
func (e *PGSearch) match(k Kind) string {
	op := "body_%[1]s &&& $1"
	switch k {
	case Phrase:
		op = "body_%[1]s ### $1"
	case Fuzzy:
		op = "body_%[1]s &&& $1::pdb.fuzzy(1, t)"
	}
	var cond string
	for i, l := range []string{"en", "de", "xx"} {
		if i > 0 {
			cond += " OR "
		}
		cond += fmt.Sprintf(op, l)
	}
	return "(" + cond + ")"
}

func (e *PGSearch) sql(ctx context.Context, q Query) (string, []any, error) {
	where := e.match(q.Kind)
	args := []any{q.Text}
	if q.Kind == Repaired {
		groups, err := e.repair(ctx, q.Text)
		if err != nil {
			return "", nil, err
		}
		where, args = repairedMatch(groups), groups
	}
	if q.Kind == Filtered {
		where += " AND library_id = ANY($2)"
		args = append(args, FilterLibraries)
	}
	cols := "id, book_id, pdb.score(id)"
	if q.Kind.snippets() {
		cols += `, coalesce(pdb.snippet(body_en), pdb.snippet(body_de), pdb.snippet(body_xx), '')`
	}
	return fmt.Sprintf("SELECT %s FROM chunks WHERE %s ORDER BY pdb.score(id) DESC LIMIT %d", cols, where, TopK), args, nil
}

// repair replaces each word of the text by the words of the corpus most like
// it, itself included if it occurs, found through pg_trgm. Each group is the
// alternatives for one word, separated by spaces.
func (e *PGSearch) repair(ctx context.Context, text string) ([]any, error) {
	var groups []any
	for w := range strings.FieldsSeq(text) {
		rows, err := e.pool.Query(ctx,
			"SELECT word FROM words WHERE word % lower($1) ORDER BY word <-> lower($1) LIMIT 5", w)
		if err != nil {
			return nil, err
		}
		alts, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		if len(alts) == 0 {
			alts = []string{w}
		}
		groups = append(groups, strings.Join(alts, " "))
	}
	return groups, nil
}

// repairedMatch requires one alternative of every group, in any of the
// three columns.
func repairedMatch(groups []any) string {
	var cols []string
	for _, l := range []string{"en", "de", "xx"} {
		var all []string
		for i := range groups {
			all = append(all, fmt.Sprintf("body_%s ||| $%d", l, i+1))
		}
		cols = append(cols, "("+strings.Join(all, " AND ")+")")
	}
	return "(" + strings.Join(cols, " OR ") + ")"
}

func (e *PGSearch) Search(ctx context.Context, q Query) ([]Hit, error) {
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

func (e *PGSearch) Plan(ctx context.Context, q Query) (string, error) {
	sql, args, err := e.sql(ctx, q)
	if err != nil {
		return "", err
	}
	return explain(ctx, e.pool, sql, args...)
}

func (e *PGSearch) Rebuild(ctx context.Context) (time.Duration, error) {
	t := time.Now()
	err := exec(ctx, e.pool, "REINDEX INDEX chunks_bm25")
	return time.Since(t), err
}

func (e *PGSearch) Close() { e.pool.Close() }
