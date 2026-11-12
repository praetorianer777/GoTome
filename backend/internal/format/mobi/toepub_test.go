package mobi

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

var converted = time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)

func toEPUB(t *testing.T, data []byte) ([]byte, Converted) {
	t.Helper()
	var out bytes.Buffer
	res, err := ToEPUB(bytes.NewReader(data), int64(len(data)), &out, converted)
	if err != nil {
		t.Fatalf("ToEPUB: %v", err)
	}
	return out.Bytes(), res
}

// opens checks that the EPUB reads and has nothing an e-reader would
// stumble on, and returns it as read.
func opens(t *testing.T, data []byte) *epub.Book {
	t.Helper()
	book, err := epub.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("epub.Parse: %v", err)
	}
	m, err := epub.Measure(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("epub.Measure: %v", err)
	}
	if f := m.Findings(); len(f) != 0 {
		t.Errorf("findings: %v (%+v)", f, m)
	}
	return book
}

func sameWords(t *testing.T, book *Book, converted *epub.Book) {
	t.Helper()
	if got, want := strings.Fields(converted.Text()), strings.Fields(book.Text); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the text changed:\n%q\nwant\n%q", strings.Join(got, " "), strings.Join(want, " "))
	}
}

func TestToEPUBKeepsTheTextTheMetadataAndTheCover(t *testing.T) {
	f := persuasionFixture(t)
	data := f.build(t)
	out, res := toEPUB(t, data)
	book := opens(t, out)
	sameWords(t, parse(t, data), book)

	m := book.Metadata
	if m.Title != "Persuasion" || strings.Join(m.Authors(), ",") != "Jane Austen" || m.Language != "en" || m.Publisher != "Penguin" {
		t.Errorf("metadata = %+v", m)
	}
	if m.Description != "Her last novel." || len(m.Subjects) != 2 {
		t.Errorf("description %q, subjects %v", m.Description, m.Subjects)
	}
	if book.Cover == nil || book.Cover.MediaType != "image/jpeg" || !bytes.Equal(book.Cover.Data, f.cover) {
		t.Errorf("cover = %+v", book.Cover)
	}
	if res.Sections != 1 || res.TOCEntries != 2 || res.Images != 1 {
		t.Errorf("result = %+v", res)
	}
	again, _ := toEPUB(t, data)
	if !bytes.Equal(out, again) {
		t.Error("the same book gave other bytes")
	}
	parsed := parse(t, data)
	if parsed.ContentHash == nil {
		t.Error("Parse lost the content hash")
	}
}

// chapters makes n chapters of about 3 KB, each a heading and paragraphs.
func chapters(n int, extra func(i int) string) string {
	var b strings.Builder
	b.WriteString("<html><head></head><body>")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "<h1>Kapitel %d</h1>", i)
		b.WriteString(extra(i))
		for p := 0; p < 12; p++ {
			fmt.Fprintf(&b, "<p>Ciri ritt durch den Schnee, Absatz %d von Kapitel %d, und die Nacht war lang &amp; kalt.</p>", p, i)
			for k := 0; k < 3; k++ {
				b.WriteString("<p>Noch ein Satz, damit die Kapitel eine Weile dauern, mit Worten und Kommata.</p>")
			}
		}
		if i < n {
			b.WriteString("<mbp:pagebreak/>")
		}
	}
	b.WriteString("</body></html>")
	return b.String()
}

func TestToEPUBCutsAtChaptersAndKeepsLinks(t *testing.T) {
	const marker = "FILEPOS00"
	markup := chapters(3, func(i int) string {
		if i == 1 {
			return `<p>Weiter zu <a filepos="` + marker + `">Kapitel 3</a> oder <a href="kindle:pos:fid:0001:off:0000000000">ins Ungewisse</a>.</p>`
		}
		return ""
	})
	markup = strings.Replace(markup, marker, fmt.Sprintf("%010d", strings.Index(strings.Replace(markup, marker, "0000000000", 1), "<h1>Kapitel 3")), 1)
	f := fixture{fullName: "Der Schwalbenturm", exth: []exthRecord{text(exthTitle, "Der Schwalbenturm"), text(exthLanguage, "de")}, markup: markup, compression: compressionPalmDOC}
	data := f.build(t)
	out, res := toEPUB(t, data)
	book := opens(t, out)
	sameWords(t, parse(t, data), book)
	if res.Sections != 3 || res.TOCEntries != 3 || res.LinksDropped != 1 {
		t.Fatalf("result = %+v", res)
	}
	if len(book.Chapters) != 3 {
		t.Fatalf("%d chapters", len(book.Chapters))
	}
	first := entry(t, out, "OEBPS/section-0001.xhtml")
	href := regexp.MustCompile(`<a href="(section-0003\.xhtml#filepos-\d+)">Kapitel 3</a>`).FindStringSubmatch(first)
	if href == nil {
		t.Fatalf("the link to chapter 3 is not in section 1:\n%s", first[:600])
	}
	third := entry(t, out, "OEBPS/section-0003.xhtml")
	_, id, _ := strings.Cut(href[1], "#")
	if !strings.Contains(third, `id="`+id+`"`) {
		t.Errorf("the link leads to %s, which section 3 lacks", id)
	}
	if !strings.Contains(first, "ins Ungewisse") || strings.Contains(first, "kindle:") {
		t.Errorf("a link into a KF8 file is not left as text")
	}
	nav := entry(t, out, "OEBPS/nav.xhtml")
	for _, want := range []string{`<a href="section-0001.xhtml#toc-1">Kapitel 1</a>`, `section-0003.xhtml#toc-3`} {
		if !strings.Contains(nav, want) {
			t.Errorf("the table of contents lacks %s:\n%s", want, nav)
		}
	}
}

func TestToEPUBWritesTheImagesTheTextShows(t *testing.T) {
	pic := coverJPEG(t)
	f := fixture{
		fullName: "Bilder", exth: []exthRecord{text(exthTitle, "Bilder")}, compression: compressionPalmDOC,
		markup: `<html><body><h1>Bilder</h1><p>Vor dem Bild.</p><img recindex="00002" alt="Ein &amp; Bild"/><p>Nach dem Bild.</p><img recindex="00009"/></body></html>`,
		cover:  pic, images: [][]byte{pic},
	}
	out, res := toEPUB(t, f.build(t))
	opens(t, out)
	doc := entry(t, out, "OEBPS/section-0001.xhtml")
	m := regexp.MustCompile(`<img src="images/(image-\d+\.jpg)" alt="Ein &amp; Bild"/>`).FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("the picture is not in the document:\n%s", doc)
	}
	if strings.Contains(doc, "image-0009") || res.Images != 2 {
		t.Errorf("a record that holds no picture was written: %+v", res)
	}
	if entry(t, out, "OEBPS/images/"+m[1]) != string(pic) {
		t.Error("the picture's bytes changed")
	}
}

func TestToEPUBCutsALongTextWithoutBreaks(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body><div>")
	for i := 0; i < 9000; i++ {
		fmt.Fprintf(&b, "<p>Absatz %d: der Turm stand still im Nebel, und die Schwalben kamen nicht zurueck.</p>", i)
	}
	b.WriteString("</div></body></html>")
	f := fixture{fullName: "Lang", exth: []exthRecord{text(exthTitle, "Lang")}, markup: b.String(), compression: compressionPalmDOC}
	data := f.build(t)
	out, res := toEPUB(t, data)
	book := opens(t, out)
	sameWords(t, parse(t, data), book)
	if res.Sections < 3 {
		t.Errorf("%d sections for %d bytes", res.Sections, b.Len())
	}
}

func TestToEPUBFollowsLinksInAFileInWindows1252(t *testing.T) {
	const marker = "FILEPOS00"
	head := "<html><body><p>Gr\u00fc\u00dfe aus dem Turm, \u00fcber die \u00c4cker. <a filepos=\"" + marker + "\">Zum Ende</a></p>"
	tail := "<p>Das Ende \u00fcberm\u00e4\u00dfig sch\u00f6n.</p>"
	markup := head + `<a id="ende"></a>` + tail + "</body></html>"
	position := len(toCP1252(t, strings.Replace(head, marker, "0000000000", 1)))
	markup = strings.Replace(markup, marker, fmt.Sprintf("%010d", position), 1)
	f := fixture{fullName: "Gr\u00fc\u00dfe", exth: []exthRecord{text(exthTitle, "Gr\u00fc\u00dfe")}, markup: markup, compression: compressionPalmDOC, encoding: 1252}
	data := f.build(t)
	out, _ := toEPUB(t, data)
	book := opens(t, out)
	sameWords(t, parse(t, data), book)
	doc := entry(t, out, "OEBPS/section-0001.xhtml")
	if !strings.Contains(doc, `<a href="#filepos-`+fmt.Sprint(position)+`">Zum Ende</a>`) || !strings.Contains(doc, `<a id="filepos-`+fmt.Sprint(position)+`"></a>`) {
		t.Errorf("the link and its place:\n%s", doc)
	}
}

func TestToEPUBOfTheFixtureFiles(t *testing.T) {
	for _, name := range []string{"persuasion.mobi", "persuasion.azw3"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			out, _ := toEPUB(t, data)
			book := opens(t, out)
			sameWords(t, parse(t, data), book)
		})
	}
}

func TestToEPUBRefusesWhatItCannotRead(t *testing.T) {
	protected := persuasionFixture(t)
	protected.encryption = 2
	data := protected.build(t)
	if _, err := ToEPUB(bytes.NewReader(data), int64(len(data)), &bytes.Buffer{}, converted); err != ErrProtected {
		t.Errorf("a protected book: %v", err)
	}
	if _, err := ToEPUB(strings.NewReader("not a book"), 10, &bytes.Buffer{}, converted); err == nil {
		t.Error("a text was converted")
	}
}

func entry(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zipReader(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			var b bytes.Buffer
			b.ReadFrom(rc)
			return b.String()
		}
	}
	t.Fatalf("no entry %s", name)
	return ""
}

func zipReader(data []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}
