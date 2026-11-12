//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

func TestAKindleFileIsConvertedToAnEPUBBesideIt(t *testing.T) {
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
	original, err := os.ReadFile("../internal/format/mobi/testdata/persuasion.azw3")
	if err != nil {
		t.Fatal(err)
	}
	status, up := a.upload(admin, lib, "Persuasion.azw3", original)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	fileID, bookID := up["fileId"].(string), up["bookId"].(string)
	a.extract(uuid.MustParse(fileID))
	var rel string
	if err := a.pool.QueryRow(ctx, "SELECT rel_path FROM book_files WHERE id = $1", fileID).Scan(&rel); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rel)))
	convert := func(c *http.Client, id string) (int, map[string]any) {
		t.Helper()
		status, body, _ := a.call(c, http.MethodPost, "/files/"+id+"/convert", nil)
		return status, body
	}

	if status, _ := convert(reader, fileID); status != 403 {
		t.Errorf("a reader converts: %d", status)
	}
	if status, body := convert(editor, uuid.NewString()); status != 404 {
		t.Errorf("a file that is not there: %d %v", status, body)
	}
	status, made := convert(editor, fileID)
	if status != 201 || made["bookId"] != bookID || made["sections"].(float64) < 1 || !strings.HasSuffix(made["name"].(string), ".epub") {
		t.Fatalf("convert: %d %v", status, made)
	}

	got, err := os.ReadFile(filepath.Join(dir, made["name"].(string)))
	if err != nil || len(got) == 0 {
		t.Fatalf("the EPUB is not beside the original: %v", err)
	}
	if book, err := epub.Parse(bytes.NewReader(got), int64(len(got))); err != nil || book.Metadata.Title != "Persuasion" {
		t.Errorf("the EPUB: %v %+v", err, book)
	}
	if now, err := os.ReadFile(filepath.Join(dir, "Persuasion.azw3")); err != nil || !bytes.Equal(now, original) {
		t.Errorf("the original changed: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("a temporary file stays: %s", e.Name())
		}
	}

	// It is a file of the same book, read as any new file is.
	_, book, _ := a.call(reader, http.MethodGet, "/books/"+bookID, nil)
	formats := map[string]bool{}
	for _, f := range book["files"].([]any) {
		formats[f.(map[string]any)["format"].(string)] = true
	}
	if !formats["azw3"] || !formats["epub"] || len(book["files"].([]any)) != 2 {
		t.Errorf("the book's files: %v", book["files"])
	}
	newID := made["fileId"].(string)
	a.extract(uuid.MustParse(newID))
	var state string
	var chunked bool
	if err := a.pool.QueryRow(ctx, "SELECT extract_state, chunked_at IS NOT NULL FROM book_files WHERE id = $1", newID).Scan(&state, &chunked); err != nil || state != "done" {
		t.Errorf("extraction: %q %v %v", state, chunked, err)
	}

	// The same conversion twice is refused, as is a file that is no Kindle file.
	if status, body := convert(editor, fileID); status != 409 {
		t.Errorf("again: %d %v", status, body)
	}
	if status, body := convert(editor, newID); status != 409 {
		t.Errorf("an EPUB: %d %v", status, body)
	}
}

func TestAKindleFileOfAReadOnlyLibraryIsNotConverted(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	original, err := os.ReadFile("../internal/format/mobi/testdata/persuasion.mobi")
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "Persuasion.mobi"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	external := a.externalLibrary(admin, "Mounted", "shared", folder)
	a.scanNow(external)
	id := a.shelfFiles()["Persuasion.mobi"].ID
	status, body, _ := a.call(editor, http.MethodPost, "/files/"+id.String()+"/convert", nil)
	if status != 409 || !strings.Contains(errorMessage(body), "read-only") {
		t.Errorf("a read-only library: %d %v", status, body)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 1 {
		t.Errorf("the folder holds %d entries", len(entries))
	}
}
