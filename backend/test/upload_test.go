//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// upload sends one file as the web app does and returns the status and the
// decoded body.
func (a *app) upload(c *http.Client, libraryID, name string, content []byte) (int, map[string]any) {
	a.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	w, err := form.CreateFormFile("file", name)
	if err != nil {
		a.t.Fatal(err)
	}
	_, _ = w.Write(content)
	_ = form.Close()
	return a.sendUpload(c, libraryID, form.FormDataContentType(), &body)
}

func (a *app) sendUpload(c *http.Client, libraryID, contentType string, body io.Reader) (int, map[string]any) {
	a.t.Helper()
	req, err := http.NewRequest(http.MethodPost, a.url+httpapi.APIPrefix+"/libraries/"+libraryID+"/uploads", body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		a.t.Fatalf("upload %s: the answer is not JSON: %v", libraryID, err)
	}
	return resp.StatusCode, decoded
}

// libraryFiles lists the files below a library's folder, staged ones
// included, as paths relative to it.
func libraryFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func (a *app) libraryRoot(libraryID string) string {
	a.t.Helper()
	var root string
	if err := a.pool.QueryRow(context.Background(), "SELECT root_path FROM libraries WHERE id = $1", libraryID).Scan(&root); err != nil {
		a.t.Fatal(err)
	}
	return root
}

func TestEditorsUploadAndReadersCannot(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	root := a.libraryRoot(lib)
	emma := filepath.Join(t.TempDir(), "Emma.epub")
	writeEPUB(t, emma, emmaMetadata, nil, "Emma Woodhouse, handsome, clever, and rich.")
	content, err := os.ReadFile(emma)
	if err != nil {
		t.Fatal(err)
	}

	if status, body := a.upload(reader, lib, "Emma.epub", content); status != 403 {
		t.Errorf("a reader uploads: %d %v", status, body)
	}
	if files := libraryFiles(t, root); len(files) != 0 {
		t.Errorf("the refused upload left %v", files)
	}

	status, body := a.upload(editor, lib, "Emma.epub", content)
	if status != 200 || body["outcome"] != "added" || body["title"] != "Emma" || body["libraryId"] != lib {
		t.Fatalf("an editor uploads: %d %v", status, body)
	}
	if files := libraryFiles(t, root); !slices.Equal(files, []string{"Emma/Emma.epub"}) {
		t.Errorf("the library holds %v, want Emma/Emma.epub", files)
	}
	file := a.shelfFiles()["Emma/Emma.epub"]
	if file.ID.String() != body["fileId"] || file.BookID.String() != body["bookId"] || file.State != "pending" {
		t.Errorf("the stored file is %+v, the answer %v", file, body)
	}
	var uploadedBy string
	if err := a.pool.QueryRow(context.Background(), "SELECT uploaded_by::text FROM book_files WHERE id = $1", file.ID).Scan(&uploadedBy); err != nil || uploadedBy != editorID {
		t.Errorf("uploaded by %q (%v), want the editor", uploadedBy, err)
	}

	// The upload queued the file's reading; the book gets what the file says.
	a.workJobs()
	deadline := time.Now().Add(20 * time.Second)
	for a.shelfFiles()["Emma/Emma.epub"].State == "pending" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if book := a.book(file.BookID); len(book.Contributors) == 0 || book.Contributors[0].Name != "Jane Austen" {
		t.Errorf("after reading, the book is %+v", book)
	}

	// A scan finds nothing to do: the upload is the file it would import.
	res, err := a.scans.Run(context.Background(), uuid.MustParse(lib), 0)
	if err != nil || !res {
		t.Fatalf("scan: %v %v", res, err)
	}
	if got := len(a.shelfFiles()); got != 1 {
		t.Errorf("after a scan the library knows %d files, want 1", got)
	}
}

func TestUploadingAFileTwiceFindsTheFirst(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	shared := a.library(admin, "Shared", "shared")
	other := a.library(admin, "Other", "shared")
	private := a.library(admin, "Private", "private")
	content := []byte("%PDF-1.4 a book about whales")

	status, first := a.upload(editor, shared, "Moby Dick.pdf", content)
	if status != 200 || first["outcome"] != "added" {
		t.Fatalf("first upload: %d %v", status, first)
	}
	// The same bytes under another name, into another library.
	status, second := a.upload(editor, other, "whales.pdf", content)
	if status != 200 || second["outcome"] != "duplicate" || second["bookId"] != first["bookId"] ||
		second["libraryId"] != shared || second["title"] != "Moby Dick" || second["fileId"] != nil {
		t.Errorf("second upload: %d %v, want the first book as a duplicate", status, second)
	}
	if files := libraryFiles(t, a.libraryRoot(other)); len(files) != 0 {
		t.Errorf("the duplicate left %v", files)
	}

	// What the uploader may not see does not count, or the answer would tell
	// them what a private library holds.
	status, hidden := a.upload(admin, private, "whales.pdf", []byte("%PDF-1.4 a private book"))
	if status != 200 || hidden["outcome"] != "added" {
		t.Fatalf("admin upload: %d %v", status, hidden)
	}
	status, body := a.upload(editor, shared, "Private.pdf", []byte("%PDF-1.4 a private book"))
	if status != 200 || body["outcome"] != "added" {
		t.Errorf("a file only a private library has: %d %v, want it added", status, body)
	}
	// The editor does not see the private library, so cannot upload into it.
	if status, body := a.upload(editor, private, "x.pdf", []byte("%PDF")); status != 404 {
		t.Errorf("upload into a hidden library: %d %v", status, body)
	}
}

func TestUploadsJoinTheirBooks(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Shelf", "shared")
	root := a.libraryRoot(lib)

	send := func(name, content string) map[string]any {
		t.Helper()
		status, body := a.upload(admin, lib, name, []byte(content))
		if status != 200 || body["outcome"] != "added" {
			t.Fatalf("upload %s: %d %v", name, status, body)
		}
		return body
	}
	epub := send("Emma.epub", "an epub")
	pdf := send("Emma.pdf", "a pdf")
	// Another file under a name that is taken is another book.
	again := send("Emma.epub", "another epub")
	part2 := send("Dune - Part 02.mp3", "second part")
	part1 := send("Dune - Part 01.mp3", "first part")
	odd := send(`../..\evil/..:name?.EPUB`, "a strange name")

	if epub["bookId"] != pdf["bookId"] || again["bookId"] == epub["bookId"] || part1["bookId"] != part2["bookId"] {
		t.Errorf("books: epub %v, pdf %v, again %v, parts %v and %v",
			epub["bookId"], pdf["bookId"], again["bookId"], part1["bookId"], part2["bookId"])
	}
	want := []string{"Dune/Dune - Part 01.mp3", "Dune/Dune - Part 02.mp3", "Emma/Emma (2).epub", "Emma/Emma.epub", "Emma/Emma.pdf", "name/name.epub"}
	if files := libraryFiles(t, root); !slices.Equal(files, want) {
		t.Errorf("the library holds %v, want %v", files, want)
	}
	if odd["title"] != "name" {
		t.Errorf("the strange name became the title %q", odd["title"])
	}
	var parts []int32
	rows, err := a.pool.Query(context.Background(), "SELECT part_index FROM book_files WHERE book_id = $1 ORDER BY rel_path", part1["bookId"])
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p int32
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, p)
	}
	if !slices.Equal(parts, []int32{0, 1}) {
		t.Errorf("parts of Dune are at %v, want 0 and 1 in the order of their names", parts)
	}
}

func TestRefusedUploadsLeaveNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Shelf", "shared")
	root := a.libraryRoot(lib)
	nothingLeft := func(what string) {
		t.Helper()
		// The server may still be tidying up after a request it gave up on.
		deadline := time.Now().Add(5 * time.Second)
		for {
			files := libraryFiles(t, root)
			if len(files) == 0 && len(a.shelfFiles()) == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("%s left %v on disk and %d files in the catalogue", what, files, len(a.shelfFiles()))
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	status, body := a.upload(admin, lib, "notes.txt", []byte("hello"))
	if status != 422 || !strings.Contains(fieldError(body, "file"), "EPUB") {
		t.Errorf("a text file: %d %v", status, body)
	}
	nothingLeft("a text file")

	status, body = a.upload(admin, lib, "Huge.m4b", make([]byte, uploadLimit+1))
	if status != 413 || errorCode(body) != "too_large" {
		t.Errorf("a file over the limit: %d %v", status, body)
	}
	nothingLeft("a file over the limit")

	status, body = a.sendUpload(admin, lib, "application/json", strings.NewReader(`{"file":"x"}`))
	if status != 400 {
		t.Errorf("not a form: %d %v", status, body)
	}

	// The connection breaks halfway through the file.
	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		w, _ := form.CreateFormFile("file", "Cut.epub")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 64<<10))
		pw.CloseWithError(errors.New("the network went away"))
	}()
	req, _ := http.NewRequest(http.MethodPost, a.url+httpapi.APIPrefix+"/libraries/"+lib+"/uploads", pr)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if resp, err := admin.Do(req); err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Errorf("a broken upload was taken: %d", resp.StatusCode)
		}
	}
	nothingLeft("a broken upload")

	books := t.TempDir()
	external := a.externalLibrary(admin, "Outside", "shared", books)
	status, body = a.upload(admin, external, "Emma.epub", []byte("an epub"))
	if status != 409 {
		t.Errorf("into an external library: %d %v", status, body)
	}
	if entries, _ := os.ReadDir(books); len(entries) != 0 {
		t.Errorf("the external folder got %d entries", len(entries))
	}
}
