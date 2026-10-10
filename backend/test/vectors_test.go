//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// vectorApp embeds with embed.Fake under the model the settings name, and
// counts how often the model is opened.
type vectorApp struct {
	*app
	vectors *similar.Service
	opened  atomic.Int32
	admin   *http.Client
	adminID uuid.UUID
	text    uuid.UUID
	bare    uuid.UUID
	wish    uuid.UUID
}

func newVectorApp(t *testing.T) *vectorApp {
	a := &vectorApp{app: newApp(t)}
	admin, adminID := a.signedIn("admin", "admin")
	a.admin, a.adminID = admin, uuid.MustParse(adminID)
	books := t.TempDir()
	long := strings.Repeat("Emma Woodhouse, handsome, clever, and rich, with a comfortable home and a happy disposition. ", 400)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, long, long, "The end.")
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	a.text = file.BookID
	ctx := context.Background()
	if err := a.pool.QueryRow(ctx, `
		INSERT INTO books (library_id, title, sort_title, title_key, description)
		VALUES ($1, 'Persuasion', 'Persuasion', 'persuasion', 'Anne Elliot, eight years on.')
		RETURNING id`, lib).Scan(&a.bare); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(ctx, `
		INSERT INTO books (library_id, title, sort_title, title_key, placeholder)
		VALUES ($1, 'Sanditon', 'Sanditon', 'sanditon', true)
		RETURNING id`, lib).Scan(&a.wish); err != nil {
		t.Fatal(err)
	}
	a.vectors = similar.NewService(a.pool, a.settings.Embedding,
		func(context.Context, embed.Spec) (embed.Embedder, error) {
			a.opened.Add(1)
			return &embed.Fake{}, nil
		}, slog.New(slog.DiscardHandler))
	return a
}

type storedVector struct {
	Kind, Model, Version string
	EmbeddedAt           time.Time
	Norm                 float64
}

func (a *vectorApp) storedVectors(book uuid.UUID) map[string]storedVector {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), `
		SELECT kind, model, model_version, embedded_at, vector_norm(embedding)
		FROM book_vectors WHERE book_id = $1`, book)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]storedVector{}
	for rows.Next() {
		var v storedVector
		if err := rows.Scan(&v.Kind, &v.Model, &v.Version, &v.EmbeddedAt, &v.Norm); err != nil {
			a.t.Fatal(err)
		}
		out[v.Kind] = v
	}
	return out
}

func (a *vectorApp) embedAll() {
	a.t.Helper()
	complete, err := a.vectors.Embed(context.Background(), time.Now().Add(time.Minute), 0)
	if err != nil || !complete {
		a.t.Fatalf("Embed: complete %v, %v", complete, err)
	}
}

func (a *vectorApp) set(key, value string) {
	a.t.Helper()
	if err := a.settings.Update(context.Background(), a.adminID, map[string]*string{key: &value}); err != nil {
		a.t.Fatal(err)
	}
}

func kinds(m map[string]storedVector) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestEveryBookWithTextGetsBothVectorsAndTheRestTheMetadataOne(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	a.embedAll()

	text := a.storedVectors(a.text)
	if got := kinds(text); !slices.Equal(got, []string{"content", "metadata"}) {
		t.Fatalf("the book with text has %v", got)
	}
	version := similar.Version(embed.E5Small.Model)
	for _, v := range text {
		if v.Model != embed.E5Small.Model.Name || v.Version != version || math.Abs(v.Norm-1) > 1e-5 {
			t.Errorf("%s vector: %+v", v.Kind, v)
		}
	}
	if got := kinds(a.storedVectors(a.bare)); !slices.Equal(got, []string{"metadata"}) {
		t.Errorf("the book without text has %v", got)
	}
	if got := a.storedVectors(a.wish); len(got) != 0 {
		t.Errorf("the placeholder has %v", kinds(got))
	}

	// The metadata vector is of the book's title, authors, series, tags and
	// description, as the model was given them.
	var metaText string
	a.pool.QueryRow(context.Background(), `SELECT book_metadata_text($1)`, a.text).Scan(&metaText)
	for _, want := range []string{"Emma", "Jane Austen", "Austen Novels", "Classics", "comedy of manners"} {
		if !strings.Contains(metaText, want) {
			t.Errorf("the metadata text %q lacks %q", metaText, want)
		}
	}
	fake, _ := (&embed.Fake{}).Embed(context.Background(), []string{embed.E5Small.Prefix + metaText})
	var distance float64
	if err := a.pool.QueryRow(context.Background(), `
		SELECT embedding <=> $2::text::vector FROM book_vectors WHERE book_id = $1 AND kind = 'metadata'`,
		a.text, vectorText(fake[0])).Scan(&distance); err != nil {
		t.Fatal(err)
	}
	if distance > 1e-5 {
		t.Errorf("the stored metadata vector is %f from the one of its text", distance)
	}

	opened := a.opened.Load()
	a.embedAll()
	if a.opened.Load() != opened {
		t.Error("a pass with nothing to do opened the model")
	}
}

func vectorText(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestAChangedBookIsEmbeddedAgainAndAGoneOneLosesItsVectors(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	a.embedAll()
	before := a.storedVectors(a.text)

	ctx := context.Background()
	if _, err := a.pool.Exec(ctx, `UPDATE books SET description = 'A novel of Highbury.' WHERE id = $1`, a.text); err != nil {
		t.Fatal(err)
	}
	a.embedAll()
	after := a.storedVectors(a.text)
	if !after["metadata"].EmbeddedAt.After(before["metadata"].EmbeddedAt) {
		t.Error("a new description left the metadata vector as it was")
	}
	if !after["content"].EmbeddedAt.Equal(before["content"].EmbeddedAt) {
		t.Error("a new description made the content vector again")
	}

	// The text read again is a new source for the content vector.
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET chunked_at = now() WHERE book_id = $1`, a.text); err != nil {
		t.Fatal(err)
	}
	a.embedAll()
	if again := a.storedVectors(a.text); !again["content"].EmbeddedAt.After(after["content"].EmbeddedAt) {
		t.Error("rechunked text left the content vector as it was")
	}

	if _, err := a.pool.Exec(ctx, `UPDATE books SET deleted_at = now() WHERE id = $1`, a.bare); err != nil {
		t.Fatal(err)
	}
	a.embedAll()
	if got := a.storedVectors(a.bare); len(got) != 0 {
		t.Errorf("a deleted book keeps %v", kinds(got))
	}
}

func TestEmbeddingPausesAndGoesOnWhereItStopped(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	ctx := context.Background()

	complete, err := a.vectors.Embed(ctx, time.Now().Add(time.Minute), 1)
	if err != nil || complete {
		t.Fatalf("a pass of one book: complete %v, %v", complete, err)
	}
	embedded := a.countRows(`SELECT count(DISTINCT book_id) FROM book_vectors`)
	if embedded != 1 {
		t.Fatalf("%d books embedded by a pass of one", embedded)
	}
	var done uuid.UUID
	if err := a.pool.QueryRow(ctx, `SELECT book_id FROM book_vectors LIMIT 1`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	first := a.storedVectors(done)

	a.set("embedding.enabled", "off")
	opened := a.opened.Load()
	complete, err = a.vectors.Embed(ctx, time.Now().Add(time.Minute), 0)
	if err != nil || !complete || a.opened.Load() != opened {
		t.Fatalf("switched off: complete %v, %v, opened %d times", complete, err, a.opened.Load()-opened)
	}
	if n := a.countRows(`SELECT count(DISTINCT book_id) FROM book_vectors`); n != 1 {
		t.Errorf("switched off, %d books are embedded", n)
	}

	a.set("embedding.enabled", "on")
	a.embedAll()
	if n := a.countRows(`SELECT count(DISTINCT book_id) FROM book_vectors`); n != 2 {
		t.Errorf("after going on, %d books are embedded", n)
	}
	for kind, v := range a.storedVectors(done) {
		if !v.EmbeddedAt.Equal(first[kind].EmbeddedAt) {
			t.Errorf("the %s vector embedded before the pause was made again", kind)
		}
	}
}

func TestChangingTheModelQueuesEmbeddingEveryBookAgain(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	a.vectors.Queue = a.server.Duplicates.Queue
	a.settings.OnChange = func(ctx context.Context, keys []string) {
		if slices.ContainsFunc(keys, func(k string) bool { return strings.HasPrefix(k, "embedding.") }) {
			if err := a.vectors.Enqueue(ctx); err != nil {
				t.Error(err)
			}
		}
	}
	a.embedAll()

	a.set("embedding.model", "multilingual-e5-small-fp32")
	if n := a.countRows(`SELECT count(*) FROM river_job WHERE kind = 'similar.embed_books' AND state = 'available'`); n != 1 {
		t.Fatalf("%d embedding jobs queued after the model changed", n)
	}
	a.embedAll()
	want := similar.Version(embed.E5SmallFP32.Model)
	for _, book := range []uuid.UUID{a.text, a.bare} {
		for kind, v := range a.storedVectors(book) {
			if v.Version != want {
				t.Errorf("%s vector of %s is of %s", kind, book, v.Version)
			}
		}
	}

	if err := a.settings.Update(context.Background(), a.adminID, map[string]*string{"embedding.model": ptr("bert")}); err == nil {
		t.Error("an unknown model was taken")
	}
}

func ptr(s string) *string { return &s }

func TestAModelThatCannotRunLeavesTheBooksForLater(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	missing := similar.NewService(a.pool, a.settings.Embedding, similar.OnnxOpener(embed.OnnxOptions{
		Runtime: "/nonexistent/libonnxruntime.so", ModelDir: t.TempDir(),
		Fetch: embed.FetchOptions{Offline: true},
	}), slog.New(slog.DiscardHandler))
	_, err := missing.Embed(context.Background(), time.Now().Add(time.Minute), 0)
	if !errors.Is(err, similar.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if n := a.countRows(`SELECT count(*) FROM book_vectors`); n != 0 {
		t.Errorf("%d vectors without a model", n)
	}
}

// TestTheRealModelEmbedsABook runs the pass with multilingual-e5-small where
// it is at hand, as TestOnnxMatchesTheReference in internal/embed does.
func TestTheRealModelEmbedsABook(t *testing.T) {
	dir, lib := os.Getenv("GOTOME_TEST_EMBED_MODEL"), os.Getenv("GOTOME_TEST_ONNXRUNTIME")
	if dir == "" || lib == "" {
		t.Skip("GOTOME_TEST_EMBED_MODEL and GOTOME_TEST_ONNXRUNTIME are not set")
	}
	a := newVectorApp(t)
	a.vectors = similar.NewService(a.pool, a.settings.Embedding, similar.OnnxOpener(embed.OnnxOptions{
		Runtime: lib, ModelDir: dir, Threads: 2, Fetch: embed.FetchOptions{Offline: true},
	}), slog.New(slog.DiscardHandler))
	a.embedAll()
	if got := kinds(a.storedVectors(a.text)); !slices.Equal(got, []string{"content", "metadata"}) {
		t.Fatalf("the book with text has %v", got)
	}
	// Emma's text and its description are about the same book; Persuasion's
	// title and description are about another.
	var near, far float64
	a.pool.QueryRow(context.Background(), `
		SELECT 1 - (c.embedding <=> m.embedding) FROM book_vectors c, book_vectors m
		WHERE c.book_id = $1 AND c.kind = 'content' AND m.book_id = $1 AND m.kind = 'metadata'`, a.text).Scan(&near)
	a.pool.QueryRow(context.Background(), `
		SELECT 1 - (c.embedding <=> m.embedding) FROM book_vectors c, book_vectors m
		WHERE c.book_id = $1 AND c.kind = 'content' AND m.book_id = $2 AND m.kind = 'metadata'`, a.text, a.bare).Scan(&far)
	t.Logf("Emma's text to its own metadata %.3f, to Persuasion's %.3f", near, far)
	if near <= far {
		t.Errorf("Emma's text is no nearer its own metadata (%.3f) than another book's (%.3f)", near, far)
	}
}
