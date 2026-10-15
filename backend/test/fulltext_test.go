//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/search"
)

// textEngine is the version of the engine GOTOME_TEST_FULLTEXT names, which
// make check-engine builds and make test-integration passes on.
var (
	textEngine     string
	textEngineOnce sync.Once
)

// openTextIndex opens a full-text index of the test's own over its
// database.
func openTextIndex(t *testing.T, pool *pgxpool.Pool) *fulltext.Service {
	t.Helper()
	lib := os.Getenv("GOTOME_TEST_FULLTEXT")
	if lib == "" {
		t.Fatal("GOTOME_TEST_FULLTEXT does not name the full-text engine; make check-engine builds it")
	}
	version, err := fulltext.Load(lib)
	if err != nil {
		t.Fatal(err)
	}
	textEngineOnce.Do(func() { textEngine = version })
	s, _, err := fulltext.OpenService(context.Background(), pool, t.TempDir(), version, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// drainingSearch applies the outbox before each search.
type drainingSearch struct {
	text *fulltext.Service
	search.Searcher
}

func (d drainingSearch) Search(ctx context.Context, scope library.Scope, q search.Query) (search.Result, error) {
	if _, err := d.text.Drain(ctx, time.Now().Add(time.Minute)); err != nil {
		return search.Result{}, err
	}
	return d.Searcher.Search(ctx, scope, q)
}

func (a *app) drain(s *fulltext.Service) {
	a.t.Helper()
	if more, err := s.Drain(context.Background(), time.Now().Add(time.Minute)); err != nil || more {
		a.t.Fatalf("drain the outbox: more %v, %v", more, err)
	}
}

// found is the IDs of the chunks the index finds for the words in the
// library.
func indexFinds(t *testing.T, s *fulltext.Service, lib uuid.UUID, words string) []int64 {
	t.Helper()
	hits, err := s.Index().Search(fulltext.Query{Words: words, Libraries: []uuid.UUID{lib}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	slices.Sort(ids)
	return ids
}

func (a *app) chunkIDs(fileID uuid.UUID) []int64 {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), "SELECT id FROM book_chunks WHERE file_id = $1 ORDER BY id", fileID)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			a.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestChunksReachTheFullTextIndexThroughTheOutbox(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	long := strings.Repeat("Emma Woodhouse, handsome, clever, and rich, with a comfortable home. ", 300)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, long, "The harpooned whale ran.")
	lib := uuid.MustParse(a.externalLibrary(admin, "Shelf", "shared", books))
	a.scanNow(lib.String())
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	ids := a.chunkIDs(file.ID)
	if len(ids) < 2 {
		t.Fatalf("%d chunks", len(ids))
	}
	if n := a.countRows("SELECT count(*) FROM book_files WHERE id = $1 AND indexed_at IS NOT NULL", file.ID); n != 1 {
		t.Error("the file is not recorded as handed to the index")
	}

	var outbox []byte
	if err := a.pool.QueryRow(ctx, "SELECT docs FROM fulltext_outbox WHERE docs <> '[]' ORDER BY id DESC LIMIT 1").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	s := a.text
	a.drain(s)
	if got := indexFinds(t, s, lib, "harpoon whales"); len(got) != 1 || got[0] != ids[len(ids)-1] {
		t.Errorf("the whale is in chunks %v, want the last of %v", got, ids)
	}
	if got := indexFinds(t, s, lib, "woodhouse"); len(got) != len(ids)-1 && len(got) != len(ids) {
		t.Errorf("Woodhouse is in chunks %v of %v", got, ids)
	}
	if n := a.countRows("SELECT count(*) FROM fulltext_outbox"); n != 0 {
		t.Errorf("%d outbox rows left", n)
	}

	// A crash between the index's commit and the outbox's delete applies
	// the rows again, which adds nothing twice.
	if _, err := a.pool.Exec(ctx, "INSERT INTO fulltext_outbox (docs) VALUES ($1)", outbox); err != nil {
		t.Fatal(err)
	}
	a.drain(s)
	a.drain(s)
	if info, err := s.Index().Info(); err != nil || info.Docs != int64(len(ids)) {
		t.Errorf("after applying twice the index holds %+v, %v; want %d chunks", info, err, len(ids))
	}

	// Read again, the old chunks go and the new ones come.
	a.extract(file.ID)
	again := a.chunkIDs(file.ID)
	a.drain(s)
	if got := indexFinds(t, s, lib, "harpoon whales"); len(got) != 1 || got[0] != again[len(again)-1] {
		t.Errorf("after reading again the whale is in %v, want the last of %v", got, again)
	}
	if info, _ := s.Index().Info(); info.Docs != int64(len(again)) {
		t.Errorf("the index holds %d chunks, the book %d", info.Docs, len(again))
	}

	// However chunks go, here with their library, they go from the index.
	if _, err := a.pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", lib); err != nil {
		t.Fatal(err)
	}
	a.drain(s)
	if info, _ := s.Index().Info(); info.Docs != 0 {
		t.Errorf("the index holds %d chunks of a deleted library", info.Docs)
	}
}

func TestReindexReadsTheFileAgainAndKeepsTheChunks(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	long := strings.Repeat("Mr. Knightley was a sensible man of about seven or eight-and-thirty. ", 300)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, long, "Highbury in the evening.")
	lib := uuid.MustParse(a.externalLibrary(admin, "Shelf", "shared", books))
	a.scanNow(lib.String())
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	ids := a.chunkIDs(file.ID)

	// As chunks cut before the index were: no place in the file, not in
	// the index, nothing waiting for it.
	if _, err := a.pool.Exec(ctx, `
		UPDATE book_chunks SET sources = '{}', source_from = 0, source_to = 0 WHERE file_id = $1;`, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET indexed_at = NULL WHERE id = $1; `, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, "DELETE FROM fulltext_outbox"); err != nil {
		t.Fatal(err)
	}
	var chunkedAt time.Time
	if err := a.pool.QueryRow(ctx, "SELECT chunked_at FROM book_files WHERE id = $1", file.ID).Scan(&chunkedAt); err != nil {
		t.Fatal(err)
	}

	s := a.text
	if more, err := a.scans.IndexFiles(ctx, time.Now().Add(time.Minute)); err != nil || more {
		t.Fatalf("index files: %v, %v", more, err)
	}
	if again := a.chunkIDs(file.ID); !slices.Equal(again, ids) {
		t.Errorf("the chunks are %v, were %v", again, ids)
	}
	var stillAt time.Time
	if err := a.pool.QueryRow(ctx, "SELECT chunked_at FROM book_files WHERE id = $1", file.ID).Scan(&stillAt); err != nil || !stillAt.Equal(chunkedAt) {
		t.Errorf("chunked at %v, was %v: the text was cut again, which embeds it again", stillAt, chunkedAt)
	}
	if n := a.countRows("SELECT count(*) FROM book_chunks WHERE file_id = $1 AND cardinality(sources) = 0", file.ID); n != 0 {
		t.Errorf("%d chunks still do not know where they are", n)
	}
	a.drain(s)
	if got := indexFinds(t, s, lib, "highbury evening"); len(got) != 1 || got[0] != ids[len(ids)-1] {
		t.Errorf("found in %v", got)
	}
	if more, err := a.scans.IndexFiles(ctx, time.Now().Add(time.Minute)); err != nil || more {
		t.Errorf("a second run: %v, %v", more, err)
	}
	var left string
	a.pool.QueryRow(ctx, "SELECT coalesce(string_agg(deletes::text || left(docs::text, 80), ' | '), '') FROM fulltext_outbox").Scan(&left)
	if left != "" {
		t.Errorf("a second run handed the index %s", left)
	}
}

func TestPassagesAreReadAgainFromTheirFiles(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	first := strings.Repeat("Emma Woodhouse, handsome, clever, and rich. ", 250)
	second := strings.Repeat("Die Kinder spielten am Ufer des Flusses. ", 250)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, first, "short", second, "The end.")
	var ws []string
	for i := range 3000 {
		ws = append(ws, fmt.Sprintf("word%d", i%97), "harbour", "lantern")
	}
	textPDF(t, filepath.Join(books, "Lanterns.pdf"), "Lanterns", ws)
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	files := a.shelfFiles()
	for _, name := range []string{"Emma.epub", "Lanterns.pdf"} {
		a.extract(files[name].ID)
	}

	rows, err := a.pool.Query(ctx, `
		SELECT c.id, c.file_id, f.format, l.root_path || '/' || f.rel_path, c.position, c.sources,
		       c.source_from, c.source_to
		FROM book_chunks c JOIN book_files f ON f.id = c.file_id JOIN libraries l ON l.id = f.library_id
		ORDER BY c.id`)
	if err != nil {
		t.Fatal(err)
	}
	var refs []ingest.PassageRef
	want := map[int64]string{}
	formats := map[string]int{}
	texts := map[uuid.UUID][]string{}
	for rows.Next() {
		var r ingest.PassageRef
		var from, to int32
		var position int32
		if err := rows.Scan(&r.ChunkID, &r.FileID, &r.Format, &r.Path, &position, &r.Sources, &from, &to); err != nil {
			t.Fatal(err)
		}
		if texts[r.FileID] == nil {
			whole, err := a.scans.FileTexts(ctx, r.FileID)
			if err != nil || whole == nil {
				t.Fatalf("the texts of %s: %v", r.Format, err)
			}
			texts[r.FileID] = whole
		}
		text := texts[r.FileID][position]
		r.Position, r.From, r.To = int(position), int(from), int(to)
		if len(r.Sources) == 0 {
			t.Errorf("chunk %d of a %s does not know where it is", r.ChunkID, r.Format)
		}
		refs = append(refs, r)
		want[r.ChunkID] = text
		formats[r.Format]++
	}
	rows.Close()
	if formats["epub"] < 2 || formats["pdf"] < 2 {
		t.Fatalf("chunks by format %v", formats)
	}
	blank := strings.NewReplacer("\n\n\n\n", "\n\n")
	got := ingest.ReadPassages(ctx, refs)
	for id, text := range want {
		if blank.Replace(got[id]) != text {
			t.Errorf("chunk %d read again:\n%.120q\nwant\n%.120q", id, got[id], text)
		}
	}

	// Without a place, a chunk is read from the file whole; from a file
	// that is gone, not at all.
	refs[0].Sources = nil
	refs[len(refs)-1].Path = filepath.Join(books, "gone.pdf")
	got = ingest.ReadPassages(ctx, []ingest.PassageRef{refs[0], refs[len(refs)-1]})
	if got[refs[0].ChunkID] != want[refs[0].ChunkID] {
		t.Errorf("read whole: %.80q", got[refs[0].ChunkID])
	}
	if text, ok := got[refs[len(refs)-1].ChunkID]; ok {
		t.Errorf("a gone file gave %.80q", text)
	}
}
