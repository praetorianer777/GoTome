package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testOPF = `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">urn:test:%d</dc:identifier>
    <dc:title>Book %d</dc:title>
    <dc:title>A subtitle</dc:title>
  </metadata>
</package>`

func testEPUB(t *testing.T, id int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mimetype, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(mimetype, "application/epub+zip")
	for name, content := range map[string]string{
		containerPath:       `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`,
		"OEBPS/content.opf": fmt.Sprintf(testOPF, id, id),
		"OEBPS/ch1.xhtml":   "<html><body><p>Text of the book.</p></body></html>",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// harvestServer lists three books on each of two pages and serves them, with
// one entry that is not an EPUB at all.
func harvestServer(t *testing.T, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robot/harvest", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("filetypes[]") != "epub.noimages" || r.URL.Query().Get("langs[]") != "de" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		first := 1
		if r.URL.Query().Get("offset") == "3" {
			first = 4
		}
		for id := first; id < first+3; id++ {
			fmt.Fprintf(w, `<p><a href="http://%s/cache/epub/%d/pg%d.epub">book</a></p>`, r.Host, id, id)
		}
		if first == 1 {
			fmt.Fprint(w, `<p><a href="harvest?offset=3&amp;filetypes[]=epub.noimages&amp;langs[]=de">Next Page</a></p>`)
		}
	})
	mux.HandleFunc("/cache/epub/", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("User-Agent") != userAgent {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		var id int
		fmt.Sscanf(filepath.Base(r.URL.Path), "pg%d.epub", &id)
		if id == 2 {
			fmt.Fprint(w, "<html>mirror error</html>")
			return
		}
		w.Write(testEPUB(t, id))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchFillsQuotaAcrossPagesAndResumes(t *testing.T) {
	var requests atomic.Int64
	srv := harvestServer(t, &requests)
	dir := t.TempDir()
	f := &Fetcher{Dir: dir, Harvest: srv.URL + "/robot/harvest", Client: srv.Client(), Log: io.Discard}

	if err := f.Fetch(context.Background(), []Quota{{Lang: "de", Count: 4}}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// Book 2 is the mirror's error page, so the fourth book comes from page two.
	want := []string{"pg1.epub", "pg3.epub", "pg4.epub", "pg5.epub"}
	if got := names(t, filepath.Join(dir, "de")); !equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}

	before := requests.Load()
	if err := f.Fetch(context.Background(), []Quota{{Lang: "de", Count: 4}}); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if after := requests.Load(); after != before {
		t.Errorf("a full corpus cost %d more requests", after-before)
	}
}

func TestFetchTriesAFailedHarvestPageAgain(t *testing.T) {
	var requests, failures atomic.Int64
	srv := harvestServer(t, &requests)
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "harvest") && failures.Add(1) <= 2 {
			http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
			return
		}
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(flaky.Close)
	dir := t.TempDir()
	f := &Fetcher{Dir: dir, Harvest: flaky.URL + "/robot/harvest", Client: flaky.Client(), Log: io.Discard, Delay: time.Millisecond}

	if err := f.Fetch(context.Background(), []Quota{{Lang: "de", Count: 2}}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := len(names(t, filepath.Join(dir, "de"))); got != 2 {
		t.Errorf("fetched %d books, want 2", got)
	}
}

func TestFetchStopsWhenTheHarvestEnds(t *testing.T) {
	var requests atomic.Int64
	srv := harvestServer(t, &requests)
	dir := t.TempDir()
	f := &Fetcher{Dir: dir, Harvest: srv.URL + "/robot/harvest", Client: srv.Client(), Log: io.Discard}

	if err := f.Fetch(context.Background(), []Quota{{Lang: "de", Count: 50}}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := len(names(t, filepath.Join(dir, "de"))); got != 5 {
		t.Errorf("fetched %d books, want the 5 the harvest has", got)
	}
}

func TestHarvestLinksUseTheHostTheCertificateNames(t *testing.T) {
	page := `<a href="https://aleph.gutenberg.org/cache/epub/50/pg50.epub">a</a>
<a href="https://example.org/cache/epub/51/pg51.epub">b</a>`
	files, _, err := parseHarvest(HarvestURL+"?langs[]=de", []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://aleph.pglaf.org/cache/epub/50/pg50.epub", "https://example.org/cache/epub/51/pg51.epub"}
	if len(files) != 2 || files[0].String() != want[0] || files[1].String() != want[1] {
		t.Errorf("files = %v, want %v", files, want)
	}
}

func TestFetchGivesUpOnAMirrorThatFailsEveryDownload(t *testing.T) {
	var downloads atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/robot/harvest", func(w http.ResponseWriter, r *http.Request) {
		for id := 1; id <= 3*maxFailuresInARow; id++ {
			fmt.Fprintf(w, `<p><a href="http://%s/cache/epub/%d/pg%d.epub">book</a></p>`, r.Host, id, id)
		}
	})
	mux.HandleFunc("/cache/epub/", func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f := &Fetcher{Dir: t.TempDir(), Harvest: srv.URL + "/robot/harvest", Client: srv.Client(), Log: io.Discard}

	err := f.Fetch(context.Background(), []Quota{{Lang: "de", Count: 5}})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Fetch = %v, want an error naming the mirror's answer", err)
	}
	if got := downloads.Load(); got != maxFailuresInARow {
		t.Errorf("tried %d downloads, want it to stop at %d", got, maxFailuresInARow)
	}
}

func TestParseLanguages(t *testing.T) {
	got, err := ParseLanguages("en:1200, de:600")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Quota{"en", 1200}) || got[1] != (Quota{"de", 600}) {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"", "en", "en:0", "en:x", "EN:5", "en:5,en:6", "../x:5"} {
		if _, err := ParseLanguages(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestReplicateMakesDistinctValidBooks(t *testing.T) {
	base, out := t.TempDir(), t.TempDir()
	for lang, ids := range map[string][]int{"de": {1, 2}, "en": {3}} {
		os.MkdirAll(filepath.Join(base, lang), 0o755)
		for _, id := range ids {
			os.WriteFile(filepath.Join(base, lang, fmt.Sprintf("pg%d.epub", id)), testEPUB(t, id), 0o644)
		}
	}

	n, err := Replicate(context.Background(), base, out, 7, io.Discard)
	if err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	if n != 7 {
		t.Fatalf("wrote %d books, want 7", n)
	}
	files, _ := listEPUBs(out)
	if len(files) != 7 {
		t.Fatalf("%d files on disk, want 7", len(files))
	}

	titles := map[string]bool{}
	for _, file := range files {
		zr, err := zip.OpenReader(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if first := zr.File[0]; first.Name != "mimetype" || first.Method != zip.Store {
			t.Errorf("%s: first entry is %s (method %d), want a stored mimetype", file, first.Name, first.Method)
		}
		opfPath, err := packagePath(&zr.Reader)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, entry := range zr.File {
			if entry.Name != opfPath {
				continue
			}
			opf, _ := readEntry(entry)
			title := string(titleElement.FindSubmatch(opf)[2])
			if !strings.Contains(title, "(copy ") {
				t.Errorf("%s: title %q carries no copy number", file, title)
			}
			if titles[title] {
				t.Errorf("title %q appears twice", title)
			}
			titles[title] = true
			if strings.Count(string(opf), "(copy ") != 1 {
				t.Errorf("%s: more than the first title was changed", file)
			}
			if !strings.Contains(string(identifierElement.FindSubmatch(opf)[2]), "-copy-") {
				t.Errorf("%s: identifier was not changed", file)
			}
		}
		zr.Close()
	}

	stats, err := collectStats(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Lang != "de" || stats[0].Books != 5 || stats[1].Books != 2 {
		t.Errorf("stats = %+v, want de:5 and en:2", stats)
	}
}

func TestReplicateNeedsABaseCorpus(t *testing.T) {
	if _, err := Replicate(context.Background(), t.TempDir(), t.TempDir(), 10, io.Discard); err == nil {
		t.Error("an empty base corpus was accepted")
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func equal(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}
