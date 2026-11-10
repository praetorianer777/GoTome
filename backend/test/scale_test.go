//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/perf"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// scaleVariable, set to anything, runs the test of the main views at 50,000
// books: make scale-test. It takes minutes, so the gate leaves it out.
const scaleVariable = "GOTOME_SCALE"

// scalePlansVariable names a folder for the plans of every view, one JSON
// file each, to read when a view is slow or reads a table whole.
const scalePlansVariable = "GOTOME_SCALE_PLANS"

// scaleSpec is the embedding model the test library is made under: the
// largest one, bge-m3 with 1,024 numbers a vector, as on a library whose
// owner chose it (#455). The smaller default would hide what its vectors
// cost.
var scaleSpec = embed.BGEM3

// scaleBudget is the longest a main view may take at 50,000 books.
const scaleBudget = time.Second

// scannedWhole are the tables no main view may read from end to end: they
// grow with the library.
var scannedWhole = []string{"books", "book_files", "book_chunks"}

// wholeQueriesByDesign are the statements, by their sqlc name, that read
// every book by design within a view that otherwise may not, so that the
// rest of the view is still held to reading only what it shows.
var wholeQueriesByDesign = map[string]string{
	// The genre review counts the waiting suggestions of every visible
	// book, which a pass gives most books.
	"CountGenreSuggestions": "counts every waiting suggestion",
}

// queryByDesign is the reason a plan's statement may read every book, if
// it is one of wholeQueriesByDesign.
func queryByDesign(plan map[string]any) string {
	text, _ := plan["Query Text"].(string)
	name, ok := strings.CutPrefix(text, "-- name: ")
	if !ok {
		return ""
	}
	name, _, _ = strings.Cut(name, " ")
	return wholeQueriesByDesign[name]
}

// planCatcher keeps the plans Postgres reports while a view is asked.
// auto_explain sends every plan to the connection as a message, so what is
// caught is the plan that ran, inside the transaction and with the
// settings it ran under.
type planCatcher struct {
	mu    sync.Mutex
	view  string
	plans map[string][]map[string]any
}

func (c *planCatcher) notice(_ *pgconn.PgConn, n *pgconn.Notice) {
	head, plan, ok := strings.Cut(n.Message, "plan:\n")
	if !ok {
		return
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(plan), &parsed) != nil {
		return
	}
	// "duration: 12.345 ms  "
	parsed["Duration"] = strings.TrimSpace(strings.TrimPrefix(head, "duration:"))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.view != "" {
		c.plans[c.view] = append(c.plans[c.view], parsed)
	}
}

func (c *planCatcher) watch(view string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.view = view
}

// wholeScans names the tables of scannedWhole a plan reads sequentially.
func wholeScans(node any, found map[string]bool) {
	switch n := node.(type) {
	case map[string]any:
		if kind, _ := n["Node Type"].(string); strings.HasSuffix(kind, "Seq Scan") {
			if rel, _ := n["Relation Name"].(string); slices.Contains(scannedWhole, rel) {
				found[rel] = true
			}
		}
		for _, v := range n {
			wholeScans(v, found)
		}
	case []any:
		for _, v := range n {
			wholeScans(v, found)
		}
	}
}

// explainingPool is a pool on the same database whose connections report
// the plan of every statement they run.
func explainingPool(t *testing.T, base *pgxpool.Pool, catcher *planCatcher) *pgxpool.Pool {
	t.Helper()
	cfg := base.Config()
	cfg.ConnConfig.OnNotice = catcher.notice
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `LOAD 'auto_explain';
			SET auto_explain.log_min_duration = 0;
			SET auto_explain.log_format = 'json';
			SET auto_explain.log_nested_statements = on;
			SET client_min_messages = log`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedStatement is one statement of the seed, with its arguments.
type seedStatement struct {
	sql  string
	args []any
}

// scaleSeed makes a library of 50,000 books in 20 libraries, two of which
// are private ones the member is in and two private ones they are not:
// authors, series, publishers and tags, files in three formats,
// identifiers, a chunk of text per book, vectors, the member's reading
// state, sessions and shelves, duplicate pairs, matches and genres to
// review, and the feeds both follow.
// Names are made of syllables, so that their trigrams repeat as real ones
// do. Each book's number n is kept in scale_books.
func scaleSeed(admin, member uuid.UUID) []seedStatement {
	model := scaleSpec.Model
	return []seedStatement{
		{sql: `CREATE FUNCTION scale_word(i bigint) RETURNS text LANGUAGE sql IMMUTABLE AS $$
			SELECT initcap(s[1 + i % 16] || s[1 + (i / 16) % 16] || s[1 + (i / 256) % 16])
			FROM (SELECT ARRAY['ka','lo','mir','san','der','son','tha','vel','ri','on','bel','ast','or','ine','qu','zu'] AS s) syllables
		$$`},
		{sql: `INSERT INTO libraries (name, root_path, mode, writable, visibility, owner_id)
			SELECT format('Library %s', lpad(n::text, 2, '0')), '/scale/library-' || n, 'external', false,
			       CASE WHEN n > 16 THEN 'private' ELSE 'shared' END, $1
			FROM generate_series(1, 20) n`, args: []any{admin}},
		{sql: `CREATE TABLE scale_libraries AS
			SELECT id, substring(name FROM 9)::int AS n FROM libraries WHERE root_path LIKE '/scale/%'`},
		{sql: `INSERT INTO library_members (library_id, user_id) SELECT id, $1 FROM scale_libraries WHERE n IN (17, 18)`, args: []any{member}},

		{sql: `INSERT INTO authors (name, sort_name, name_key)
			SELECT f || ' ' || l, l || ', ' || f, lower(f || ' ' || l)
			FROM generate_series(0, 11999) i,
			     LATERAL (SELECT scale_word(i) AS f, scale_word(i / 4096 * 911 + i * 13 + 5) AS l) n`},
		{sql: `INSERT INTO series (name, name_key) SELECT scale_word(i + 1000) || ' Saga', lower(scale_word(i + 1000) || ' saga') FROM generate_series(0, 2499) i`},
		{sql: `INSERT INTO publishers (name, name_key) SELECT scale_word(i) || ' Press', lower(scale_word(i) || ' press') FROM generate_series(0, 799) i`},
		{sql: `INSERT INTO tags (name, name_key) SELECT lower(scale_word(i + 2000)), lower(scale_word(i + 2000)) FROM generate_series(0, 399) i`},
		{sql: `CREATE TABLE scale_authors AS SELECT id, name, (row_number() OVER (ORDER BY created_at, name_key) - 1)::int AS n FROM authors`},
		{sql: `CREATE TABLE scale_series AS SELECT id, (row_number() OVER (ORDER BY name_key) - 1)::int AS n FROM series`},
		{sql: `CREATE TABLE scale_publishers AS SELECT id, (row_number() OVER (ORDER BY name_key) - 1)::int AS n FROM publishers`},
		{sql: `CREATE TABLE scale_tags AS SELECT id, name, (row_number() OVER (ORDER BY name_key) - 1)::int AS n FROM tags`},

		{sql: `INSERT INTO books (library_id, title, sort_title, title_key, description, language,
			                  published_on, published_precision, publisher_id, series_id, series_index,
			                  page_count, cover_key, created_at, updated_at)
			SELECT l.id, t.title, t.title, lower(t.title),
			       'A book about ' || lower(scale_word(i * 17)) || ' and ' || lower(scale_word(i * 19 + 3)) || '.',
			       (ARRAY['en','en','en','de','de','fr','en','es'])[1 + i % 8],
			       date '1800-01-01' + (i * 37 % 80000), 'day',
			       p.id, s.id, CASE WHEN s.id IS NOT NULL THEN 1 + i % 12 END,
			       80 + i % 700, CASE WHEN i % 5 > 0 THEN encode(sha256(i::text::bytea), 'hex') END,
			       timestamptz '2020-01-01' + i * interval '1 hour', timestamptz '2020-01-01' + i * interval '1 hour'
			FROM generate_series(0, 49999) i
			JOIN scale_libraries l ON l.n = 1 + i % 20
			JOIN scale_publishers p ON p.n = i * 7 % 800
			LEFT JOIN scale_series s ON i % 10 < 3 AND s.n = i * 13 % 2500
			CROSS JOIN LATERAL (SELECT scale_word(i * 3) || ' ' || lower(scale_word(i * 5 + 1)) || ' ' ||
			                           lower(scale_word(i / 4096 + i * 11)) AS title) t`},
		{sql: `CREATE TABLE scale_books AS
			SELECT id, library_id, (extract(epoch FROM created_at - timestamptz '2020-01-01') / 3600)::int AS n
			FROM books WHERE library_id IN (SELECT id FROM scale_libraries)`},
		{sql: `CREATE UNIQUE INDEX ON scale_books (n)`},

		{sql: `INSERT INTO book_contributors (book_id, author_id, role, position)
			SELECT b.id, a.id, 'author', 0 FROM scale_books b JOIN scale_authors a ON a.n = b.n * 17 % 12000
			UNION ALL
			SELECT b.id, a.id, 'author', 1 FROM scale_books b JOIN scale_authors a ON a.n = (b.n * 29 + 5) % 12000
			WHERE b.n % 7 = 0 AND (b.n * 29 + 5) % 12000 <> b.n * 17 % 12000`},
		// Some authors under their name the other way round too, some
		// titles with what a shop added, and some with a series in them,
		// for the clean-up to find.
		{sql: `INSERT INTO authors (name, sort_name, name_key)
			SELECT split_part(name_key, ' ', 2) || ', ' || split_part(name_key, ' ', 1), sort_name,
			       split_part(name_key, ' ', 2) || ' ' || split_part(name_key, ' ', 1)
			FROM authors a JOIN scale_authors s ON s.id = a.id WHERE s.n < 3000
			ON CONFLICT (name_key) DO NOTHING`},
		{sql: `UPDATE book_contributors c SET author_id = r.id
			FROM scale_books b, authors a, authors r
			WHERE c.book_id = b.id AND b.n % 9 = 4 AND c.position = 0 AND a.id = c.author_id
			  AND r.name_key = split_part(a.name_key, ' ', 2) || ' ' || split_part(a.name_key, ' ', 1) AND r.id <> a.id`},
		{sql: `UPDATE books b SET title = b.title || ' (German Edition)' FROM scale_books s WHERE s.id = b.id AND s.n % 5 = 0`},
		{sql: `UPDATE books b SET title = r.name || ' ' || (1 + s.n % 12) || ' - ' || b.title
			FROM scale_books s, scale_series n, series r
			WHERE s.id = b.id AND s.n % 10 = 7 AND n.n = s.n % 2500 AND r.id = n.id`},
		{sql: `UPDATE books b SET author_sort = a.sort_name
			FROM book_contributors c JOIN authors a ON a.id = c.author_id
			WHERE c.book_id = b.id AND c.position = 0`},
		{sql: `INSERT INTO book_tags (book_id, tag_id)
			SELECT b.id, t.id FROM scale_books b
			JOIN scale_tags t ON t.n IN (b.n * 3 % 400, (b.n * 7 + 1) % 400, (b.n * 11 + 2) % 400)`},
		{sql: `INSERT INTO book_identifiers (book_id, type, value)
			SELECT id, 'isbn', '978' || lpad(n::text, 10, '0') FROM scale_books WHERE n % 10 < 7`},

		{sql: `INSERT INTO book_files (book_id, library_id, kind, format, rel_path, size_bytes, modified_at, sha256,
			                       extract_state, has_text, page_count, duration_ms, chunked_at)
			SELECT b.id, b.library_id, f.kind, f.format, 'Scale/' || b.n || '.' || f.format, 300000 + b.n, now(),
			       sha256((b.n || f.format)::bytea), 'done', f.kind = 'ebook',
			       CASE WHEN f.kind = 'ebook' THEN 200 END, CASE WHEN f.kind = 'audio' THEN 36000000 END,
			       CASE WHEN f.format = 'epub' THEN now() END
			FROM scale_books b
			CROSS JOIN (VALUES ('ebook', 'epub', 0), ('ebook', 'pdf', 1), ('audio', 'm4b', 2)) f(kind, format, k)
			WHERE f.k = 0 OR (f.k = 1 AND b.n % 10 < 3) OR (f.k = 2 AND b.n % 20 = 0)`},
		{sql: `UPDATE books b SET primary_text_file_id = f.id FROM book_files f WHERE f.book_id = b.id AND f.format = 'epub'`},
		// A few books whose files a scan no longer found: all of them, or
		// only the PDF.
		{sql: `UPDATE book_files f SET missing_at = now() FROM scale_books b
			WHERE b.id = f.book_id AND (b.n % 1000 = 1 OR (b.n % 500 = 2 AND f.format = 'pdf'))`},
		// Every EPUB measured, a few of them slow on an e-reader.
		{sql: `UPDATE book_files f SET ereader_findings = CASE
				WHEN b.n % 50 = 3 THEN ARRAY['large-fonts'] WHEN b.n % 200 = 4 THEN ARRAY['large-document', 'large-image'] ELSE '{}' END
			FROM scale_books b WHERE b.id = f.book_id AND f.format = 'epub'`},
		// One chunk a book, where a real one has about 75: the plans are
		// the ones a full library gets. Its text goes to the full-text
		// index through the outbox, which the test applies after the seed;
		// the file is not there, so a hit is shown without a snippet.
		{sql: `INSERT INTO book_chunks (book_id, library_id, file_id, position, page_from, page_to, char_offset, lang)
			SELECT b.id, b.library_id, f.id, 0, 1, 3, 0, bk.language
			FROM scale_books b
			JOIN books bk ON bk.id = b.id
			JOIN book_files f ON f.book_id = b.id AND f.format = 'epub'`},
		{sql: `INSERT INTO fulltext_outbox (docs)
			SELECT jsonb_agg(jsonb_build_object('id', c.id, 'book', c.book_id, 'library', c.library_id, 'lang', c.lang, 'text', x.body))
			FROM book_chunks c
			JOIN scale_books b ON b.id = c.book_id
			CROSS JOIN LATERAL (SELECT string_agg(lower(scale_word(b.n * 31 + w * 97)), ' ') AS body
			                    FROM generate_series(1, 120) w) x
			GROUP BY b.n / 500`},
		{sql: `INSERT INTO search_words (word) SELECT DISTINCT lower(scale_word(i)) FROM generate_series(0, 4095) i ON CONFLICT DO NOTHING`},
		{sql: `WITH pool AS (
				SELECT n, array_agg(random() - 0.5)::vector AS v
				FROM generate_series(0, 999) n, generate_series(1, $3::int) d
				GROUP BY n)
			INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
			SELECT b.id, k.kind, $1, $2, sha256('scale'), p.v
			FROM scale_books b
			CROSS JOIN (VALUES ('content', 0), ('metadata', 500)) k(kind, shift)
			JOIN pool p ON p.n = (b.n + k.shift) % 1000`, args: []any{model.Name, similar.Version(model), model.Dim}},

		{sql: `INSERT INTO user_books (user_id, book_id, status, rating, started_on, finished_on)
			SELECT u, id, (ARRAY['reading','completed','completed','abandoned','wishlist','unread'])[1 + n / 6 % 6],
			       CASE WHEN n / 6 % 3 = 0 THEN 1 + n % 5 END,
			       CASE WHEN n / 6 % 6 < 4 THEN date '2025-01-01' + n % 600 END, NULL
			FROM scale_books, (VALUES ($1::uuid, 1), ($2::uuid, 2)) w(u, k)
			WHERE n % 6 = 1 AND k = 2 OR n % 11 = 2 AND k = 1`, args: []any{admin, member}},
		// An imported history: 1,200 of the books the member read are ones
		// the library does not hold, with a metadata vector alone.
		{sql: `UPDATE books SET placeholder = true WHERE id IN (SELECT id FROM scale_books WHERE n % 42 = 7)`},
		{sql: `DELETE FROM book_vectors WHERE kind = 'content' AND book_id IN (SELECT id FROM scale_books WHERE n % 42 = 7)`},
		{sql: `INSERT INTO reading_sessions (user_id, book_id, medium, client_id, started_at, ended_at, from_fraction, to_fraction)
			SELECT $1, b.id, 'ebook', 'scale', now() - (s % 365) * interval '1 day',
			       now() - (s % 365) * interval '1 day' + interval '40 minutes', (s % 10) / 10.0, (s % 10 + 1) / 10.0
			FROM generate_series(0, 2999) s JOIN scale_books b ON b.n = (s * 6 + 1) % 50000`, args: []any{member}},
		{sql: `INSERT INTO reading_finishes (user_id, book_id, medium, finished_at)
			SELECT $1, b.id, 'ebook', now() - (s % 700) * interval '1 day'
			FROM generate_series(0, 399) s JOIN scale_books b ON b.n = (s * 6 + 1) % 50000`, args: []any{member}},

		{sql: `INSERT INTO collections (owner_id, name, visibility) VALUES ($1, 'Favourites', 'shared'), ($2, 'To read', 'private')`, args: []any{admin, member}},
		{sql: `INSERT INTO collection_items (collection_id, book_id, position)
			SELECT c.id, b.id, row_number() OVER (PARTITION BY c.id ORDER BY b.n)
			FROM collections c JOIN scale_books b ON (c.name = 'Favourites' AND b.n % 25 = 0) OR (c.name = 'To read' AND b.n % 160 = 7)`},
		{sql: `INSERT INTO smart_shelves (owner_id, name, filter, visibility)
			VALUES ($1, 'German, finished', '{"all": [{"field": "language", "op": "in", "values": ["de"]}, {"field": "status", "op": "in", "values": ["completed"]}]}', 'shared')`, args: []any{member}},

		{sql: `INSERT INTO duplicate_pairs (book_a, book_b, state, score)
			SELECT least(a.id, b.id), greatest(a.id, b.id), CASE WHEN s % 10 = 0 THEN 'kept_both' ELSE 'open' END, 0.75 + (s % 25) / 100.0
			FROM generate_series(0, 2999) s
			JOIN scale_books a ON a.n = s * 7
			JOIN scale_books b ON b.n = s * 7 + 25000`},
		{sql: `INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, 'title_author', 'scale' FROM duplicate_pairs`},
		{sql: `INSERT INTO metadata_matches (book_id, provider, record_id, score, record, state)
			SELECT b.id, 'openlibrary', 'OL' || b.n, 0.8, jsonb_build_object('title', bk.title), 'pending'
			FROM scale_books b JOIN books bk ON bk.id = b.id WHERE b.n % 25 = 3`},
		// Both follow an author with 200 books to come, half of them books
		// the library has, which the list must tell.
		{sql: `INSERT INTO release_subjects (kind, name, name_key, polled_at)
			SELECT 'author', au.name, au.name_key, now() FROM scale_authors a JOIN authors au ON au.id = a.id WHERE a.n = 4242`},
		{sql: `INSERT INTO trackers (user_id, subject_id) SELECT u, s.id FROM release_subjects s, unnest(ARRAY[$1, $2]::uuid[]) u`, args: []any{admin, member}},
		{sql: `INSERT INTO releases (subject_id, dedupe_key, title, authors, author_keys, release_date, precision, provider, provider_id, backlog)
			SELECT s.id, bk.title_key || CASE WHEN g % 2 = 0 THEN '' ELSE ' sequel' END, bk.title, ARRAY[au.name], ARRAY[au.name_key],
			       current_date + 1 + g, 'day', 'hardcover', g::text, true
			FROM release_subjects s
			CROSS JOIN generate_series(0, 199) g
			JOIN scale_books b ON b.n = g * 250
			JOIN books bk ON bk.id = b.id
			JOIN book_contributors c ON c.book_id = b.id AND c.role = 'author' AND c.position = 0
			JOIN authors au ON au.id = c.author_id
			ON CONFLICT DO NOTHING`},
		// Genres for a third of the books, two each: most suggested and
		// waiting, a few decided, and some from tags, which the review
		// leaves out.
		{sql: `INSERT INTO book_genres (book_id, genre, source, method, score, state)
			SELECT b.id, g.genre, CASE WHEN b.n % 7 = 0 THEN 'tags' ELSE 'automatic' END,
			       CASE WHEN b.n % 7 <> 0 THEN (ARRAY['vector', 'neighbours'])[1 + b.n % 2] END,
			       CASE WHEN b.n % 7 <> 0 THEN 0.3 + (b.n * 13 % 70) / 100.0 END,
			       CASE WHEN b.n % 11 = 0 THEN 'accepted' WHEN b.n % 13 = 0 THEN 'rejected' ELSE 'suggested' END
			FROM scale_books b
			CROSS JOIN LATERAL (SELECT (ARRAY['crime', 'thriller', 'fantasy', 'science-fiction', 'romance', 'historical',
			                                  'history', 'biography', 'humour', 'classics'])[1 + (b.n + k * 3) % 10] AS genre
			                    FROM generate_series(0, 1) k) g
			WHERE b.n % 3 = 0`},
		// Both follow ten feeds of 5,000 items each, a fifth of which name
		// what the feed's filters ask for.
		{sql: `INSERT INTO feed_trackers (user_id, url, title, keywords, checked_at, next_fetch_at)
			SELECT u, 'https://feeds.example/' || u || '/' || f, 'Feed ' || f, ARRAY['saga'], now(), now() + interval '1 hour'
			FROM unnest(ARRAY[$1, $2]::uuid[]) u, generate_series(1, 10) f`, args: []any{admin, member}},
		{sql: `INSERT INTO feed_items (tracker_id, item_key, title, link, authors, published_at, matched, backlog, first_seen_at, last_seen_at)
			SELECT t.id, 'item-' || i, scale_word(i * 7) || ' Saga', 'https://feeds.example/item/' || i, ARRAY[scale_word(i)],
			       CASE WHEN i % 4 > 0 THEN timestamptz '2020-01-01' + i * interval '3 hours' END,
			       i % 5 = 0, i < 50, timestamptz '2020-01-01' + i * interval '3 hours', now()
			FROM feed_trackers t, generate_series(0, 4999) i`},
		// What the clean-up finds besides spellings, titles and series: a
		// value a tool left in place of an author on 600 books, and an
		// author who is the name of a series, on some of its books.
		{sql: `INSERT INTO authors (name, sort_name, name_key) VALUES ('Unknown', 'Unknown', 'unknown'), ('N/A', 'N/A', 'n a')`},
		{sql: `INSERT INTO book_contributors (book_id, author_id, role, position)
			SELECT b.id, a.id, 'author', 5 FROM scale_books b, authors a WHERE a.name_key IN ('unknown', 'n a') AND b.n % 160 = 9 + (a.name_key = 'n a')::int`},
		{sql: `INSERT INTO authors (name, sort_name, name_key)
			SELECT s.name, s.name, s.name_key FROM series s
			WHERE s.id IN (SELECT series_id FROM books WHERE series_id IS NOT NULL GROUP BY series_id ORDER BY count(*) DESC, series_id LIMIT 2)`},
		{sql: `INSERT INTO book_contributors (book_id, author_id, role, position)
			SELECT b.id, a.id, 'author', 6 FROM books b
			JOIN series s ON s.id = b.series_id
			JOIN authors a ON a.name_key = s.name_key`},
		{sql: `ANALYZE`},
	}
}

// scaleViews are perf.Views with what the seed named filled in.
func scaleViews(t *testing.T, a *app, ctx context.Context) []perf.View {
	t.Helper()
	var p perf.Params
	for _, q := range []struct {
		sql string
		to  *string
	}{
		{`SELECT id::text FROM scale_books WHERE n = 25000`, &p.Book},
		{`SELECT id::text FROM scale_libraries WHERE n = 3`, &p.Library},
		{`SELECT id::text FROM collections WHERE name = 'Favourites'`, &p.Collection},
		{`SELECT id::text FROM smart_shelves`, &p.Shelf},
		{`SELECT name FROM scale_authors WHERE n = 4242`, &p.Author},
		{`SELECT s.name_key FROM series s JOIN scale_series n ON n.id = s.id WHERE n.n = 1300`, &p.Series},
		{`SELECT name FROM scale_tags WHERE n = 17`, &p.Tag},
		{`SELECT lower(scale_word(37035))`, &p.Quick},
		{`SELECT lower(scale_word(25000 * 31 + 5 * 97))`, &p.Word},
		{`SELECT lower(scale_word(25000 * 31 + 5 * 97)) || ' ' || lower(scale_word(25000 * 31 + 6 * 97))`, &p.Phrase},
	} {
		if err := a.pool.QueryRow(ctx, q.sql).Scan(q.to); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	p.SimilarBook, p.Language, p.Describe = p.Book, "de", "a detective story set in Venice"
	return perf.Views(p)
}

// TestTheMainViewsAtFiftyThousandBooks asks every main view of the web app
// on a library of 50,000 books, as an administrator who sees every library
// and as an editor who sees 18 of 20: each must answer within scaleBudget,
// and none may read books, their files or their chunks from end to end.
// The log is the table in docs/performance.md.
func TestTheMainViewsAtFiftyThousandBooks(t *testing.T) {
	if os.Getenv(scaleVariable) == "" {
		t.Skip("set " + scaleVariable + "=1 to run it: make scale-test")
	}
	ctx := context.Background()
	base := dbtest.New(t)
	catcher := &planCatcher{plans: map[string][]map[string]any{}}
	a := newAppOn(t, explainingPool(t, base, catcher))
	// The review queue reads through the lookup service; no provider is asked.
	a.matcher()
	admin, adminID := a.signedIn("admin", "admin")
	member, memberID := a.signedIn("member", "editor")

	choice := "bge-m3"
	if err := a.settings.Update(ctx, uuid.MustParse(adminID), map[string]*string{settings.EmbeddingModel: &choice}); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	for _, s := range scaleSeed(uuid.MustParse(adminID), uuid.MustParse(memberID)) {
		if _, err := base.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, s.sql)
		}
	}
	if more, err := a.text.Drain(ctx, time.Now().Add(10*time.Minute)); err != nil || more {
		t.Fatalf("index the seed: more %v, %v", more, err)
	}
	var books int
	base.QueryRow(ctx, `SELECT count(*) FROM books`).Scan(&books)
	t.Logf("seeded %d books in %s", books, time.Since(started).Round(time.Second))

	views := scaleViews(t, a, ctx)
	type result struct {
		best     time.Duration
		scans    []string
		byDesign string
	}
	results := map[string]map[string]result{}
	for _, who := range []struct {
		name   string
		client *http.Client
	}{{"administrator", admin}, {"member", member}} {
		results[who.name] = map[string]result{}
		next := map[string]string{}
		for _, v := range views {
			if v.AdminOnly && who.name != "administrator" {
				continue
			}
			path := strings.Replace(v.Path, "@next", url.QueryEscape(next[v.After]), 1)
			key := who.name + " " + v.Name
			catcher.watch(key)
			// The best of three, so that a busy machine does not decide.
			best := time.Hour
			for range 3 {
				began := time.Now()
				var status int
				var body map[string]any
				if v.Feed {
					var raw []byte
					status, raw = a.get(who.client, path)
					body = map[string]any{"body": string(raw)}
				} else {
					status, body, _ = a.call(who.client, http.MethodGet, path, nil)
				}
				took := time.Since(began)
				if status != http.StatusOK {
					logs := a.logs.String()
					t.Fatalf("%s: GET %s: %d %v\n%s", key, path, status, body, logs[max(0, len(logs)-2000):])
				}
				if c, ok := body["nextCursor"].(string); ok {
					next[v.Name] = c
				}
				best = min(best, took)
			}
			catcher.watch("")
			found := map[string]bool{}
			r := result{best: best}
			catcher.mu.Lock()
			for _, plan := range catcher.plans[key] {
				if why := queryByDesign(plan); why != "" {
					r.byDesign = why
					continue
				}
				wholeScans(plan, found)
			}
			catcher.mu.Unlock()
			for rel := range found {
				r.scans = append(r.scans, rel)
			}
			sort.Strings(r.scans)
			results[who.name][v.Name] = r
			if best > scaleBudget {
				t.Errorf("%s: %s, want at most %s", key, best.Round(time.Millisecond), scaleBudget)
			}
			if _, whole := perf.WholeByDesign[v.Name]; len(r.scans) > 0 && !whole {
				t.Errorf("%s reads all of %s", key, strings.Join(r.scans, ", "))
			}
		}
	}

	if dir := os.Getenv(scalePlansVariable); dir != "" {
		writePlans(t, dir, catcher)
	}

	cleanupTakesOneSuggestion(t, a, admin, catcher)

	var table strings.Builder
	table.WriteString("\n| View | Administrator | Member | Reads every book |\n|---|---:|---:|---|\n")
	for _, v := range views {
		cell := func(who string) string {
			r, ok := results[who][v.Name]
			if !ok {
				return "–"
			}
			return fmt.Sprintf("%d ms", r.best.Milliseconds())
		}
		why := perf.WholeByDesign[v.Name]
		if why == "" {
			why = results["administrator"][v.Name].byDesign
		}
		fmt.Fprintf(&table, "| %s | %s | %s | %s |\n", v.Name, cell("administrator"), cell("member"), why)
	}
	t.Log(table.String())
}

func writePlans(t *testing.T, dir string, catcher *planCatcher) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	catcher.mu.Lock()
	defer catcher.mu.Unlock()
	for view, plans := range catcher.plans {
		out, err := json.MarshalIndent(plans, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(view, " ", "-") + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// cleanupTakesOneSuggestion previews and applies one suggestion of each kind
// and dismisses another, as the clean-up page does when a button is clicked:
// each must take under scaleBudget and read no table of scannedWhole from
// end to end, as it would if it worked out every suggestion of its kind.
func cleanupTakesOneSuggestion(t *testing.T, a *app, who *http.Client, catcher *planCatcher) {
	t.Helper()
	for _, kind := range cleanup.Kinds {
		status, body, _ := a.call(who, http.MethodGet, "/cleanup?kind="+kind+"&limit=3", nil)
		if status != http.StatusOK {
			t.Fatalf("clean-up %s: %d %v", kind, status, body)
		}
		suggestions, _ := body["suggestions"].([]any)
		if len(suggestions) < 2 {
			t.Fatalf("the seed has %d clean-up suggestions of kind %s, want at least 2", len(suggestions), kind)
		}
		subject := func(i int) string { return suggestions[i].(map[string]any)["subject"].(string) }
		// A clash that is to choose is applied with the fix chosen, and
		// previewed with it.
		fix := ""
		if suggestions[0].(map[string]any)["fix"] == "choose" {
			fix = "nameIsSeries"
		}
		pick := map[string]any{"subject": subject(0)}
		if fix != "" {
			pick["fix"] = fix
		}
		for _, act := range []struct {
			name, path string
			req        map[string]any
			want       int
		}{
			{"preview", "/cleanup/preview?kind=" + kind + "&subject=" + url.QueryEscape(subject(0)) + "&limit=5&fix=" + fix, nil, http.StatusOK},
			{"apply", "/cleanup/apply", map[string]any{"kind": kind, "picks": []any{pick}}, http.StatusAccepted},
			{"dismiss", "/cleanup/dismiss", map[string]any{"kind": kind, "subject": subject(1)}, http.StatusNoContent},
		} {
			key := "clean-up " + act.name + " " + kind
			catcher.watch(key)
			began := time.Now()
			method := http.MethodPost
			if act.req == nil {
				method = http.MethodGet
			}
			status, out, _ := a.call(who, method, act.path, act.req)
			took := time.Since(began)
			catcher.watch("")
			if status != act.want {
				t.Fatalf("%s: %d %v", key, status, out)
			}
			found := map[string]bool{}
			catcher.mu.Lock()
			for _, plan := range catcher.plans[key] {
				wholeScans(plan, found)
			}
			catcher.mu.Unlock()
			if took > scaleBudget {
				t.Errorf("%s: %s, want at most %s", key, took.Round(time.Millisecond), scaleBudget)
			}
			for rel := range found {
				t.Errorf("%s reads all of %s", key, rel)
			}
			if took > scaleBudget {
				catcher.mu.Lock()
				for _, plan := range catcher.plans[key] {
					out, _ := json.Marshal(plan)
					t.Logf("%s plan: %.3000s", key, out)
				}
				catcher.mu.Unlock()
			}
			t.Logf("%s: %d ms", key, took.Milliseconds())
		}
	}
}
