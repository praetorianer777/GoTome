//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// writableLibrary is an external library GOtome may change files in.
func (a *app) writableLibrary(admin *http.Client, name, root string) string {
	a.t.Helper()
	id := a.externalLibrary(admin, name, "shared", root)
	if status, body, _ := a.call(admin, http.MethodPatch, "/libraries/"+id, map[string]any{"writable": true}); status != 200 {
		a.t.Fatalf("make %s writable: %d %v", name, status, body)
	}
	return id
}

func (a *app) writeBack(id uuid.UUID) error {
	a.t.Helper()
	return a.scans.WriteBack(context.Background(), id)
}

// writeJobs counts the queued jobs that write into the file.
func (a *app) writeJobs(id uuid.UUID) int {
	a.t.Helper()
	var n int
	err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'ingest.write_metadata' AND args->>'fileId' = $1`, id.String()).Scan(&n)
	if err != nil {
		a.t.Fatal(err)
	}
	return n
}

type fileHashes struct {
	sha, original, content []byte
	size                   int64
	modified               time.Time
}

func (a *app) hashes(id uuid.UUID) fileHashes {
	a.t.Helper()
	var h fileHashes
	err := a.pool.QueryRow(context.Background(),
		`SELECT sha256, original_sha256, content_sha256, size_bytes, modified_at FROM book_files WHERE id = $1`, id).
		Scan(&h.sha, &h.original, &h.content, &h.size, &h.modified)
	if err != nil {
		a.t.Fatal(err)
	}
	return h
}

func readEPUB(t *testing.T, path string) *epub.Book {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	book, err := epub.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func TestAnEditIsWrittenIntoTheEPUB(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	path := filepath.Join(folder, "Emma.epub")
	writeEPUB(t, path, emmaMetadata, coverPNG(t), "Emma Woodhouse, handsome, clever, and rich.")
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)
	before := a.hashes(emma.ID)
	bookPath := "/books/" + emma.BookID.String()

	if status, body, _ := a.call(editor, http.MethodPatch, bookPath, map[string]any{"locks": map[string]bool{"title": true}}); status != 200 {
		t.Fatalf("lock: %d %v", status, body)
	}
	if n := a.writeJobs(emma.ID); n != 0 {
		t.Errorf("a change of locks alone queued %d writes", n)
	}
	status, body, _ := a.call(editor, http.MethodPatch, bookPath, map[string]any{
		"title":  "Emma (Annotated)",
		"series": map[string]any{"name": "Penguin Austen", "index": 2},
		"tags":   []string{"Romance"},
	})
	if status != 200 {
		t.Fatalf("edit: %d %v", status, body)
	}
	if n := a.writeJobs(emma.ID); n != 1 {
		t.Fatalf("%d writes queued, want 1", n)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}

	written := readEPUB(t, path)
	md := written.Metadata
	if md.Title != "Emma (Annotated)" || md.Series != "Penguin Austen" || *md.SeriesIndex != 2 ||
		!slices.Equal(md.Subjects, []string{"Romance"}) || md.Publisher != "Penguin Classics" {
		t.Errorf("the file says %+v", md)
	}
	after := a.hashes(emma.ID)
	info, _ := os.Stat(path)
	if bytes.Equal(after.sha, before.sha) || !bytes.Equal(after.original, before.original) ||
		!bytes.Equal(after.content, before.content) || !bytes.Equal(written.ContentHash, before.content) {
		t.Errorf("hashes before %x/%x/%x, after %x/%x/%x", before.sha, before.original, before.content, after.sha, after.original, after.content)
	}
	if after.size != info.Size() || !after.modified.Equal(info.ModTime().UTC().Truncate(time.Microsecond)) {
		t.Errorf("recorded %d bytes at %v, the file has %d at %v", after.size, after.modified, info.Size(), info.ModTime())
	}
	// The scan finds the file as recorded, and does not read it again.
	a.scanNow(lib)
	if state := a.shelfFiles()["Emma.epub"].State; state != "done" {
		t.Errorf("after the next scan the file is %s", state)
	}

	// A cover chosen by hand goes in too.
	if status, body := a.putCover(editor, emma.BookID.String(), squarePNG(t)); status != 200 {
		t.Fatalf("cover: %d %v", status, body)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if cover := readEPUB(t, path).Cover; cover == nil || cover.MediaType != "image/jpeg" {
		t.Errorf("the file's cover: %+v", cover)
	}

	// Another GOtome, or another library, reads what was written.
	elsewhere := t.TempDir()
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(filepath.Join(elsewhere, "Copy.epub"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	a.scanNow(a.externalLibrary(admin, "Elsewhere", "shared", elsewhere))
	copied := a.shelfFiles()["Copy.epub"]
	a.extract(copied.ID)
	if book := a.book(copied.BookID); book.Title != "Emma (Annotated)" || book.Series != "Penguin Austen" {
		t.Errorf("the copy is imported as %q in %q", book.Title, book.Series)
	}
}

func TestNothingIsWrittenWhereItMayNotBe(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")

	readOnly := t.TempDir()
	writeEPUB(t, filepath.Join(readOnly, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	a.scanNow(a.externalLibrary(admin, "Read only", "shared", readOnly))
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)
	original, _ := os.ReadFile(filepath.Join(readOnly, "Emma.epub"))
	a.call(editor, http.MethodPatch, "/books/"+emma.BookID.String(), map[string]any{"title": "Changed"})
	if n := a.writeJobs(emma.ID); n != 0 {
		t.Errorf("%d writes queued into a library GOtome may not change", n)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(filepath.Join(readOnly, "Emma.epub")); !bytes.Equal(now, original) {
		t.Error("a file of a read-only library was changed")
	}
}

func TestAFailedWriteLeavesTheOriginal(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	path := filepath.Join(folder, "Emma.epub")
	writeEPUB(t, path, emmaMetadata, nil, "Emma Woodhouse.")
	a.scanNow(a.writableLibrary(admin, "Shelf", folder))
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)
	a.call(editor, http.MethodPatch, "/books/"+emma.BookID.String(), map[string]any{"title": "Changed"})
	original, _ := os.ReadFile(path)
	before := a.hashes(emma.ID)

	// No room for the new file next to the old one.
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	err := a.writeBack(emma.ID)
	_ = os.Chmod(folder, 0o755)
	if err == nil {
		t.Fatal("the write succeeded in a folder that takes no new files")
	}
	entries, _ := os.ReadDir(folder)
	if now, _ := os.ReadFile(path); !bytes.Equal(now, original) || len(entries) != 1 || !bytes.Equal(a.hashes(emma.ID).sha, before.sha) {
		t.Errorf("after the failed write: the file is the same: %v, entries %v", bytes.Equal(now, original), entries)
	}

	// Changed on disk since it was hashed: the scan reads it, nothing writes.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.Write([]byte("trailing bytes"))
	f.Close()
	changed, _ := os.ReadFile(path)
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, changed) {
		t.Error("a file changed outside GOtome was written over")
	}
}
