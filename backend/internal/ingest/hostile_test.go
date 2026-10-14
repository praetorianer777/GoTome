package ingest

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostilePDF writes a small PDF whose text is far larger than any book's:
// pages that all show one compressed content stream of many lines. Read
// whole, it would hand poppler and GOtome hundreds of megabytes.
func hostilePDF(t *testing.T, pages int) string {
	t.Helper()
	line := "(" + strings.Repeat("W", 200) + ") Tj 0 -10 Td\n"
	var content bytes.Buffer
	zw := zlib.NewWriter(&content)
	fmt.Fprint(zw, "BT /F1 8 Tf 10 4990 Td\n")
	for range 490 {
		fmt.Fprint(zw, line)
	}
	fmt.Fprint(zw, "ET\n")
	zw.Close()

	var buf bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	buf.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, pages)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", i+5)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>")
	obj(fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", content.Len(), content.Bytes()))
	for range pages {
		obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 5000 5000] /Contents 4 0 R /Resources << /Font << /F1 3 0 R >> >> >>")
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	path := filepath.Join(t.TempDir(), "hostile.pdf")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAPDFOfEndlessTextIsUnreadable(t *testing.T) {
	needPoppler(t)
	// Lower than in earnest, so that poppler reaches it in a moment.
	limits := pdfLimits
	pdfLimits.MaxOutput = 4 << 20
	t.Cleanup(func() { pdfLimits = limits })
	path := hostilePDF(t, 100)
	if info, _ := os.Stat(path); info.Size() > 1<<20 {
		t.Fatalf("the PDF is %d bytes", info.Size())
	}
	_, err := safely(context.Background(), "pdf", extractPDF, path)
	if !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), "wrote too much") {
		t.Fatalf("err = %v, want ErrUnreadable as poppler wrote too much", err)
	}
}

// writeZip writes an EPUB-like archive of the entries, in their order.
func writeZip(t *testing.T, entries ...[2]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	container = `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`
	opf       = `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Hostile</dc:title><dc:identifier id="id">x</dc:identifier></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`
)

func TestHostileEPUBsAreUnreadable(t *testing.T) {
	base := [][2]string{{"mimetype", "application/epub+zip"}, {"META-INF/container.xml", container}, {"OEBPS/content.opf", opf}}
	for name, extra := range map[string][2]string{
		"a zip bomb":          {"OEBPS/c.xhtml", "<html><body><p>" + strings.Repeat(" ", 16<<20) + "</p></body></html>"},
		"a name that escapes": {"OEBPS/../../../../etc/cron.d/gotome", "* * * * * root sh"},
		"an absolute name":    {"/etc/passwd", "root::0:0::/:/bin/sh"},
		"a name twice":        {"OEBPS/content.opf", strings.Replace(opf, "Hostile", "Another", 1)},
	} {
		path := writeZip(t, append(base, extra)...)
		_, err := safely(context.Background(), "epub", extractEPUB, path)
		if !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: err = %v, want ErrUnreadable", name, err)
		}
	}
	if _, err := safely(context.Background(), "epub", extractEPUB, writeZip(t, append(base, [2]string{"OEBPS/c.xhtml", "<p>Fine.</p>"})...)); err != nil {
		t.Errorf("the same EPUB without anything hostile: %v", err)
	}
}

func TestAFileThatIsNotItsFormatIsNotParsed(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"page.pdf":   "<html>a download that went wrong</html>",
		"book.epub":  "%PDF-1.4 renamed",
		"book.mobi":  "PK\x03\x04 a zip renamed",
		"song.mp3":   "OggS renamed",
		"book.m4b":   "<html>",
		"track.flac": "",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		format := strings.TrimPrefix(filepath.Ext(name), ".")
		called := false
		_, err := safely(context.Background(), format, func(context.Context, string) (Extracted, error) {
			called = true
			return Extracted{}, nil
		}, path)
		if !errors.Is(err, ErrUnreadable) || !errors.Is(err, ErrWrongContent) || called {
			t.Errorf("%s: err = %v, parser called %v", name, err, called)
		}
	}
}

func TestSniff(t *testing.T) {
	palm := func(kind string) []byte { return append(make([]byte, 60), kind+"\x00\x00"...) }
	for _, tc := range []struct {
		format string
		head   []byte
		want   bool
	}{
		{"epub", []byte("PK\x03\x04\x14\x00"), true},
		{"epub", []byte("%PDF-1.7"), false},
		{"pdf", []byte("%PDF-1.7\n"), true},
		{"pdf", []byte("\xef\xbb\xbfjunk before\n%PDF-1.4"), true},
		{"pdf", []byte("%!PS-Adobe"), false},
		{"mobi", palm("BOOKMOBI"), true},
		{"azw3", palm("BOOKMOBI"), true},
		{"mobi", palm("TEXtREAd"), true},
		{"azw", []byte("TPZ0"), true},
		{"mobi", []byte("TPZ0"), false},
		{"mobi", palm("DATAxxxx"), false},
		{"m4b", []byte("\x00\x00\x00\x20ftypM4B "), true},
		{"m4a", []byte("\x00\x00\x00\x20moov"), false},
		{"mp3", []byte("ID3\x03"), true},
		{"mp3", []byte{0xFF, 0xFB, 0x90}, true},
		{"mp3", []byte("RIFF"), false},
		{"flac", []byte("fLaC"), true},
		{"ogg", []byte("OggS"), true},
		{"opus", []byte("OggS"), true},
		{"opus", []byte("fLaC"), false},
	} {
		if got := sniff(tc.format, tc.head); got != tc.want {
			t.Errorf("sniff(%s, %q) = %v", tc.format, tc.head, got)
		}
	}
}
