//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// heavyEPUB is the bytes of an EPUB with every finding.
func heavyEPUB(t *testing.T, title string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heavy.epub")
	writeEPUBParts(t, path, title, heavyParts(t)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fileState is what the fix tests look at in a file's record.
type fileState struct {
	relPath          string
	sha, original    []byte
	findings         []string
	trashed          bool
	content, chunked bool
}

func (a *app) fileState(id string) fileState {
	a.t.Helper()
	var s fileState
	if err := a.pool.QueryRow(context.Background(), `SELECT rel_path, sha256, original_sha256, ereader_findings,
		trashed_at IS NOT NULL, content_sha256 IS NOT NULL, chunked_at IS NOT NULL FROM book_files WHERE id = $1`, id).
		Scan(&s.relPath, &s.sha, &s.original, &s.findings, &s.trashed, &s.content, &s.chunked); err != nil {
		a.t.Fatal(err)
	}
	return s
}

func TestAFixMakesAnEPUBLighterAndCanBeUndone(t *testing.T) {
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
	original := heavyEPUB(t, "Der Schwalbenturm")
	status, up := a.upload(admin, lib, "Der Schwalbenturm.epub", original)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	fileID, bookID := up["fileId"].(string), up["bookId"].(string)
	a.extract(uuid.MustParse(fileID))
	before := a.fileState(fileID)
	if !slices.Equal(before.findings, epub.Findings) || !before.chunked {
		t.Fatalf("before the fix: %+v", before)
	}

	// Where a reader stands, and a device that has the book.
	if _, err := a.pool.Exec(ctx, `INSERT INTO reading_progress (user_id, book_id, medium, file_id, locator, fraction, client_id)
		SELECT id, $1, 'ebook', $2, 'epubcfi(/6/4!/4/2/1:0)', 0.4, 'web' FROM users WHERE username = 'reader'`, bookID, fileID); err != nil {
		t.Fatal(err)
	}
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	deviceID := made["device"].(map[string]any)["id"].(string)
	if _, err := a.pool.Exec(ctx, `INSERT INTO deliveries (device_id, book_id, delivered_at) VALUES ($1, $2, now())`, deviceID, bookID); err != nil {
		t.Fatal(err)
	}

	fix := func(c *http.Client, id string, findings ...string) (int, map[string]any) {
		t.Helper()
		status, body, _ := a.call(c, http.MethodPost, "/files/"+id+"/fix", map[string]any{"findings": findings})
		return status, body
	}
	if status, _ := fix(reader, fileID, epub.LargeFonts); status != 403 {
		t.Errorf("a reader fixes: %d", status)
	}
	if status, body := fix(editor, fileID, "subset-fonts"); status != 422 {
		t.Errorf("an unknown fix: %d %v", status, body)
	}

	status, body := fix(editor, fileID, epub.LargeFonts)
	if status != 200 || body["fontsRemoved"] != 2.0 || body["documentsSplit"] != 0.0 {
		t.Fatalf("removing the fonts: %d %v", status, body)
	}
	after := a.fileState(fileID)
	if bytes.Equal(after.sha, before.sha) || !bytes.Equal(after.original, before.original) || after.relPath != before.relPath {
		t.Errorf("the fixed file's record: %+v, before %+v", after, before)
	}
	if want := []string{epub.LargeDocument, epub.LargeImage, epub.MissingResources}; !slices.Equal(after.findings, want) {
		t.Errorf("findings after removing the fonts: %v", after.findings)
	}
	path := filepath.Join(root, filepath.FromSlash(before.relPath))
	onDiskNow, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(onDiskNow); !bytes.Equal(sum[:], after.sha) {
		t.Error("the file on disk is not the one recorded")
	}
	m, err := epub.Measure(bytes.NewReader(onDiskNow), int64(len(onDiskNow)))
	if err != nil || m.Fonts != 0 {
		t.Errorf("the file on disk: %+v, %v", m, err)
	}

	// The original lies in the trash, as a file of its own.
	trash := a.trash(editor)
	if len(trash.Files) != 1 || trash.Files[0].Name != "Der Schwalbenturm (original).epub" || trash.Files[0].TrashedBy != "editor" {
		t.Fatalf("the trash: %+v", trash)
	}
	trashedID := trash.Files[0].ID
	trashed := a.fileState(trashedID)
	inTrash, err := os.ReadFile(filepath.Join(root, ".trash", trashedID, "Der Schwalbenturm (original).epub"))
	if err != nil || !bytes.Equal(inTrash, original) || !bytes.Equal(trashed.sha, before.sha) || !slices.Equal(trashed.findings, epub.Findings) {
		t.Errorf("the original in the trash: %+v, %v", trashed, err)
	}

	var locator string
	if err := a.pool.QueryRow(ctx, `SELECT locator FROM reading_progress WHERE book_id = $1`, bookID).Scan(&locator); err != nil || locator != "fraction:0.4" {
		t.Errorf("the place read: %q, %v", locator, err)
	}
	var delivered bool
	if err := a.pool.QueryRow(ctx, `SELECT delivered_at IS NOT NULL FROM deliveries WHERE book_id = $1`, bookID).Scan(&delivered); err != nil || delivered {
		t.Errorf("the device is not sent the book again: delivered %v, %v", delivered, err)
	}
	var queued int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'ingest.extract_file' AND args->>'fileId' = $1`, fileID).Scan(&queued); err != nil || queued != 1 {
		t.Errorf("%d extractions queued: %v", queued, err)
	}
	a.extract(uuid.MustParse(fileID))
	if s := a.fileState(fileID); !s.content || !s.chunked {
		t.Errorf("read again: %+v", s)
	}

	// The rest at once.
	status, body = fix(editor, fileID, epub.LargeDocument, epub.LargeImage, epub.MissingResources)
	if status != 200 || body["documentsSplit"] != 1.0 || body["imagesScaled"] != 1.0 || body["itemsRemoved"] != 1.0 {
		t.Fatalf("the other fixes: %d %v", status, body)
	}
	if s := a.fileState(fileID); s.findings == nil || len(s.findings) != 0 || !bytes.Equal(s.original, before.original) {
		t.Errorf("after every fix: %+v", s)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	book, err := epub.Parse(bytes.NewReader(fixed), int64(len(fixed)))
	if err != nil || len(book.Chapters) < 2 {
		t.Fatalf("the fixed book: %d documents, %v", len(book.Chapters), err)
	}
	if m, _ := epub.Measure(bytes.NewReader(fixed), int64(len(fixed))); m.ImageWidth != 1600 || m.LargestDocument > epub.SplitBytes+4096 {
		t.Errorf("measured: %+v", m)
	}
	if status, body := fix(editor, fileID, epub.LargeFonts); status != 409 {
		t.Errorf("nothing left to fix: %d %v", status, body)
	}
	if trash := a.trash(editor); len(trash.Files) != 2 || trash.Files[0].Name != "Der Schwalbenturm (original 2).epub" {
		t.Errorf("the trash after the second fix: %+v", trash)
	}

	// Undone: the original comes back beside the fixed file.
	if status, _, _ := a.call(editor, http.MethodPost, "/files/"+trashedID+"/restore", nil); status != 204 {
		t.Fatalf("restore: %d", status)
	}
	restored, err := os.ReadFile(filepath.Join(root, filepath.Dir(filepath.FromSlash(before.relPath)), "Der Schwalbenturm (original).epub"))
	if err != nil || !bytes.Equal(restored, original) {
		t.Errorf("the restored original: %v", err)
	}
	_, detail, _ := a.call(editor, http.MethodGet, "/books/"+bookID, nil)
	if files := detail["files"].([]any); len(files) != 2 {
		t.Errorf("the book's files: %v", files)
	}
}

func errorMessage(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	m, _ := e["message"].(string)
	return m
}

func TestAFixIsRefusedWhereItMayNot(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	books := t.TempDir()
	if err := os.WriteFile(filepath.Join(books, "Heavy.epub"), heavyEPUB(t, "Heavy"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := a.externalLibrary(admin, "Mounted", "shared", books)
	hidden := a.library(admin, "Private", "private")
	a.scanNow(external)
	status, up := a.upload(admin, hidden, "Secret.epub", heavyEPUB(t, "Secret"))
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}
	fix := func(id uuid.UUID) (int, map[string]any) {
		t.Helper()
		status, body, _ := a.call(editor, http.MethodPost, "/files/"+id.String()+"/fix", map[string]any{"findings": []string{epub.LargeFonts}})
		return status, body
	}
	if status, body := fix(files["Heavy.epub"].ID); status != 409 || !strings.Contains(errorMessage(body), "read-only") {
		t.Errorf("a read-only library: %d %v", status, body)
	}
	secret := uuid.MustParse(up["fileId"].(string))
	status, body := fix(secret)
	if status != 404 {
		t.Errorf("a file of a library the editor does not see: %d %v", status, body)
	}
	_, other, _ := a.call(editor, http.MethodPost, "/files/"+uuid.NewString()+"/fix", map[string]any{"findings": []string{epub.LargeFonts}})
	if errorMessage(body) != errorMessage(other) {
		t.Errorf("a hidden file is answered unlike one that does not exist: %v, %v", body, other)
	}
}

func TestABulkFixMakesTheFilteredBooksLighter(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := a.library(admin, "Shelf", "shared")
	var ids []string
	for _, title := range []string{"Heavy", "Also heavy"} {
		status, up := a.upload(admin, lib, title+".epub", heavyEPUB(t, title))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		a.extract(uuid.MustParse(up["fileId"].(string)))
		ids = append(ids, up["fileId"].(string))
	}
	status, light := a.upload(admin, lib, "Light.epub", func() []byte {
		path := filepath.Join(t.TempDir(), "light.epub")
		writeEPUBParts(t, path, "Light", epubPart{id: "ch", name: "ch.xhtml", mediaType: "application/xhtml+xml", read: true, content: xhtml("Light", "<p>Kurz.</p>")})
		data, _ := os.ReadFile(path)
		return data
	}())
	if status != 200 {
		t.Fatalf("upload: %d %v", status, light)
	}
	a.extract(uuid.MustParse(light["fileId"].(string)))

	if status, _, _ := a.call(editor, http.MethodPost, "/books/bulk", map[string]any{"action": "fix", "library": lib}); status != 422 {
		t.Errorf("a fix of nothing named: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/books/bulk", map[string]any{"action": "fetch", "findings": []string{"large-fonts"}, "library": lib}); status != 422 {
		t.Errorf("a lookup with findings: %d", status)
	}
	done := a.runBulk(editor, map[string]any{
		"action": "fix", "findings": []string{epub.LargeFonts, epub.LargeImage}, "library": lib,
	})
	if got, want := outcomes(done), map[string]string{"Heavy": "changed", "Also heavy": "changed", "Light": "unchanged"}; !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	for _, id := range ids {
		if s := a.fileState(id); !slices.Equal(s.findings, []string{epub.LargeDocument, epub.MissingResources}) {
			t.Errorf("%s after the bulk fix: %v", id, s.findings)
		}
	}
	var trashed int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM book_files WHERE trashed_at IS NOT NULL`).Scan(&trashed); err != nil || trashed != 2 {
		t.Errorf("%d originals in the trash: %v", trashed, err)
	}
}
