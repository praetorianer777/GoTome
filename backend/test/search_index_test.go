//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/ingest"
)

// searchStatus asks how much of the text search knows.
func (a *app) searchStatus(c *http.Client, query string) (int, map[string]any) {
	a.t.Helper()
	status, body := a.get(c, "/search/status"+query)
	var out map[string]any
	if status == 200 {
		if err := json.Unmarshal(body, &out); err != nil {
			a.t.Fatal(err)
		}
	}
	return status, out
}

// chunkQueued cuts the text of every file queued for it, as the chunk
// worker would.
func (a *app) chunkQueued() int {
	a.t.Helper()
	ctx := context.Background()
	rows, err := a.pool.Query(ctx, `
		SELECT (args->>'fileId')::uuid FROM river_job
		WHERE kind = 'ingest.chunk_file' AND state = 'available'`)
	if err != nil {
		a.t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			a.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := a.scans.Chunk(ctx, id); err != nil {
			a.t.Fatal(err)
		}
	}
	if _, err := a.pool.Exec(ctx, "UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'ingest.chunk_file' AND state = 'available'"); err != nil {
		a.t.Fatal(err)
	}
	return len(ids)
}

func TestTheTextAndItsIndexAreBuiltAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")

	books := t.TempDir()
	meta := func(title string) string {
		return `<dc:identifier id="uid">urn:uuid:` + title + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator opf:role="aut">Anna Reiter</dc:creator>
    <dc:language>en</dc:language>`
	}
	filler := strings.Repeat("Nothing much happened in the town that week, and the weather was mild. ", 40)
	writeEPUB(t, filepath.Join(books, "Horses.epub"), meta("Horses"), nil, filler, "The horses ran along the river. "+filler)
	writeEPUB(t, filepath.Join(books, "Sea.epub"), meta("Sea"), nil, filler, "Out at sea they saw the white whale at last. "+filler)
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	whale := func() []string {
		t.Helper()
		_, got := a.searchText(admin, url.Values{"q": {"whale"}})
		return titles(got)
	}
	if got := whale(); !slices.Equal(got, []string{"Sea"}) {
		t.Fatalf("before: %v", got)
	}
	if status, got := a.searchStatus(admin, "?library="+lib); status != 200 || got["files"] != 2.0 || got["indexed"] != 2.0 || got["rebuilding"] != false || got["engine"] == "" {
		t.Fatalf("status: %d %v", status, got)
	}

	// Reading a library's text again leaves search finding the old text
	// until the new one is in.
	status, out, _ := a.call(admin, http.MethodPost, "/search/reread", map[string]any{"library": lib})
	if status != 202 || out["files"] != 2.0 {
		t.Fatalf("reread the library: %d %v", status, out)
	}
	if _, got := a.searchStatus(admin, "?library="+lib); got["indexed"] != 0.0 {
		t.Errorf("after asking: %v", got)
	}
	if got := whale(); !slices.Equal(got, []string{"Sea"}) {
		t.Errorf("while being read again: %v", got)
	}
	if n := a.chunkQueued(); n != 2 {
		t.Errorf("chunked %d files", n)
	}
	if _, got := a.searchStatus(admin, ""); got["files"] != 2.0 || got["indexed"] != 2.0 {
		t.Errorf("after: %v", got)
	}
	if got := whale(); !slices.Equal(got, []string{"Sea"}) {
		t.Errorf("after: %v", got)
	}

	// One book, and nothing a caller may not ask for.
	_, found := a.searchText(admin, url.Values{"q": {"whale"}})
	sea := found.Books[0].Book.ID
	if status, out, _ := a.call(admin, http.MethodPost, "/search/reread", map[string]any{"book": sea}); status != 202 || out["files"] != 1.0 {
		t.Errorf("reread a book: %d %v", status, out)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/search/reread", map[string]any{"book": uuid.NewString()}); status != 404 {
		t.Errorf("reread no book: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/search/reread", map[string]any{"library": uuid.NewString()}); status != 404 {
		t.Errorf("reread no library: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/search/reread", map[string]any{}); status != 403 {
		t.Errorf("a reader rereads: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/search/rebuild", nil); status != 403 {
		t.Errorf("a reader rebuilds: %d", status)
	}
	a.chunkQueued()

	// A new pg_search, as an upgraded database image brings, queues the
	// rebuild at start; the rebuild needs the chunks alone, not the files.
	var engine string
	if err := a.pool.QueryRow(ctx, "SELECT extversion FROM pg_extension WHERE extname = 'pg_search'").Scan(&engine); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO index_versions (name, version) VALUES ('pg_search', '0.0.1')
		ON CONFLICT (name) DO UPDATE SET version = EXCLUDED.version`); err != nil {
		t.Fatal(err)
	}
	if err := a.server.Index.CheckEngine(ctx); err != nil {
		t.Fatal(err)
	}
	if _, got := a.searchStatus(admin, ""); got["rebuilding"] != true {
		t.Errorf("no rebuild queued: %v", got)
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/search/rebuild", nil); status != 202 || out["queued"] != false {
		t.Errorf("a second rebuild: %d %v", status, out)
	}
	if err := os.RemoveAll(books); err != nil {
		t.Fatal(err)
	}
	if err := a.server.Index.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	var recorded string
	if err := a.pool.QueryRow(ctx, "SELECT version FROM index_versions WHERE name = 'pg_search'").Scan(&recorded); err != nil || recorded != engine {
		t.Errorf("recorded %q, %v; want %q", recorded, err, engine)
	}
	if got := whale(); !slices.Equal(got, []string{"Sea"}) {
		t.Errorf("after the rebuild: %v", got)
	}

	// A new chunker marks every file to be read again, and search keeps
	// what it has meanwhile.
	if _, err := a.pool.Exec(ctx, `INSERT INTO index_versions (name, version) VALUES ('chunks', '0')
		ON CONFLICT (name) DO UPDATE SET version = EXCLUDED.version`); err != nil {
		t.Fatal(err)
	}
	if err := a.scans.CheckChunkVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if _, got := a.searchStatus(admin, ""); got["indexed"] != 0.0 {
		t.Errorf("after a new chunker: %v", got)
	}
	if err := a.pool.QueryRow(ctx, "SELECT version FROM index_versions WHERE name = 'chunks'").Scan(&recorded); err != nil || recorded != ingest.ChunkVersion {
		t.Errorf("chunk version %q, %v", recorded, err)
	}
	if got := whale(); !slices.Equal(got, []string{"Sea"}) {
		t.Errorf("after a new chunker: %v", got)
	}
}
