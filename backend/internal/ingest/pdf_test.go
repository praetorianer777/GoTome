package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func needPoppler(t *testing.T) {
	t.Helper()
	for _, program := range []string{pdfinfo, pdftotext, pdftoppm} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not installed here; the toolchain image has it", program)
		}
	}
}

// writePDF writes a PDF with one page per content stream, and an Info
// dictionary with the entries given. A page may show an image instead of
// text, as a scan does.
func writePDF(t *testing.T, info map[string]string, pages ...string) string {
	t.Helper()
	var objects []string
	add := func(o string) int { objects = append(objects, o); return len(objects) }
	catalog := add("")
	tree := add("")
	font := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	// A grey square, eight pixels a side, for pages that are pictures.
	pixels := strings.Repeat("\x80", 8*8)
	picture := add(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream", len(pixels), pixels))
	var kids []string
	for _, content := range pages {
		stream := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		page := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 420 595] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> /XObject << /Im1 %d 0 R >> >> >>", tree, stream, font, picture))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	objects[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", tree)
	objects[tree-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids))
	var entries []string
	for k, v := range info {
		entries = append(entries, fmt.Sprintf("/%s (%s)", k, v))
	}
	infoObj := add("<< " + strings.Join(entries, " ") + " >>")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, catalog, infoObj, xref)
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func textPage(lines ...string) string {
	var b strings.Builder
	b.WriteString("BT /F1 11 Tf 40 540 Td 14 TL")
	for _, line := range lines {
		fmt.Fprintf(&b, " (%s) Tj T*", line)
	}
	b.WriteString(" ET")
	return b.String()
}

const scannedPage = "q 420 0 0 595 0 0 cm /Im1 Do Q"

func TestPDFWithText(t *testing.T) {
	needPoppler(t)
	path := writePDF(t, map[string]string{
		"Title": "Persuasion", "Author": "Jane Austen; Deirdre Le Faye", "Keywords": "Fiction, Classics",
	},
		textPage("Sir Walter Elliot, of Kellynch Hall, in Somersetshire,", "was a man who, for his own amusement,"),
		textPage("never took up any book but the Baronetage."),
	)
	got, err := extractPDF(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.Title != "Persuasion" || len(got.Metadata.Contributors) != 2 ||
		got.Metadata.Contributors[1].Name != "Deirdre Le Faye" || strings.Join(got.Metadata.Tags, "|") != "Fiction|Classics" {
		t.Errorf("metadata = %+v", got.Metadata)
	}
	if got.Pages == nil || *got.Pages != 2 || got.PagesEstimated || !got.HasText {
		t.Errorf("pages %v (estimated %v), text %v; want 2 counted pages with text", got.Pages, got.PagesEstimated, got.HasText)
	}
	if len(got.Sections) != 2 || got.Sections[1].Label != "2" ||
		got.Sections[1].Text != "never took up any book but the Baronetage." ||
		!strings.Contains(got.Sections[0].Text, "Kellynch Hall") {
		t.Errorf("sections = %q", got.Sections)
	}
	cover, err := png.Decode(bytes.NewReader(got.Cover))
	if err != nil || cover.Bounds().Dx() != 1000 {
		t.Errorf("cover: %v, %v; want the first page 1000 pixels wide", err, cover)
	}
}

func TestPDFWithoutText(t *testing.T) {
	needPoppler(t)
	// A scan, with only a stamp for text on its first page.
	path := writePDF(t, map[string]string{"Title": "Scan0001.pdf"},
		scannedPage+" "+textPage("Library copy"), scannedPage, scannedPage)
	got, err := extractPDF(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasText || got.Sections != nil || got.Pages == nil || *got.Pages != 3 || got.Cover == nil {
		t.Errorf("a scan: text %v, sections %d, pages %v, cover %d bytes; want no text, 3 pages and a cover",
			got.HasText, len(got.Sections), got.Pages, len(got.Cover))
	}
	if got.Metadata.Title != "" {
		t.Errorf("the title %q is the name of a file, not of a book", got.Metadata.Title)
	}
}

func TestPDFThatIsNone(t *testing.T) {
	needPoppler(t)
	path := filepath.Join(t.TempDir(), "fake.pdf")
	os.WriteFile(path, []byte("<html>a download that went wrong</html>"), 0o644)
	_, err := extractPDF(context.Background(), path)
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v, want ErrUnreadable", err)
	}
}

// fakePoppler puts a script in the place of pdfinfo for the test.
func fakePoppler(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdfinfo")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	was, limits := pdfinfo, pdfLimits
	pdfinfo = path
	// Long enough for a shell to start on a machine busy compiling.
	pdfLimits.Timeout = 10 * time.Second
	t.Cleanup(func() { pdfinfo, pdfLimits = was, limits })
}

func TestPDFThatMakesPopplerHang(t *testing.T) {
	fakePoppler(t, "sleep 60")
	pdfLimits.Timeout = 500 * time.Millisecond
	started := time.Now()
	_, err := extractPDF(context.Background(), writePDF(t, nil, textPage("x")))
	if !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), "took too long") {
		t.Fatalf("err = %v, want the file to be unreadable because poppler hung", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("gave up after %s", took)
	}
}

func TestPDFThatMakesPopplerCrash(t *testing.T) {
	fakePoppler(t, "kill -SEGV $$")
	_, err := extractPDF(context.Background(), writePDF(t, nil, textPage("x")))
	if !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), "SIGSEGV") && !strings.Contains(err.Error(), "segmentation") {
		t.Fatalf("err = %v, want the file to be unreadable because poppler crashed", err)
	}
}

func TestPDFLockedWithAPassword(t *testing.T) {
	fakePoppler(t, "echo 'Command Line Error: Incorrect password' >&2; exit 1")
	got, err := extractPDF(context.Background(), writePDF(t, nil, textPage("x")))
	if err != nil || !got.DRM {
		t.Fatalf("got %+v, %v; want it catalogued as locked", got, err)
	}
}
