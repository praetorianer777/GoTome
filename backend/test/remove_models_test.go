//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/passages"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// putModelFiles writes a file of every name of the spec's into dir, of a
// few bytes each.
func putModelFiles(t *testing.T, dir string, spec embed.Spec) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, spec.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.Files() {
		if err := os.WriteFile(filepath.Join(dir, spec.Dir, f.Name), []byte("weights of "+spec.Model.Weights), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (a *vectorApp) vectorCount(spec embed.Spec) int {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM book_vectors WHERE model = $1 AND model_version = $2`,
		spec.Model.Name, similar.Version(spec.Model)).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestAModelNoLongerChosenIsRemovedOnceItsTimeHasCome(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	ctx := context.Background()
	models := t.TempDir()
	a.vectors.ModelDir = models
	a.vectors.RemoveAfter = a.settings.RemoveUnusedAfter
	a.vectors.PassageBytes = a.passages.Bytes
	a.vectors.RemovePassages = a.passages.Remove
	a.server.Similar.ModelDir = models
	a.server.Similar.RemoveAfter = a.settings.RemoveUnusedAfter

	// bge-m3 was chosen, then e5-small again: bge-m3 is unused from now.
	a.set(settings.EmbeddingModel, "bge-m3")
	a.set(settings.EmbeddingModel, "multilingual-e5-small")
	putModelFiles(t, models, embed.BGEM3)
	putModelFiles(t, models, embed.E5Small)
	putModelFiles(t, models, embed.E5SmallFP32)
	a.putVectorOf(a.bare, similar.KindMetadata, embed.E5Small)
	a.putVectorOf(a.text, similar.KindMetadata, embed.E5SmallFP32)
	// More vectors of bge-m3 than one batch deletes, two a book.
	if _, err := a.pool.Exec(ctx, `
		WITH more AS (
			INSERT INTO books (library_id, title, sort_title, title_key)
			SELECT library_id, 'Copy ' || n, 'Copy ' || n, 'copy ' || n
			FROM books, generate_series(1, 400) n WHERE id = $1
			RETURNING id
		)
		INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
		SELECT more.id, kind, $2, $3, sha256('test'), array_fill(0.5, ARRAY[1024])::vector
		FROM more, unnest(ARRAY['metadata', 'content']) kind`,
		a.bare, embed.BGEM3.Model.Name, similar.Version(embed.BGEM3.Model)); err != nil {
		t.Fatal(err)
	}
	// The passage store holds bge-m3's passages, and passage_files says so.
	store := a.passages.Store()
	centre := make([]float32, 1024)
	centre[0] = 1
	if err := store.Begin(embed.BGEM3.Model.Name, similar.Version(embed.BGEM3.Model), centre); err != nil {
		t.Fatal(err)
	}
	if err := store.Append([]passages.Record{{Chunk: 1, Library: uuid.New(), Vector: centre}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `
		INSERT INTO passage_files (file_id, model, model_version, chunked_at, passages)
		SELECT id, $2, $3, chunked_at, 1 FROM book_files WHERE book_id = $1`,
		a.text, embed.BGEM3.Model.Name, similar.Version(embed.BGEM3.Model)); err != nil {
		t.Fatal(err)
	}

	admin := library.Scope{Viewer: a.adminID, SeesAll: true}
	list, err := a.vectors.Models(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]similar.ModelChoice{}
	for _, m := range list.Models {
		byName[m.Name] = m
	}
	bge, small, fp32 := byName["bge-m3"], byName["multilingual-e5-small"], byName["multilingual-e5-small-fp32"]
	if bge.StoredBytes <= store.Size() || bge.DatabaseBytes < 800*1024 || bge.UnusedSince == nil || bge.RemoveAt == nil ||
		bge.RemoveAt.Sub(*bge.UnusedSince) != 24*time.Hour {
		t.Errorf("bge-m3 before: %d on disk, %d in the database, unused since %v, removed at %v",
			bge.StoredBytes, bge.DatabaseBytes, bge.UnusedSince, bge.RemoveAt)
	}
	if small.UnusedSince != nil || small.RemoveAt != nil || small.StoredBytes == 0 || small.DatabaseBytes == 0 {
		t.Errorf("the chosen model: %+v", small)
	}
	// fp32 shares e5-small's tokenizer, which removing it does not free.
	if want := int64(len("weights of fp32")); fp32.StoredBytes != want {
		t.Errorf("fp32 takes %d bytes of its own, want %d", fp32.StoredBytes, want)
	}

	// Within the day nothing goes; fp32, not known to be unused, is from now.
	if err := a.vectors.RemoveUnused(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := a.vectorCount(embed.BGEM3); n != 800 {
		t.Fatalf("bge-m3 has %d vectors within its day", n)
	}
	if _, err := os.Stat(filepath.Join(models, "bge-m3", "model.onnx_data")); err != nil {
		t.Fatalf("bge-m3's files within its day: %v", err)
	}

	if err := a.vectors.RemoveUnused(ctx, time.Now().Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := a.vectorCount(embed.BGEM3); n != 0 {
		t.Errorf("bge-m3 has %d vectors after its day", n)
	}
	if n := a.vectorCount(embed.E5SmallFP32); n != 0 {
		t.Errorf("fp32 has %d vectors after its day", n)
	}
	if n := a.vectorCount(embed.E5Small); n != 1 {
		t.Errorf("the chosen model has %d vectors, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(models, "bge-m3")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bge-m3's folder: %v", err)
	}
	for _, f := range embed.E5Small.Files() {
		if _, err := os.Stat(filepath.Join(models, embed.E5Small.Dir, f.Name)); err != nil {
			t.Errorf("the chosen model's %s: %v", f.Name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(models, embed.E5Small.Dir, embed.E5SmallFP32.Weights.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("fp32's weights: %v", err)
	}
	if store.Meta() != nil {
		t.Errorf("the passage store still holds %v", store.Meta())
	}
	var files int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM passage_files`).Scan(&files); err != nil || files != 0 {
		t.Errorf("%d passage files left: %v", files, err)
	}

	// The chosen model is never removed, and choosing one ends its time.
	if _, err := a.vectors.RemoveModel(ctx, "multilingual-e5-small"); !errors.Is(err, similar.ErrModelChosen) {
		t.Errorf("removing the chosen model: %v", err)
	}
	a.set(settings.EmbeddingModel, "bge-m3")
	if err := a.vectors.RemoveUnused(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	list, err = a.vectors.Models(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list.Models {
		if m.Name == "bge-m3" && m.UnusedSince != nil {
			t.Errorf("bge-m3 chosen again is unused since %v", m.UnusedSince)
		}
		if m.Name == "multilingual-e5-small" && m.UnusedSince == nil {
			t.Error("e5-small, left just now, is not unused")
		}
	}
	if n := a.vectorCount(embed.E5Small); n != 1 {
		t.Errorf("e5-small left just now has %d vectors, want 1", n)
	}

	// Remove now: queued for a model not chosen, refused for the chosen one.
	editor, _ := a.signedIn("editor", "editor")
	for _, c := range []struct {
		who  *http.Client
		name string
		want int
	}{
		{a.admin, "multilingual-e5-small", 202},
		{a.admin, "bge-m3", 409},
		{a.admin, "gpt-17", 404},
		{editor, "multilingual-e5-small", 403},
	} {
		if code, out, _ := a.call(c.who, http.MethodPost, "/embedding/models/"+c.name+"/remove", nil); code != c.want {
			t.Errorf("remove %s: %d %v, want %d", c.name, code, out, c.want)
		}
	}
	var queued int
	if err := a.pool.QueryRow(ctx, `
		SELECT count(*) FROM river_job WHERE kind = 'similar.remove_models' AND args->>'model' = 'multilingual-e5-small'`,
	).Scan(&queued); err != nil || queued != 1 {
		t.Errorf("%d removals queued: %v", queued, err)
	}
	if _, err := a.vectors.RemoveModel(ctx, "multilingual-e5-small"); err != nil {
		t.Fatal(err)
	}
	if n := a.vectorCount(embed.E5Small); n != 0 {
		t.Errorf("e5-small removed has %d vectors", n)
	}
}
