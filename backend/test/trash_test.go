//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

type trashView struct {
	Files []struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		BookTitle string    `json:"bookTitle"`
		TrashedBy string    `json:"trashedBy"`
		TrashedAt time.Time `json:"trashedAt"`
		PurgeAt   time.Time `json:"purgeAt"`
	} `json:"files"`
}

func (a *app) trash(c *http.Client) trashView {
	a.t.Helper()
	status, body := a.get(c, "/trash")
	if status != 200 {
		a.t.Fatalf("trash: %d %s", status, body)
	}
	var out trashView
	if err := json.Unmarshal(body, &out); err != nil {
		a.t.Fatal(err)
	}
	return out
}

func onDisk(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestTrashedFilesAreRestoredOrPurged(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	var root string
	if err := a.pool.QueryRow(ctx, "SELECT root_path FROM libraries WHERE id = $1", lib).Scan(&root); err != nil {
		t.Fatal(err)
	}
	status, up := a.upload(admin, lib, "Kept.pdf", []byte("%PDF-1.4 kept"))
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	fileID, bookID := up["fileId"].(string), up["bookId"].(string)
	var relPath string
	if err := a.pool.QueryRow(ctx, "SELECT rel_path FROM book_files WHERE id = $1", fileID).Scan(&relPath); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, filepath.FromSlash(relPath))
	inTrash := filepath.Join(root, ".trash", fileID, filepath.Base(relPath))
	trash := func(c *http.Client, id string) int {
		status, _, _ := a.call(c, http.MethodPost, "/files/"+id+"/trash", nil)
		return status
	}
	restore := func(c *http.Client, id string) int {
		status, _, _ := a.call(c, http.MethodPost, "/files/"+id+"/restore", nil)
		return status
	}
	bookFiles := func() int {
		_, book, _ := a.call(admin, http.MethodGet, "/books/"+bookID, nil)
		files, _ := book["files"].([]any)
		return len(files)
	}

	// An editor trashes; the file moves aside, leaves its book, and waits.
	if status := trash(reader, fileID); status != 403 {
		t.Errorf("a reader trashes: %d", status)
	}
	if status := trash(editor, fileID); status != 204 {
		t.Fatalf("trash: %d", status)
	}
	if onDisk(original) || !onDisk(inTrash) {
		t.Errorf("after trashing: original %v, in the trash %v", onDisk(original), onDisk(inTrash))
	}
	if n := bookFiles(); n != 0 {
		t.Errorf("the book still shows %d files", n)
	}
	listed := a.trash(editor)
	if len(listed.Files) != 1 || listed.Files[0].ID != fileID || listed.Files[0].TrashedBy != "editor" ||
		listed.Files[0].PurgeAt.Sub(listed.Files[0].TrashedAt) != 30*24*time.Hour {
		t.Errorf("trash: %+v", listed)
	}
	if status := trash(editor, fileID); status != 404 {
		t.Errorf("trash twice: %d", status)
	}

	// A scan passes by the trash, and leaves the record as it is.
	a.scanNow(lib)
	var files, missing int
	if err := a.pool.QueryRow(ctx, "SELECT count(*), count(missing_at) FROM book_files WHERE library_id = $1", lib).Scan(&files, &missing); err != nil {
		t.Fatal(err)
	}
	if files != 1 || missing != 0 {
		t.Errorf("after a scan: %d files, %d missing", files, missing)
	}

	// Restored, it is back where it was and in its book.
	if status := restore(editor, fileID); status != 204 {
		t.Fatalf("restore: %d", status)
	}
	if !onDisk(original) || onDisk(filepath.Dir(inTrash)) || bookFiles() != 1 || len(a.trash(editor).Files) != 0 {
		t.Errorf("after restoring: original %v, trash folder %v, book files %d", onDisk(original), onDisk(filepath.Dir(inTrash)), bookFiles())
	}
	if status := restore(editor, fileID); status != 404 {
		t.Errorf("restore what is not trashed: %d", status)
	}

	// Not over another file that took its place.
	trash(editor, fileID)
	if err := os.WriteFile(original, []byte("another"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status := restore(editor, fileID); status != 409 {
		t.Errorf("restore over another file: %d", status)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}

	// Purged by an administrator, it is gone for good; the book stays.
	if status, _, _ := a.call(editor, http.MethodDelete, "/files/"+fileID, nil); status != 403 {
		t.Errorf("an editor purges: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/files/"+fileID, nil); status != 204 {
		t.Fatalf("purge: %d", status)
	}
	if onDisk(filepath.Dir(inTrash)) || len(a.trash(admin).Files) != 0 {
		t.Errorf("after purging: folder %v", onDisk(filepath.Dir(inTrash)))
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/books/"+bookID, nil); status != 200 {
		t.Errorf("the book after the purge: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/files/"+uuid.NewString(), nil); status != 404 {
		t.Errorf("purge no file: %d", status)
	}

	// The purge job takes what is older than the retention, and only that.
	_, old := a.upload(admin, lib, "Old.pdf", []byte("%PDF-1.4 old"))
	_, fresh := a.upload(admin, lib, "Fresh.pdf", []byte("%PDF-1.4 fresh"))
	for _, f := range []map[string]any{old, fresh} {
		if status := trash(editor, f["fileId"].(string)); status != 204 {
			t.Fatalf("trash: %d", status)
		}
	}
	if _, err := a.pool.Exec(ctx, "UPDATE book_files SET trashed_at = now() - interval '40 days' WHERE id = $1", old["fileId"]); err != nil {
		t.Fatal(err)
	}
	if n, err := a.scans.PurgeExpired(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Errorf("purge expired: %d, %v", n, err)
	}
	if left := a.trash(admin); len(left.Files) != 1 || left.Files[0].ID != fresh["fileId"] {
		t.Errorf("left in the trash: %+v", left)
	}

	// A library GOtome may not write to keeps its files where they are.
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "Fixed.pdf"), []byte("%PDF-1.4 fixed"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := a.externalLibrary(admin, "Fixed", "shared", folder)
	a.scanNow(external)
	var fixed string
	if err := a.pool.QueryRow(ctx, "SELECT id FROM book_files WHERE library_id = $1", external).Scan(&fixed); err != nil {
		t.Fatal(err)
	}
	if status := trash(admin, fixed); status != 409 {
		t.Errorf("trash in a read-only library: %d", status)
	}
}
