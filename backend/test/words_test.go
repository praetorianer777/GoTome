//go:build integration

package test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

type storedWords struct {
	Counts    wordcount.Counts
	Version   int
	Same      bool
	Encounted int
}

func (a *app) fileWords(fileID uuid.UUID) (storedWords, bool) {
	a.t.Helper()
	var raw []byte
	var out storedWords
	err := a.pool.QueryRow(context.Background(), `
		SELECT w.counts, w.version, w.chunked_at = f.chunked_at, w.words
		FROM file_words w JOIN book_files f ON f.id = w.file_id WHERE w.file_id = $1`, fileID).
		Scan(&raw, &out.Version, &out.Same, &out.Encounted)
	if err != nil {
		return out, false
	}
	counts, err := wordcount.Decode(raw)
	if err != nil {
		a.t.Fatal(err)
	}
	out.Counts = counts
	return out, true
}

func TestWordsAreCountedWhenAFileIsChunkedAndCountedAgainWhenItsTextChanges(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	path := filepath.Join(books, "Emma.epub")
	writeEPUB(t, path, emmaMetadata, nil, strings.Repeat("Emma Woodhouse walked in the garden. ", 100))
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)

	w, ok := a.fileWords(file.ID)
	if !ok || !w.Same || w.Version != wordcount.Version {
		t.Fatalf("counted %v, current %v, version %d", ok, w.Same, w.Version)
	}
	if w.Counts["woodhouse"] != 100 || w.Counts["garden"] != 100 || w.Counts["in"] != 0 || w.Encounted != len(w.Counts) {
		t.Errorf("counts %v", w.Counts)
	}

	writeEPUB(t, path, emmaMetadata, nil, strings.Repeat("Harriet Smith sat by the window. ", 50))
	ctx := context.Background()
	if _, err := a.pool.Exec(ctx, "UPDATE book_files SET chunked_at = NULL WHERE id = $1", file.ID); err != nil {
		t.Fatal(err)
	}
	if w, ok := a.fileWords(file.ID); ok && w.Same {
		t.Error("counts of a file waiting to be chunked count as current")
	}
	if err := a.scans.Chunk(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	w, ok = a.fileWords(file.ID)
	if !ok || !w.Same || w.Counts["harriet"] != 50 || w.Counts["woodhouse"] != 0 {
		t.Errorf("counts after the text changed: %v (current %v)", w.Counts, w.Same)
	}
	if n := a.countRows("SELECT count(*) FROM file_words WHERE file_id = $1", file.ID); n != 1 {
		t.Errorf("%d rows for one file", n)
	}
}

func TestBooksChunkedBeforeCountingAreCountedByAJobThatIsSafeToRepeat(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, strings.Repeat("Emma Woodhouse walked in the garden. ", 100))
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	before, _ := a.fileWords(file.ID)

	ctx := context.Background()
	if _, err := a.pool.Exec(ctx, "DELETE FROM file_words WHERE file_id = $1", file.ID); err != nil {
		t.Fatal(err)
	}
	more, err := a.scans.CountFiles(ctx, time.Now().Add(time.Minute))
	if err != nil || more {
		t.Fatalf("more %v, error %v", more, err)
	}
	after, ok := a.fileWords(file.ID)
	if !ok || !after.Same || after.Counts["woodhouse"] != before.Counts["woodhouse"] || len(after.Counts) != len(before.Counts) {
		t.Fatalf("counted again: %v %v", ok, after.Counts)
	}

	if more, err := a.scans.CountFiles(ctx, time.Now().Add(time.Minute)); err != nil || more {
		t.Fatalf("second run: more %v, error %v", more, err)
	}
	if n := a.countRows("SELECT count(*) FROM file_words WHERE file_id = $1", file.ID); n != 1 {
		t.Errorf("%d rows after a repeat", n)
	}
	if again, _ := a.fileWords(file.ID); len(again.Counts) != len(after.Counts) {
		t.Error("the repeat changed the counts")
	}
}

func TestCountsGoWhenTheFileIsNoLongerTheBooksText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, strings.Repeat("Emma Woodhouse walked in the garden. ", 100))
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	if _, ok := a.fileWords(file.ID); !ok {
		t.Fatal("not counted")
	}
	if err := sqlc.New(a.pool).DeleteFileChunks(context.Background(), file.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.fileWords(file.ID); ok {
		t.Error("the counts of a file without chunks stay")
	}
}
