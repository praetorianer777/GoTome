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

	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// scaleVariable, set to anything, runs the test of the main views at 50,000
// books: make scale-test. It takes minutes, so the gate leaves it out.
const scaleVariable = "GOTOME_SCALE"

// scalePlansVariable names a folder for the plans of every view, one JSON
// file each, to read when a view is slow or reads a table whole.
const scalePlansVariable = "GOTOME_SCALE_PLANS"

// scaleBudget is the longest a main view may take at 50,000 books.
const scaleBudget = time.Second

// scannedWhole are the tables no main view may read from end to end: they
// grow with the library.
var scannedWhole = []string{"books", "book_files", "book_chunks"}

// wholeByDesign are the views that read every book, or every file, the
// viewer sees, because what they answer is about all of them; they are held
// to scaleBudget alone.
var wholeByDesign = map[string]string{
	// It counts each value over every book the filter leaves.
	"facets": "counts every book",
	// The exact cosine scan of #66 compares the book with every other one;
	// at 50,000 books an approximate index is not worth what it costs.
	"similar books": "compares with every book",
	// Each library's files are counted and their sizes added up.
	"libraries": "counts every file",
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
// state, sessions and shelves, duplicate pairs and matches to review.
// Names are made of syllables, so that their trigrams repeat as real ones
// do. Each book's number n is kept in scale_books.
func scaleSeed(admin, member uuid.UUID) []seedStatement {
	model := embed.E5Small.Model
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
		// One chunk a book, where a real one has about 75: the plans are
		// the ones a full library gets, the timings of the full-text search
		// are those of docs/decisions/search-engine.md.
		{sql: `INSERT INTO book_chunks (book_id, library_id, file_id, position, page_from, page_to, char_offset, lang,
			                        body_en, body_de, body_xx)
			SELECT b.id, b.library_id, f.id, 0, 1, 3, 0, bk.language,
			       CASE WHEN bk.language = 'en' THEN x.body END,
			       CASE WHEN bk.language = 'de' THEN x.body END,
			       CASE WHEN bk.language NOT IN ('en', 'de') THEN x.body END
			FROM scale_books b
			JOIN books bk ON bk.id = b.id
			JOIN book_files f ON f.book_id = b.id AND f.format = 'epub'
			CROSS JOIN LATERAL (SELECT string_agg(lower(scale_word(b.n * 31 + w * 97)), ' ') AS body
			                    FROM generate_series(1, 120) w) x`},
		{sql: `INSERT INTO search_words (word) SELECT DISTINCT lower(scale_word(i)) FROM generate_series(0, 4095) i ON CONFLICT DO NOTHING`},
		{sql: `WITH pool AS (
				SELECT n, array_agg(random() - 0.5)::vector AS v
				FROM generate_series(0, 999) n, generate_series(1, 384) d
				GROUP BY n)
			INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
			SELECT b.id, k.kind, $1, $2, sha256('scale'), p.v
			FROM scale_books b
			CROSS JOIN (VALUES ('content', 0), ('metadata', 500)) k(kind, shift)
			JOIN pool p ON p.n = (b.n + k.shift) % 1000`, args: []any{model.Name, similar.Version(model)}},

		{sql: `INSERT INTO user_books (user_id, book_id, status, rating, started_on, finished_on)
			SELECT u, id, (ARRAY['reading','completed','completed','abandoned','wishlist','unread'])[1 + n / 6 % 6],
			       CASE WHEN n / 6 % 3 = 0 THEN 1 + n % 5 END,
			       CASE WHEN n / 6 % 6 < 4 THEN date '2025-01-01' + n % 600 END, NULL
			FROM scale_books, (VALUES ($1::uuid, 1), ($2::uuid, 2)) w(u, k)
			WHERE n % 6 = 1 AND k = 2 OR n % 11 = 2 AND k = 1`, args: []any{admin, member}},
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
		{sql: `ANALYZE`},
	}
}

// scaleView is one main view, asked as the web app asks it.
type scaleView struct {
	name string
	path string
	// adminOnly is a view the member may not use.
	adminOnly bool
}

// scaleViews are the views of the web app's pages, with what the seed
// named filled in.
func scaleViews(t *testing.T, a *app, ctx context.Context) []scaleView {
	t.Helper()
	var book, library, collection, shelf string
	var author, tag, quick, word string
	for _, q := range []struct {
		sql string
		to  *string
	}{
		{`SELECT id::text FROM scale_books WHERE n = 25000`, &book},
		{`SELECT id::text FROM scale_libraries WHERE n = 3`, &library},
		{`SELECT id::text FROM collections WHERE name = 'Favourites'`, &collection},
		{`SELECT id::text FROM smart_shelves`, &shelf},
		{`SELECT name FROM scale_authors WHERE n = 4242`, &author},
		{`SELECT name FROM scale_tags WHERE n = 17`, &tag},
		{`SELECT lower(scale_word(37035))`, &quick},
		{`SELECT lower(scale_word(25000 * 31 + 5 * 97))`, &word},
	} {
		if err := a.pool.QueryRow(ctx, q.sql).Scan(q.to); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	filter := func(tree string) string { return url.Values{"filter": {tree}}.Encode() }
	byAuthor := fmt.Sprintf(`{"field": "author", "op": "in", "values": [%q]}`, author)
	tagAndLanguage := fmt.Sprintf(`{"all": [{"field": "tag", "op": "in", "values": [%q]}, {"field": "language", "op": "in", "values": ["de"]}]}`, tag)
	return []scaleView{
		{name: "library by title", path: "/books?limit=50"},
		{name: "library by author", path: "/books?sort=author&limit=50"},
		{name: "library newest first", path: "/books?sort=added&order=desc&limit=50"},
		{name: "library page 2", path: "/books?limit=50&cursor=@next"},
		{name: "one library", path: "/books?limit=50&library=" + library},
		{name: "filter by author", path: "/books?limit=50&" + filter(byAuthor)},
		{name: "filter by tag and language", path: "/books?limit=50&" + filter(tagAndLanguage)},
		{name: "filter by status", path: "/books?limit=50&" + filter(`{"field": "status", "op": "in", "values": ["reading"]}`)},
		{name: "filter by year", path: "/books?limit=50&" + filter(`{"field": "published", "op": "between", "values": ["1900", "1950"]}`)},
		{name: "facets", path: "/books/facets"},
		{name: "facets of a filter", path: "/books/facets?" + filter(tagAndLanguage)},
		{name: "count of a filter", path: "/books/count?" + filter(tagAndLanguage)},
		{name: "quick search", path: "/books/search?q=" + quick},
		{name: "author names", path: "/books/names?kind=author&q=" + quick[:3]},
		{name: "full-text search", path: "/search?q=" + word},
		{name: "book", path: "/books/" + book},
		{name: "similar books", path: "/books/" + book + "/similar"},
		{name: "duplicates", path: "/duplicates"},
		{name: "review", path: "/matches"},
		{name: "collection", path: "/collections/" + collection},
		{name: "smart shelf", path: "/smart-shelves/" + shelf + "/books"},
		{name: "collections", path: "/collections"},
		{name: "reading statistics", path: "/me/stats?days=30"},
		{name: "new books", path: "/releases"},
		{name: "trash", path: "/trash"},
		{name: "jobs", path: "/jobs"},
		{name: "libraries", path: "/libraries"},
		{name: "users", path: "/users", adminOnly: true},
	}
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

	started := time.Now()
	for _, s := range scaleSeed(uuid.MustParse(adminID), uuid.MustParse(memberID)) {
		if _, err := base.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, s.sql)
		}
	}
	var books int
	base.QueryRow(ctx, `SELECT count(*) FROM books`).Scan(&books)
	t.Logf("seeded %d books in %s", books, time.Since(started).Round(time.Second))

	views := scaleViews(t, a, ctx)
	type result struct {
		best  time.Duration
		scans []string
	}
	results := map[string]map[string]result{}
	for _, who := range []struct {
		name   string
		client *http.Client
	}{{"administrator", admin}, {"member", member}} {
		results[who.name] = map[string]result{}
		next := ""
		for _, v := range views {
			if v.adminOnly && who.name != "administrator" {
				continue
			}
			path := strings.Replace(v.path, "@next", url.QueryEscape(next), 1)
			key := who.name + " " + v.name
			catcher.watch(key)
			// The best of three, so that a busy machine does not decide.
			best := time.Hour
			for range 3 {
				began := time.Now()
				status, body, _ := a.call(who.client, http.MethodGet, path, nil)
				took := time.Since(began)
				if status != http.StatusOK {
					logs := a.logs.String()
					t.Fatalf("%s: GET %s: %d %v\n%s", key, path, status, body, logs[max(0, len(logs)-2000):])
				}
				if c, ok := body["nextCursor"].(string); ok && v.name == "library by title" {
					next = c
				}
				best = min(best, took)
			}
			catcher.watch("")
			found := map[string]bool{}
			catcher.mu.Lock()
			for _, plan := range catcher.plans[key] {
				wholeScans(plan, found)
			}
			catcher.mu.Unlock()
			r := result{best: best}
			for rel := range found {
				r.scans = append(r.scans, rel)
			}
			sort.Strings(r.scans)
			results[who.name][v.name] = r
			if best > scaleBudget {
				t.Errorf("%s: %s, want at most %s", key, best.Round(time.Millisecond), scaleBudget)
			}
			if _, whole := wholeByDesign[v.name]; len(r.scans) > 0 && !whole {
				t.Errorf("%s reads all of %s", key, strings.Join(r.scans, ", "))
			}
		}
	}

	if dir := os.Getenv(scalePlansVariable); dir != "" {
		writePlans(t, dir, catcher)
	}

	var table strings.Builder
	table.WriteString("\n| View | Administrator | Member | Reads every book |\n|---|---:|---:|---|\n")
	for _, v := range views {
		cell := func(who string) string {
			r, ok := results[who][v.name]
			if !ok {
				return "–"
			}
			return fmt.Sprintf("%d ms", r.best.Milliseconds())
		}
		fmt.Fprintf(&table, "| %s | %s | %s | %s |\n", v.name, cell("administrator"), cell("member"), wholeByDesign[v.name])
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
