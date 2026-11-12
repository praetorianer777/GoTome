//go:build integration

package test

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestALibraryLeftOutOfFullTextSearchAddsNothingToTheIndex(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	filler := strings.Repeat("Nothing much happened in the town that week, and the weather was mild. ", 40)
	meta := func(title string) string {
		return `<dc:identifier id="uid">urn:uuid:` + title + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator opf:role="aut">Anna Reiter</dc:creator>
    <dc:language>en</dc:language>`
	}
	openDir, quietDir := t.TempDir(), t.TempDir()
	writeEPUB(t, filepath.Join(openDir, "Open.epub"), meta("Open"), nil, filler, "A lighthouse stood on the cliff. "+filler)
	writeEPUB(t, filepath.Join(quietDir, "Quiet.epub"), meta("Quiet"), nil, filler, "A lighthouse stood on the shore. "+filler)
	open := a.externalLibrary(admin, "Open shelf", "shared", openDir)
	quiet := a.externalLibrary(admin, "Quiet shelf", "shared", quietDir)

	if status, _, _ := a.call(admin, "PATCH", "/libraries/"+quiet, map[string]any{"fulltext": false}); status != 200 {
		t.Fatalf("switch off: %d", status)
	}
	a.scanNow(open)
	a.scanNow(quiet)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	a.drain(a.text)

	quietFile := a.countRows("SELECT count(*) FROM book_chunks WHERE library_id = $1", uuid.MustParse(quiet))
	if quietFile == 0 {
		t.Fatal("a library left out of the index must still have its chunks")
	}
	if n := a.countRows("SELECT count(*) FROM book_files WHERE library_id = $1 AND indexed_at IS NOT NULL", uuid.MustParse(quiet)); n != 0 {
		t.Errorf("%d files of the library left out are recorded as indexed", n)
	}
	if got := indexFinds(t, a.text, uuid.MustParse(quiet), "lighthouse"); len(got) != 0 {
		t.Errorf("the index holds %v of the library left out", got)
	}

	status, got := a.searchText(admin, url.Values{"q": {"lighthouse"}})
	if status != 200 || strings.Join(titles(got), ",") != "Open" {
		t.Fatalf("search: %d %v", status, titles(got))
	}
	if len(got.NotSearched) != 1 || got.NotSearched[0].ID != quiet || got.NotSearched[0].Name != "Quiet shelf" {
		t.Errorf("the answer names %v as not searched", got.NotSearched)
	}
	status, got = a.searchText(admin, url.Values{"q": {"lighthouse"}, "library": {quiet}})
	if status != 200 || len(got.Books) != 0 || len(got.NotSearched) != 1 {
		t.Errorf("searching the library left out: %d %v %v", status, titles(got), got.NotSearched)
	}
	_, got = a.searchText(admin, url.Values{"q": {"lighthouse"}, "library": {open}})
	if len(got.NotSearched) != 0 {
		t.Errorf("searching only the open library names %v", got.NotSearched)
	}

	if status, st := a.searchStatus(admin, ""); status != 200 || st["files"] != 1.0 || st["indexed"] != 1.0 || st["notSearched"] != 1.0 {
		t.Errorf("status %d %v", status, st)
	}
	if status, st := a.searchStatus(admin, "?library="+quiet); status != 200 || st["files"] != 1.0 || st["indexed"] != 0.0 {
		t.Errorf("status of the library left out %d %v", status, st)
	}

	// Turning it on reads the files for the index.
	if status, body, _ := a.call(admin, "PATCH", "/libraries/"+quiet, map[string]any{"fulltext": true}); status != 200 || body["fulltext"] != true {
		t.Fatalf("switch on: %d %v", status, body)
	}
	if more, err := a.scans.IndexFiles(ctx, time.Now().Add(time.Minute)); err != nil || more {
		t.Fatalf("index files: %v, %v", more, err)
	}
	status, got = a.searchText(admin, url.Values{"q": {"lighthouse"}})
	if status != 200 || len(got.Books) != 2 || len(got.NotSearched) != 0 {
		t.Fatalf("after switching on: %d %v %v", status, titles(got), got.NotSearched)
	}

	// And off again takes its entries out of the index.
	if status, _, _ := a.call(admin, "PATCH", "/libraries/"+quiet, map[string]any{"fulltext": false}); status != 200 {
		t.Fatalf("switch off again: %d", status)
	}
	a.drain(a.text)
	if got := indexFinds(t, a.text, uuid.MustParse(quiet), "lighthouse"); len(got) != 0 {
		t.Errorf("the index still holds %v of the library", got)
	}
	if got := indexFinds(t, a.text, uuid.MustParse(open), "lighthouse"); len(got) == 0 {
		t.Error("the other library lost its entries")
	}
	if n := a.countRows("SELECT count(*) FROM book_chunks WHERE library_id = $1", uuid.MustParse(quiet)); n != quietFile {
		t.Errorf("%d chunks of the library, were %d: they are not the index's to take", n, quietFile)
	}
	if n := a.countRows("SELECT count(*) FROM book_files WHERE library_id = $1 AND indexed_at IS NOT NULL", uuid.MustParse(quiet)); n != 0 {
		t.Errorf("%d files still recorded as indexed", n)
	}
}
