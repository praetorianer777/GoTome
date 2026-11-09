//go:build integration

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/perf"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// perfNames is everything the seeded library calls by name. None of it may
// reach the output of perf-check, whatever the view or the error.
var perfNames = []string{
	"Zephyrine Marzipan", "Quillon Marzipan Atlas", "Xanthippe Marzipan Orrery",
	"Ottoline Vandermeer", "Cornelius Pettibone", "Thaddeus Quibble",
	"Lumenfall Cycle", "Brindlewick Tales", "Gallowglass",
	"Cartographic Fiction", "Zephyrine",
	"Bibliotheca Zyx", "Ottoline", "Vandermeer", "Pettibone", "Quibble", "Marzipan", "Orrery",
	"Lumenfall", "Brindlewick", "Cartographic", "Quillon", "Xanthippe", "Thaddeus", "Cornelius",
	"A lighthouse keeper catalogues forgotten tides",
	"Midsommar Collection",
	"perfwatcher",
}

// seedPerf makes six books with names that must not come out again, a text
// of words from the titles each, in the index and with its vectors, and a
// collection. It returns the account the check asks as.
func seedPerf(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	key, err := os.ReadFile(os.Getenv("GOTOME_SECRET_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := secret.ParseKey(string(key))
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Open(ctx, pool, box); err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := accounts.Setup(ctx, "perfwatcher", "the-first-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.NewService(pool, t.TempDir()).Create(ctx, admin.ID, library.NewLibrary{Name: "Bibliotheca Zyx", Mode: "managed", Visibility: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	books := []struct{ title, author, series, tag string }{
		{"Zephyrine Marzipan", "Ottoline Vandermeer", "Lumenfall Cycle", "Cartographic Fiction"},
		{"Quillon Marzipan Atlas", "Ottoline Vandermeer", "Lumenfall Cycle", "Cartographic Fiction"},
		{"Xanthippe Marzipan Orrery", "Ottoline Vandermeer", "", "Cartographic Fiction"},
		{"Brindlewick Tales Marzipan", "Cornelius Pettibone", "Brindlewick Tales", "Cartographic Fiction"},
		{"Gallowglass Marzipan", "Cornelius Pettibone", "", "Cartographic Fiction"},
		{"Thaddeus Marzipan Orrery", "Thaddeus Quibble", "", "Cartographic Fiction"},
	}
	catalogue := catalog.NewService(pool)
	model := embed.Specs[embed.DefaultSpec].Model
	var docs []fulltext.Doc
	for i, b := range books {
		id, err := catalogue.CreateBook(ctx, catalog.NewBook{
			LibraryID: lib.ID, Title: b.title, Series: b.series, Tags: []string{b.tag}, Language: "en",
			Description:  "A lighthouse keeper catalogues forgotten tides",
			Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var fileID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO book_files (book_id, library_id, kind, format, rel_path, size_bytes, modified_at, sha256, extract_state, has_text, chunked_at)
			VALUES ($1, $2, 'ebook', 'epub', $3::text, 1000, now(), sha256($3::text::bytea), 'done', true, now()) RETURNING id`,
			id, lib.ID, fmt.Sprintf("Zyx/%d.epub", i)).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE books SET primary_text_file_id = $2 WHERE id = $1`, id, fileID); err != nil {
			t.Fatal(err)
		}
		var chunk int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO book_chunks (book_id, library_id, file_id, position, page_from, page_to, char_offset, lang)
			VALUES ($1, $2, $3, 0, 1, 3, 0, 'en') RETURNING id`, id, lib.ID, fileID).Scan(&chunk); err != nil {
			t.Fatal(err)
		}
		docs = append(docs, fulltext.Doc{ID: chunk, BookID: id, LibraryID: lib.ID, Lang: "en", Text: strings.ToLower(b.title) + " marzipan marzipan orrery"})
		for _, kind := range []string{similar.KindContent, similar.KindMetadata} {
			v := make([]string, model.Dim)
			for j := range v {
				v[j] = "0"
			}
			v[0], v[i+1] = "1", "0.1"
			if _, err := pool.Exec(ctx, `
				INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
				VALUES ($1, $2, $3, $4, sha256('test'), $5::text::vector)`,
				id, kind, model.Name, similar.Version(model), "["+strings.Join(v, ",")+"]"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO search_words (word) VALUES ('marzipan'), ('orrery') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var collection uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO collections (owner_id, name, visibility) VALUES ($1, 'Midsommar Collection', 'shared') RETURNING id`, admin.ID).Scan(&collection); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO collection_items (collection_id, book_id, position) SELECT $1, id, 1 FROM books LIMIT 1`, collection); err != nil {
		t.Fatal(err)
	}

	lib2 := os.Getenv("GOTOME_TEST_FULLTEXT")
	if lib2 == "" {
		t.Fatal("GOTOME_TEST_FULLTEXT does not name the full-text engine; make check-engine builds it")
	}
	t.Setenv("GOTOME_FULLTEXT_LIBRARY", lib2)
	engine, err := fulltext.Load(lib2)
	if err != nil {
		t.Fatal(err)
	}
	service, _, err := fulltext.OpenService(ctx, pool, fulltext.Dir(os.Getenv("GOTOME_DATA_DIR")), engine, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Index().Apply(fulltext.Batch{Add: docs}); err != nil {
		t.Fatal(err)
	}
	if err := service.Index().Commit(); err != nil {
		t.Fatal(err)
	}
	service.Close()
}

func perfOutputNamesNothing(t *testing.T, out string) {
	t.Helper()
	lower := strings.ToLower(out)
	for _, name := range perfNames {
		if strings.Contains(lower, strings.ToLower(name)) {
			t.Errorf("the output names %q:\n%s", name, out)
		}
	}
	if id := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-`).FindString(out); id != "" {
		t.Errorf("the output holds an ID (%s):\n%s", id, out)
	}
}

func TestPerfCheckTimesEveryViewAndNamesNothing(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	localOpener = func(embed.OnnxOptions) similar.Opener {
		return func(context.Context, embed.Spec) (embed.Embedder, error) { return &embed.Fake{}, nil }
	}
	t.Cleanup(func() { localOpener = similar.OnnxOpener })
	seedPerf(t, pool)

	count := func(table string) (n int) {
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tables := []string{"river_job", "sessions", "settings", "notifications", "book_chunks", "search_words", "users", "books"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = count(table)
	}

	out, err := command(t, "", "perf-check", "-pause", "0", "-skip", "jobs, trash")
	if err != nil {
		t.Fatalf("perf-check: %v\n%s", err, out)
	}
	t.Log(out)
	perfOutputNamesNothing(t, out)

	// A view is a row, whatever it found; the ones that ask about a
	// smart shelf, which the library has none of, say so.
	all := perf.Views(perf.Params{Describe: "x"})
	for _, v := range all {
		if !strings.Contains(out, "| "+v.Name+" |") {
			t.Errorf("no row for %q:\n%s", v.Name, out)
		}
	}
	for _, line := range []string{"| smart shelf | nothing in the library to ask about |", "| jobs | skipped |", "| trash | skipped |"} {
		if !strings.Contains(out, line) {
			t.Errorf("no %q:\n%s", line, out)
		}
	}
	rows := func(view string) int {
		for _, line := range strings.Split(out, "\n") {
			if cells := strings.Split(line, "|"); len(cells) > 3 && strings.TrimSpace(cells[1]) == view {
				n, err := strconv.Atoi(strings.TrimSpace(cells[3]))
				if err != nil {
					t.Fatalf("%s: rows %q", view, cells[3])
				}
				return n
			}
		}
		t.Fatalf("no row for %s", view)
		return 0
	}
	for view, want := range map[string]int{"library by title": 6, "full-text search": 3, "collection": 1, "an author's books": 3} {
		if got := rows(view); got < 1 || (view == "library by title" && got != want) {
			t.Errorf("%s answered %d rows, want %d", view, got, want)
		}
	}
	for _, want := range []string{"| Books | 6, with 6 files |", "| Authors and series | 3 and 2 |", "| Books with vectors | 6 |", "PostgreSQL", "shared_buffers", "Search words from | the titles"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}

	for _, table := range tables {
		if got := count(table); got != before[table] {
			t.Errorf("%s has %d rows, had %d: the check wrote", table, got, before[table])
		}
	}
}

func TestPerfCheckStopsOnAFailureWithoutNamingAnything(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	localOpener = func(embed.OnnxOptions) similar.Opener {
		return func(context.Context, embed.Spec) (embed.Embedder, error) { return &embed.Fake{}, nil }
	}
	t.Cleanup(func() { localOpener = similar.OnnxOpener })
	seedPerf(t, pool)

	out, err := command(t, "", "perf-check", "-pause", "0", "-timeout", "1ns")
	if err == nil {
		t.Fatalf("a request that cannot answer in a nanosecond is not an error:\n%s", out)
	}
	perfOutputNamesNothing(t, out)
	perfOutputNamesNothing(t, err.Error())
	if !strings.Contains(err.Error(), `"library by title"`) {
		t.Errorf("the error does not say which view: %v", err)
	}

	if _, err := command(t, "", "perf-check", "-user", "nobody-of-that-name"); err == nil || strings.Contains(err.Error(), "nobody-of-that-name") {
		t.Errorf("an unknown account: %v", err)
	}
}

func TestPerfCheckConnectionsRefuseEveryWrite(t *testing.T) {
	pool := dbtest.New(t)
	url, err := readOnlyURL(pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	readOnly, err := db.Open(context.Background(), url, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if _, err := readOnly.Exec(context.Background(), `INSERT INTO search_words (word) VALUES ('anything')`); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a write on a check's connection: %v", err)
	}
	if _, err := readOnly.Exec(context.Background(), `SELECT 1`); err != nil {
		t.Errorf("a read: %v", err)
	}
}
