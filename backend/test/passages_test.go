//go:build integration

package test

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/passages"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// keyed embeds a text as the direction of the first of its words it
// knows, and any other text somewhere else, so that a test says which
// passages are near which description.
type keyed struct{ words []string }

func (k keyed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, embed.E5Small.Model.Dim)
		dim := -1
		for w, word := range k.words {
			if strings.Contains(strings.ToLower(t), word) {
				dim = w
				break
			}
		}
		if dim < 0 {
			h := fnv.New32a()
			h.Write([]byte(t))
			dim = len(k.words) + int(h.Sum32()%100)
		}
		v[dim] = 1
		// The common part every vector of e5 has.
		v[len(v)-1] = 0.5
		var n float64
		for _, x := range v {
			n += float64(x) * float64(x)
		}
		for j := range v {
			v[j] /= float32(math.Sqrt(n))
		}
		out[i] = v
	}
	return out, nil
}

func (keyed) Model() embed.Model { return embed.E5Small.Model }
func (keyed) Close() error       { return nil }

func TestPassagesFindTheBooksTheyAreFromOnlyWhereTheyAreSeen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	set := func(key, value string) {
		t.Helper()
		if err := a.settings.Update(context.Background(), uuid.MustParse(adminID), map[string]*string{key: &value}); err != nil {
			t.Fatal(err)
		}
	}

	// Books of several chunks, the scene in one chapter of each.
	filler := strings.Repeat("Nothing much happened in the town that week, and nobody minded. ", 150)
	shelf := func(name, visibility string, books map[string]string) string {
		dir := t.TempDir()
		for title, scene := range books {
			meta := `<dc:identifier id="uid">urn:uuid:` + title + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:language>en</dc:language>`
			writeEPUB(t, filepath.Join(dir, title+".epub"), meta, nil, filler, filler+" "+scene, filler)
		}
		lib := a.externalLibrary(admin, name, visibility, dir)
		a.scanNow(lib)
		return lib
	}
	open := shelf("Open", "shared", map[string]string{
		"The Keeper": "All winter she kept the lighthouse burning above the rocks.",
		"The Whaler": "They followed the white whale for three days and three nights.",
	})
	shelf("Vault", "private", map[string]string{
		"The Secret Keeper": "He climbed the lighthouse stairs each night of the winter.",
	})
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}

	words := keyed{[]string{"lighthouse", "whale"}}
	opener := func(context.Context, embed.Spec) (embed.Embedder, error) { return words, nil }
	quiet := slog.New(slog.DiscardHandler)
	vectors := passages.NewService(a.pool, a.passages.Store(), a.settings.Embedding, a.settings.EmbeddingPassages, opener, quiet)
	vectors.Chunks = a.scans.FileChunks
	a.server.Similar = similar.NewService(a.pool, a.settings.Embedding, opener, quiet)
	a.server.Similar.NearPassages = vectors.Near
	a.server.Similar.PassageStatus = vectors.Status
	pass := func() {
		t.Helper()
		if complete, err := vectors.Embed(context.Background(), time.Now().Add(time.Minute)); err != nil || !complete {
			t.Fatalf("pass: %v %v", complete, err)
		}
	}
	scene := url.Values{"q": {"a lighthouse in winter"}}

	// Off by default: nothing is embedded, and nothing found by passages.
	pass()
	if n := a.countRows(`SELECT count(*) FROM passage_files`); n != 0 || vectors.Store().Len() != 0 {
		t.Fatalf("embedded while off: %d files, %d passages", n, vectors.Store().Len())
	}
	if _, got, _ := a.described(admin, scene); len(got) != 0 {
		t.Errorf("found while off: %v", got)
	}
	if _, off, _ := a.call(admin, http.MethodGet, "/embedding/status", nil); off["passages"] != nil {
		t.Errorf("status while off: %v", off)
	}

	set(settings.EmbeddingPassages, "on")
	pass()
	if n := a.countRows(`SELECT count(*) FROM passage_files`); n != 3 {
		t.Fatalf("%d files embedded", n)
	}
	chunks := a.countRows(`SELECT count(*) FROM book_chunks`)
	if chunks < 6 || vectors.Store().Len() != chunks {
		t.Fatalf("%d passages stored of %d chunks", vectors.Store().Len(), chunks)
	}
	// A second pass finds nothing to do.
	before := vectors.Store().Len()
	pass()
	if vectors.Store().Len() != before {
		t.Errorf("embedded again: %d passages after %d", vectors.Store().Len(), before)
	}

	if status, got, out := a.described(reader, scene); status != 200 || len(got) == 0 || got[0] != "The Keeper" || slices.Contains(got, "The Secret Keeper") {
		t.Errorf("the reader's books: %d %v %v", status, got, out)
	}
	if _, got, _ := a.described(admin, scene); len(got) < 2 || !slices.Equal(sorted(got[:2]), []string{"The Keeper", "The Secret Keeper"}) {
		t.Errorf("the administrator's, who sees the vault: %v", got)
	}
	if _, got, _ := a.described(admin, url.Values{"q": {"a lighthouse in winter"}, "library": {open}}); slices.Contains(got, "The Secret Keeper") || len(got) == 0 {
		t.Errorf("one library's: %v", got)
	}
	if _, got, _ := a.described(reader, url.Values{"q": {"the whale"}}); len(got) == 0 || got[0] != "The Whaler" {
		t.Errorf("the whale: %v", got)
	}
	_, status, _ := a.call(admin, http.MethodGet, "/embedding/status", nil)
	if p, ok := status["passages"].(map[string]any); !ok || p["withText"] != 3.0 || p["withPassages"] != 3.0 || p["bytes"].(float64) <= 0 {
		t.Errorf("status: %v", status)
	}

	// The store may be behind the database: a book gone is not found,
	// though its passages are still stored.
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET deleted_at = now() WHERE title = 'The Whaler'`); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := a.described(reader, url.Values{"q": {"the whale"}}); slices.Contains(got, "The Whaler") {
		t.Errorf("a deleted book: %v", got)
	}

	// A file chunked again is embedded again, and the store, a fifth of
	// it then old records, written again without them.
	keeperEmbedded := func() (at time.Time) {
		t.Helper()
		if err := a.pool.QueryRow(context.Background(), `
			SELECT p.embedded_at FROM passage_files p JOIN books b ON b.primary_text_file_id = p.file_id
			WHERE b.title = 'The Keeper'`).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	first := keeperEmbedded()
	if _, err := a.pool.Exec(context.Background(), `
		UPDATE book_files f SET chunked_at = now() FROM books b
		WHERE b.primary_text_file_id = f.id AND b.title = 'The Keeper'`); err != nil {
		t.Fatal(err)
	}
	pass()
	if !keeperEmbedded().After(first) {
		t.Errorf("the Keeper was not embedded again")
	}
	if got := vectors.Store().Len(); got != chunks {
		t.Errorf("%d passages stored of %d chunks after compacting", got, chunks)
	}
	if _, got, _ := a.described(reader, scene); len(got) == 0 || got[0] != "The Keeper" {
		t.Errorf("after compacting: %v", got)
	}

	// Off again, the description search is the books' own vectors', of
	// which these books have none.
	set(settings.EmbeddingPassages, "off")
	if _, got, _ := a.described(admin, scene); len(got) != 0 {
		t.Errorf("found once off: %v", got)
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// passageOf switches passages on and stores one for the book's file, of a
// chunk it already has, as the passage the fake model embeds for a
// description of the title is.
func (a *app) passageOf(adminID, book, file, title string) {
	a.t.Helper()
	ctx := context.Background()
	on := "on"
	if err := a.settings.Update(ctx, uuid.MustParse(adminID), map[string]*string{settings.EmbeddingPassages: &on}); err != nil {
		a.t.Fatal(err)
	}
	spec := embed.E5Small
	var chunk int64
	var library uuid.UUID
	if err := a.pool.QueryRow(ctx, `
		WITH f AS (UPDATE book_files SET chunked_at = now() WHERE id = $2 RETURNING id, chunked_at),
		     b AS (UPDATE books SET primary_text_file_id = $2 WHERE id = $1),
		     p AS (INSERT INTO passage_files (file_id, model, model_version, chunked_at, passages)
		           SELECT id, $3, $4, chunked_at, 1 FROM f)
		SELECT c.id, c.library_id FROM book_chunks c WHERE c.file_id = $2 ORDER BY c.position LIMIT 1`,
		book, file, spec.Model.Name, similar.Version(spec.Model)).Scan(&chunk, &library); err != nil {
		a.t.Fatal(err)
	}
	vecs, err := (&embed.Fake{}).Embed(ctx, []string{spec.Prefix + title})
	if err != nil {
		a.t.Fatal(err)
	}
	store := a.passages.Store()
	if store.Meta() == nil {
		if err := store.Begin(spec.Model.Name, similar.Version(spec.Model), make([]float32, spec.Model.Dim)); err != nil {
			a.t.Fatal(err)
		}
	}
	if err := store.Append([]passages.Record{{Chunk: chunk, Library: library, Vector: vecs[0]}}); err != nil {
		a.t.Fatal(err)
	}
}
