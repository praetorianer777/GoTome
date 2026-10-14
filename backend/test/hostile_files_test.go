//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withEntry copies the archive at path with one more entry, written as
// given whatever its name.
func withEntry(t *testing.T, path, name, content string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	zr.Close()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(content))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Files built to hurt their reader cost a scan those files, recorded as
// unreadable, and nothing else.
func TestHostileFilesAreRecordedAsUnreadable(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Bomb.epub"), emmaMetadata, nil, strings.Repeat(" ", 64<<20))
	escape := filepath.Join(books, "Escape.epub")
	writeEPUB(t, escape, emmaMetadata, nil, "Emma Woodhouse, handsome, clever, and rich.")
	withEntry(t, escape, "../../../escaped.txt", "out")
	twice := filepath.Join(books, "Twice.epub")
	writeEPUB(t, twice, emmaMetadata, nil, "Emma Woodhouse, handsome, clever, and rich.")
	withEntry(t, twice, "OEBPS/ch0.xhtml", "<html><body><p>Something else.</p></body></html>")
	os.WriteFile(filepath.Join(books, "Saved page.pdf"), []byte("<html><body>Not found</body></html>"), 0o644)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse, handsome, clever, and rich.")
	if info, _ := os.Stat(filepath.Join(books, "Bomb.epub")); info.Size() > 1<<20 {
		t.Fatalf("the bomb is %d bytes", info.Size())
	}

	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	want := map[string]string{
		"Bomb.epub":      "exceeds a limit",
		"Escape.epub":    "leaves the archive",
		"Twice.epub":     "is there twice",
		"Saved page.pdf": "it is not a PDF",
	}
	for path, f := range a.shelfFiles() {
		reason, hostile := want[path]
		switch {
		case !hostile && f.State != "done":
			t.Errorf("%s: %s %v, want it read", path, f.State, f.Error)
		case hostile && (f.State != "failed" || f.Error == nil || !strings.Contains(*f.Error, reason)):
			t.Errorf("%s: %s %v, want it failed as %q", path, f.State, f.Error, reason)
		}
	}
	if status, body, _ := a.call(admin, http.MethodGet, "/books", nil); status != 200 || len(body["books"].([]any)) != 5 {
		t.Errorf("the books after the scan: %d %v", status, body)
	}
}
