//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// epubPart is one file of an EPUB that writeEPUBParts puts together: in the
// manifest under its id, and in the spine when it is read.
type epubPart struct {
	id, name, mediaType, content string
	properties                   string
	read                         bool
}

// writeEPUBParts puts an EPUB 3 at the path with the title and the parts,
// whose names are relative to the package document in OEBPS/. Entries
// named with a leading slash go into the archive at that name and not
// into the manifest.
func writeEPUBParts(t *testing.T, path, title string, parts ...epubPart) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`, zip.Deflate)
	var manifest, spine strings.Builder
	for _, p := range parts {
		if name, ok := strings.CutPrefix(p.name, "/"); ok {
			add(name, p.content, zip.Deflate)
			continue
		}
		props := ""
		if p.properties != "" {
			props = ` properties="` + p.properties + `"`
		}
		fmt.Fprintf(&manifest, "\n    <item id=%q href=%q media-type=%q%s/>", p.id, p.name, p.mediaType, props)
		if p.read {
			fmt.Fprintf(&spine, "\n    <itemref idref=%q/>", p.id)
		}
		if p.content != "" {
			add("OEBPS/"+p.name, p.content, zip.Deflate)
		}
	}
	add("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">urn:uuid:`+fmt.Sprintf("%x", title)+`</dc:identifier>
    <dc:title>`+title+`</dc:title>
    <dc:language>de</dc:language>
    <meta property="dcterms:modified">2026-01-01T00:00:00Z</meta>
  </metadata>
  <manifest>`+manifest.String()+`
  </manifest>
  <spine>`+spine.String()+`
  </spine>
</package>`, zip.Deflate)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func grayPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i % 251)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func xhtml(title, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title>
<link rel="stylesheet" type="text/css" href="style.css"/></head><body>` + body + `</body></html>`
}

// heavyParts are an EPUB with every finding: one chapter of the whole
// novel, two fonts of 1.2 MB, a map of 4.25 megapixels and a picture the
// package lists and the archive lacks.
func heavyParts(t *testing.T) []epubPart {
	var novel strings.Builder
	for ch := 1; ch <= 6; ch++ {
		fmt.Fprintf(&novel, `<h2 id="c%d">Kapitel %d</h2>`, ch, ch)
		for p := 0; p < 120; p++ {
			fmt.Fprintf(&novel, "<p>Ciri ritt durch den Schnee&nbsp;– Absatz %d von Kapitel %d, und die Nacht war lang.</p>\n", p, ch)
		}
	}
	return []epubPart{
		{id: "nav", name: "nav.xhtml", mediaType: "application/xhtml+xml", properties: "nav", content: xhtml("Inhalt",
			`<nav epub:type="toc" xmlns:epub="http://www.idpf.org/2007/ops"><ol><li><a href="tower.xhtml">Der Schwalbenturm</a></li></ol></nav>`)},
		{id: "tower", name: "tower.xhtml", mediaType: "application/xhtml+xml", read: true, content: xhtml("Der Schwalbenturm",
			novel.String()+strings.Repeat("<p>"+strings.Repeat("Schnee ", 60)+"</p>\n", 800)+`<p><img src="map.png" alt="Karte"/></p>`)},
		{id: "css", name: "style.css", mediaType: "text/css", content: `@font-face { font-family: "DejaVu Serif"; src: url(fonts/DejaVuSerif.ttf); }
@font-face { font-family: "DejaVu Sans"; src: url("fonts/DejaVuSans.ttf") format("truetype"); }
body { font-family: "DejaVu Serif"; }`},
		{id: "f1", name: "fonts/DejaVuSerif.ttf", mediaType: "application/vnd.ms-opentype", content: strings.Repeat("serif", 130_000)},
		{id: "f2", name: "fonts/DejaVuSans.ttf", mediaType: "application/vnd.ms-opentype", content: strings.Repeat("sans", 150_000)},
		{id: "map", name: "map.png", mediaType: "image/png", content: grayPNG(t, 2500, 1700)},
		{id: "gone", name: "gone.jpg", mediaType: "image/jpeg"},
	}
}

func TestEPUBsSlowOnAnEReaderAreFound(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books, private := t.TempDir(), t.TempDir()
	writeEPUBParts(t, filepath.Join(books, "Schwalbenturm.epub"), "Der Schwalbenturm", heavyParts(t)...)
	writeEPUBParts(t, filepath.Join(books, "Light.epub"), "Light",
		epubPart{id: "ch", name: "ch.xhtml", mediaType: "application/xhtml+xml", read: true, content: xhtml("Light", "<p>Kurz&nbsp;und gut.</p>")})
	writeEPUBParts(t, filepath.Join(private, "Secret.epub"), "Secret", heavyParts(t)...)
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	hidden := a.externalLibrary(admin, "Private", "private", private)
	a.scanNow(lib)
	a.scanNow(hidden)
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}

	findings := func(c *http.Client, name string) map[string]any {
		t.Helper()
		status, body, _ := a.call(c, http.MethodGet, "/books/"+files[name].BookID.String(), nil)
		if status != 200 {
			t.Fatalf("%s: %d %v", name, status, body)
		}
		file := body["files"].([]any)[0].(map[string]any)
		check, _ := file["ereader"].(map[string]any)
		return check
	}
	heavy := findings(reader, "Schwalbenturm.epub")
	if got := heavy["findings"]; fmt.Sprint(got) != fmt.Sprint([]any{epub.LargeDocument, epub.LargeFonts, epub.LargeImage, epub.MissingResources}) {
		t.Errorf("findings of the heavy book: %v", heavy)
	}
	if heavy["fonts"] != 2.0 || heavy["fontBytes"] != 1_250_000.0 || heavy["imageWidth"] != 2500.0 || heavy["imageHeight"] != 1700.0 ||
		heavy["largestDocument"].(float64) < epub.MaxDocumentBytes || fmt.Sprint(heavy["missingResources"]) != "[OEBPS/gone.jpg]" {
		t.Errorf("measures of the heavy book: %v", heavy)
	}
	if light := findings(reader, "Light.epub"); light == nil || len(light["findings"].([]any)) != 0 {
		t.Errorf("the light book: %v", light)
	}

	for filter, want := range map[string][]string{
		`{"field": "ereader", "op": "in", "values": ["large-fonts"]}`:                {"Der Schwalbenturm"},
		`{"field": "ereader", "op": "in", "values": ["large-image", "large-fonts"]}`: {"Der Schwalbenturm"},
		`{"not": {"field": "ereader", "op": "in", "values": ["large-document"]}}`:    {"Light"},
	} {
		got, _ := a.listAll(reader, url.Values{"filter": {filter}})
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s\n got %v\nwant %v", filter, got, want)
		}
	}
	if got, _ := a.listAll(admin, url.Values{"filter": {`{"field": "ereader", "op": "in", "values": ["large-image"]}`}}); len(got) != 2 {
		t.Errorf("the administrator's heavy books: %v", got)
	}
	status, body, _ := a.call(reader, http.MethodGet, "/books?"+url.Values{"filter": {`{"field": "ereader", "op": "in", "values": ["slow"]}`}}.Encode(), nil)
	if status != 422 || !strings.Contains(fieldError(body, "filter"), "not a finding") {
		t.Errorf("an unknown finding: %d %v", status, body)
	}

	_, body, _ = a.call(reader, http.MethodGet, "/books/facets", nil)
	counts, _ := facetCounts(t, body)
	if want := map[string]int{"large-document": 1, "large-fonts": 1, "large-image": 1, "missing-resources": 1}; !mapsEqualInt(counts["ereader"], want) {
		t.Errorf("the reader's facet: %v", counts["ereader"])
	}
	for c, want := range map[*http.Client]float64{reader: 1, admin: 2} {
		if _, body, _ := a.call(c, http.MethodGet, "/statistics", nil); body["withEreaderFindings"] != want {
			t.Errorf("statistics: %v, want %v", body["withEreaderFindings"], want)
		}
	}

	// Files read before measuring was are measured by the job, and one
	// that cannot be is recorded as such.
	if err := os.WriteFile(filepath.Join(books, "Broken.epub"), []byte("no zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.scanNow(lib)
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET epub_measures = NULL, ereader_findings = NULL`); err != nil {
		t.Fatal(err)
	}
	if n, err := a.scans.MeasureFiles(ctx); err != nil || n != 4 {
		t.Fatalf("measured %d files: %v", n, err)
	}
	if n, err := a.scans.MeasureFiles(ctx); err != nil || n != 0 {
		t.Errorf("measured %d files again: %v", n, err)
	}
	var broken []string
	var measured bool
	if err := a.pool.QueryRow(ctx, `SELECT ereader_findings, epub_measures IS NOT NULL FROM book_files WHERE rel_path = 'Broken.epub'`).Scan(&broken, &measured); err != nil {
		t.Fatal(err)
	}
	if broken == nil || len(broken) != 0 || measured {
		t.Errorf("the broken file: findings %v, measured %v", broken, measured)
	}
	if again := findings(reader, "Schwalbenturm.epub"); fmt.Sprint(again) != fmt.Sprint(heavy) {
		t.Errorf("measured by the job: %v, at extraction: %v", again, heavy)
	}

	// New bytes are measured again.
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET sha256 = sha256('changed'::bytea) WHERE rel_path = 'Light.epub'`); err != nil {
		t.Fatal(err)
	}
	if light := findings(reader, "Light.epub"); light != nil {
		t.Errorf("a file whose bytes changed keeps its measures: %v", light)
	}
}
