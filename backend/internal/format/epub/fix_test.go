package epub

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"
)

// fixPackage is a package document with the items and itemrefs given.
func fixPackage(items, spine string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">urn:uuid:5e2f0c1a-1111-4222-8333-444455556666</dc:identifier>
    <dc:title>Der Schwalbenturm</dc:title>
    <dc:language>de</dc:language>
    <meta property="dcterms:modified">2026-01-01T00:00:00Z</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + items + `
  </manifest>
  <spine toc="ncx">` + spine + `
  </spine>
  <guide><reference type="text" title="Start" href="text/book.xhtml#c2"/></guide>
</package>`
}

const xhtmlHead = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Der Schwalbenturm</title>
<link rel="stylesheet" type="text/css" href="../style.css"/></head>
`

// novel is a body of chapters, each with a heading of id cN and paragraphs
// of about perChapter bytes, inside a wrapper as calibre writes it.
func novel(chapters, perChapter int) string {
	var b strings.Builder
	b.WriteString(`<body class="calibre"><div class="calibre1" id="top">`)
	for c := 1; c <= chapters; c++ {
		fmt.Fprintf(&b, "\n<h2 id=\"c%d\">Kapitel %d</h2>", c, c)
		if c == 1 {
			b.WriteString(`<p>Siehe <a href="#c7">Kapitel 7</a>.</p>`)
		}
		for n := 0; n*90 < perChapter; n++ {
			fmt.Fprintf(&b, "\n<p class=\"p\">Ciri&nbsp;ritt durch den Schnee, Absatz %d von Kapitel %d, die Nacht war lang.</p>", n, c)
		}
	}
	b.WriteString("\n</div></body></html>")
	return b.String()
}

func largeBook(t *testing.T, body string) []byte {
	return build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="end" href="text/end.xhtml" media-type="application/xhtml+xml"/>
    <item id="css" href="style.css" media-type="text/css"/>`, `
    <itemref idref="book"/>
    <itemref idref="end" linear="yes"/>`)},
		entry{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="toc"><ol>
<li><a href="text/book.xhtml">Anfang</a></li>
<li><a href="text/book.xhtml#c3">Kapitel 3</a></li>
<li><a href='text/book.xhtml#c8'>Kapitel 8</a></li>
<li><a href="text/end.xhtml">Ende</a></li>
</ol></nav></body></html>`},
		entry{"OEBPS/toc.ncx", `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint id="p1"><navLabel><text>Kapitel 1</text></navLabel><content src="text/book.xhtml#c1"/></navPoint>
<navPoint id="p6"><navLabel><text>Kapitel 6</text></navLabel><content src="text/book.xhtml#c6"/></navPoint>
</navMap></ncx>`},
		entry{"OEBPS/text/book.xhtml", xhtmlHead + body},
		entry{"OEBPS/text/end.xhtml", xhtmlHead + `<body><p>Zur <a href="book.xhtml#c5">Mitte</a>.</p></body></html>`},
		entry{"OEBPS/style.css", "p { margin: 0 }"},
	)
}

func fix(t *testing.T, data []byte, findings ...string) ([]byte, Fixed) {
	t.Helper()
	var out bytes.Buffer
	fixed, err := Fix(bytes.NewReader(data), int64(len(data)), &out, findings, modified)
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	checkStructure(t, out.Bytes())
	return out.Bytes(), fixed
}

// entryText is an entry of a written EPUB.
func entryText(t *testing.T, data []byte, name string) string {
	t.Helper()
	b, err := newArchive(mustZip(t, data), DefaultLimits).read(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// target follows a reference from the document at doc to the entry that
// holds its fragment, and checks that the entry has the id.
func target(t *testing.T, data []byte, doc, href string) string {
	t.Helper()
	name, err := resolve(doc, href)
	if err != nil {
		t.Fatal(err)
	}
	_, fragment, _ := strings.Cut(href, "#")
	if !strings.Contains(entryText(t, data, name), `id="`+fragment+`"`) {
		t.Errorf("%s in %s leads to %s, which has no id %s", href, doc, name, fragment)
	}
	return name
}

func words(b *Book) []string { return strings.Fields(b.Text()) }

func wellFormed(t *testing.T, name, doc string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(doc))
	d.Entity = xml.HTMLEntity
	for {
		if _, err := d.Token(); errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			t.Errorf("%s is not well formed: %v", name, err)
			return
		}
	}
}

func TestFixSplitsALargeDocumentAtItsChapters(t *testing.T) {
	original := largeBook(t, novel(8, 60_000))
	written, fixed := fix(t, original, LargeDocument)
	if fixed.DocumentsSplit != 1 || fixed.PartsAdded < 1 {
		t.Fatalf("fixed %+v", fixed)
	}
	before, after := parse(t, original), parse(t, written)
	if !slices.Equal(words(before), words(after)) {
		t.Error("the text changed")
	}
	if len(after.Chapters) != len(before.Chapters)+fixed.PartsAdded {
		t.Errorf("%d documents read, want %d", len(after.Chapters), len(before.Chapters)+fixed.PartsAdded)
	}
	parts := 0
	for _, ch := range after.Chapters {
		if !strings.HasPrefix(ch.Href, "OEBPS/text/book") {
			continue
		}
		parts++
		doc := entryText(t, written, ch.Href)
		wellFormed(t, ch.Href, doc)
		if len(doc) > SplitBytes+2048 {
			t.Errorf("%s holds %d bytes", ch.Href, len(doc))
		}
		if !strings.Contains(doc, "<!DOCTYPE html") || !strings.Contains(doc, `<div class="calibre1" id="top">`) {
			t.Errorf("%s lost its head or its wrapper", ch.Href)
		}
		// Every part but the first begins with a chapter.
		if body := doc[strings.Index(doc, `id="top">`)+len(`id="top">`):]; parts > 1 && !strings.HasPrefix(strings.TrimSpace(body), "<h2") {
			t.Errorf("%s begins with %.40q", ch.Href, strings.TrimSpace(body))
		}
		if ch.Href != "OEBPS/text/book.xhtml" && !strings.HasPrefix(ch.Href, "OEBPS/text/book-gotome-") {
			t.Errorf("a part is named %s", ch.Href)
		}
	}
	if parts != fixed.PartsAdded+1 || after.Chapters[parts].Href != "OEBPS/text/end.xhtml" {
		t.Errorf("%d parts, then %s", parts, after.Chapters[parts].Href)
	}
	if got := after.Chapters[0].Title; got != "Anfang" {
		t.Errorf("the first part's title is %q", got)
	}

	nav := entryText(t, written, "OEBPS/nav.xhtml")
	for _, want := range []string{"c3", "c8"} {
		i := strings.Index(nav, "#"+want)
		start := strings.LastIndexAny(nav[:i], `"'`) + 1
		target(t, written, "OEBPS/nav.xhtml", nav[start:i+len(want)+1])
	}
	if !strings.Contains(nav, `href="text/book.xhtml">Anfang`) {
		t.Error("the link to the document's start moved")
	}
	ncx := entryText(t, written, "OEBPS/toc.ncx")
	if target(t, written, "OEBPS/toc.ncx", between(ncx, `<content src="`, `#c6"`)+"#c6") == "OEBPS/text/book.xhtml" {
		t.Error("chapter 6 is still in the first part")
	}
	target(t, written, "OEBPS/text/end.xhtml", between(entryText(t, written, "OEBPS/text/end.xhtml"), `<a href="`, `#c5"`)+"#c5")
	opf := entryText(t, written, "OEBPS/content.opf")
	target(t, written, "OEBPS/content.opf", between(opf, `title="Start" href="`, `#c2"`)+"#c2")
	first := entryText(t, written, "OEBPS/text/book.xhtml")
	target(t, written, "OEBPS/text/book.xhtml", between(first, `Siehe <a href="`, `#c7"`)+"#c7")
	if !strings.Contains(opf, `linear="yes"`) || strings.Count(opf, "<itemref") != fixed.PartsAdded+2 {
		t.Errorf("the spine:\n%s", opf)
	}

	m, err := Measure(bytes.NewReader(written), int64(len(written)))
	if err != nil || slices.Contains(m.Findings(), LargeDocument) {
		t.Errorf("measured after the fix: %+v, %v", m, err)
	}
}

// between is the text of s between the last before and the first after it.
func between(s, before, after string) string {
	end := strings.Index(s, after)
	if end < 0 {
		return ""
	}
	return s[strings.LastIndex(s[:end], before)+len(before) : end]
}

func TestFixSplitsADocumentWithoutChaptersBetweenItsParagraphs(t *testing.T) {
	var body strings.Builder
	body.WriteString("<body>")
	for n := 0; n < 9000; n++ {
		fmt.Fprintf(&body, "<p>Absatz %d, und es schneite weiter über Kaer Morhen.</p>\n", n)
	}
	body.WriteString("</body></html>")
	original := largeBook(t, body.String())
	written, fixed := fix(t, original, LargeDocument)
	if fixed.DocumentsSplit != 1 || fixed.PartsAdded < 1 {
		t.Fatalf("fixed %+v", fixed)
	}
	if !slices.Equal(words(parse(t, original)), words(parse(t, written))) {
		t.Error("the text changed")
	}
	for _, ch := range parse(t, written).Chapters {
		if doc := entryText(t, written, ch.Href); len(doc) > SplitBytes+1024 {
			t.Errorf("%s holds %d bytes", ch.Href, len(doc))
		}
	}
}

func TestFixLeavesADocumentItCannotReadAlone(t *testing.T) {
	broken := largeBook(t, "<body>"+strings.Repeat("<p>offen", 40_000)+"</body></html>")
	_, fixed := fix(t, broken, LargeDocument)
	if fixed.Any() {
		t.Errorf("fixed %+v", fixed)
	}
}

func fontBook(t *testing.T) []byte {
	return build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="css" href="style.css" media-type="text/css"/>
    <item id="f1" href="fonts/DejaVuSerif.ttf" media-type="application/vnd.ms-opentype"/>
    <item id="f2" href="fonts/DejaVuSans-Bold.otf" media-type="font/otf"/>`, `
    <itemref idref="book"/>`)},
		entry{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav><a href="text/book.xhtml#c2">Zwei</a></nav></body></html>`},
		entry{"OEBPS/toc.ncx", `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap/></ncx>`},
		entry{"OEBPS/text/book.xhtml", xhtmlHead + `<head><style type="text/css">
@font-face { font-family: "Inline"; src: url(../fonts/DejaVuSans-Bold.otf) }
h2 { font-weight: bold }
</style></head><body><h2 id="c2">Zwei</h2><p>Text.</p></body></html>`},
		entry{"OEBPS/style.css", `@font-face {
  font-family: "DejaVu Serif";
  src: url("fonts/DejaVuSerif.ttf");
}
@FONT-FACE { font-family: "DejaVu Sans"; font-weight: bold; src: url(fonts/DejaVuSans-Bold.otf) format("opentype") }
body { font-family: "DejaVu Serif", serif }`},
		entry{"OEBPS/fonts/DejaVuSerif.ttf", strings.Repeat("s", 700_000)},
		entry{"OEBPS/fonts/DejaVuSans-Bold.otf", strings.Repeat("b", 600_000)},
		entry{"OEBPS/fonts/stray.woff", "unlisted"},
		entry{encryptionPath, `<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/fonts/DejaVuSerif.ttf"/></enc:CipherData>
  </enc:EncryptedData>
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/fonts/DejaVuSans-Bold.otf"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`},
	)
}

func TestFixRemovesEmbeddedFonts(t *testing.T) {
	original := fontBook(t)
	written, fixed := fix(t, original, LargeFonts)
	if fixed.FontsRemoved != 3 {
		t.Errorf("fixed %+v", fixed)
	}
	for _, f := range mustZip(t, written).File {
		if isFont("", f.Name) || f.Name == encryptionPath {
			t.Errorf("%s is still there", f.Name)
		}
	}
	css := entryText(t, written, "OEBPS/style.css")
	if strings.Contains(strings.ToLower(css), "font-face") || !strings.Contains(css, `body { font-family: "DejaVu Serif", serif }`) {
		t.Errorf("the style sheet:\n%s", css)
	}
	if doc := entryText(t, written, "OEBPS/text/book.xhtml"); strings.Contains(doc, "@font-face") || !strings.Contains(doc, "h2 { font-weight: bold }") {
		t.Errorf("the document's style:\n%s", doc)
	}
	if opf := entryText(t, written, "OEBPS/content.opf"); strings.Contains(opf, "fonts/") {
		t.Errorf("the manifest:\n%s", opf)
	}
	if !slices.Equal(words(parse(t, original)), words(parse(t, written))) {
		t.Error("the text changed")
	}
	if m, _ := Measure(bytes.NewReader(written), int64(len(written))); m.Fonts != 0 {
		t.Errorf("measured after the fix: %+v", m)
	}
}

func TestFixKeepsEncryptionOfWhatStays(t *testing.T) {
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="f1" href="fonts/a.ttf" media-type="font/ttf"/>
    <item id="pic" href="pic.svg" media-type="image/svg+xml"/>`, `
    <itemref idref="book"/>`)},
		entry{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav/></body></html>`},
		entry{"OEBPS/toc.ncx", `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap/></ncx>`},
		entry{"OEBPS/text/book.xhtml", xhtmlHead + `<body><p id="c2">Text.</p></body></html>`},
		entry{"OEBPS/fonts/a.ttf", "font"},
		entry{"OEBPS/pic.svg", "<svg/>"},
		entry{encryptionPath, `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#"><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/><CipherData><CipherReference URI="OEBPS/fonts/a.ttf"/></CipherData></EncryptedData>
<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#"><EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/><CipherData><CipherReference URI="OEBPS/pic.svg"/></CipherData></EncryptedData>
</encryption>`},
	)
	written, _ := fix(t, data, LargeFonts)
	enc := entryText(t, written, encryptionPath)
	if strings.Contains(enc, "a.ttf") || !strings.Contains(enc, "pic.svg") {
		t.Errorf("encryption.xml:\n%s", enc)
	}
}

// imageOf makes an image of a pattern, or of noise, which a JPEG cannot
// make small.
func imageOf(t *testing.T, format string, w, h int, noisy bool) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	noise := uint32(1)
	for i := 0; i < len(img.Pix); i += 4 {
		x, y := (i/4)%w, (i/4)/w
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(x), uint8(y), uint8(x^y), 255
		if noisy {
			noise = noise*1664525 + 1013904223
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(noise>>24), uint8(noise>>16), uint8(noise>>8)
		}
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100})
	case "gif":
		err = gif.Encode(&buf, image.NewPaletted(img.Rect, color.Palette{color.Black, color.White}), nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestFixScalesImagesDown(t *testing.T) {
	smallButHeavy := imageOf(t, "jpeg", 760, 760, true)
	if len(smallButHeavy) <= MaxImageBytes {
		t.Fatalf("the heavy JPEG holds only %d bytes", len(smallButHeavy))
	}
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="map" href="images/map.png" media-type="image/png"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="heavy" href="images/heavy.jpeg" media-type="image/jpeg"/>
    <item id="small" href="images/small.png" media-type="image/png"/>
    <item id="anim" href="images/anim.gif" media-type="image/gif"/>`, `
    <itemref idref="book"/>`)},
		entry{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav/></body></html>`},
		entry{"OEBPS/toc.ncx", `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap/></ncx>`},
		entry{"OEBPS/text/book.xhtml", xhtmlHead + `<body><p id="c2"><img src="../images/map.png" alt="Karte"/></p></body></html>`},
		entry{"OEBPS/images/map.png", imageOf(t, "png", 1800, 100, false)},
		entry{"OEBPS/images/cover.jpg", imageOf(t, "jpeg", 100, 1800, false)},
		entry{"OEBPS/images/heavy.jpeg", smallButHeavy},
		entry{"OEBPS/images/small.png", imageOf(t, "png", 300, 200, false)},
		entry{"OEBPS/images/anim.gif", imageOf(t, "gif", 1700, 20, false)},
	)
	written, fixed := fix(t, data, LargeImage)
	if fixed.ImagesScaled != 3 {
		t.Errorf("fixed %+v", fixed)
	}
	for name, want := range map[string]string{
		"OEBPS/images/map.png": "png 1600x88", "OEBPS/images/cover.jpg": "jpeg 88x1600",
		"OEBPS/images/heavy.jpeg": "jpeg 760x760", "OEBPS/images/small.png": "png 300x200", "OEBPS/images/anim.gif": "gif 1700x20",
	} {
		img := entryText(t, written, name)
		cfg, format, err := image.DecodeConfig(strings.NewReader(img))
		if got := fmt.Sprintf("%s %dx%d", format, cfg.Width, cfg.Height); err != nil || got != want {
			t.Errorf("%s: %s, %v; want %s", name, got, err, want)
		}
	}
	if heavy := entryText(t, written, "OEBPS/images/heavy.jpeg"); len(heavy) >= len(smallButHeavy) {
		t.Errorf("the heavy JPEG holds %d bytes, before %d", len(heavy), len(smallButHeavy))
	}
	if small := entryText(t, written, "OEBPS/images/small.png"); small != imageOf(t, "png", 300, 200, false) {
		t.Error("the small image changed")
	}
	if parse(t, written).Cover == nil {
		t.Error("the cover is gone")
	}
}

func TestFixRepairsAManifestThatListsMissingFiles(t *testing.T) {
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="lost" href="text/lost.xhtml" media-type="application/xhtml+xml"/>
    <item id="gone" href="images/gone.jpg" media-type="image/jpeg"/>`, `
    <itemref idref="book"/>
    <itemref idref="lost"/>`)},
		entry{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav/></body></html>`},
		entry{"OEBPS/toc.ncx", `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap/></ncx>`},
		entry{"OEBPS/text/book.xhtml", xhtmlHead + `<body><p id="c2">Text.</p></body></html>`},
	)
	written, fixed := fix(t, data, MissingResources)
	if fixed.ItemsRemoved != 2 {
		t.Errorf("fixed %+v", fixed)
	}
	opf := entryText(t, written, "OEBPS/content.opf")
	if strings.Contains(opf, "lost") || strings.Contains(opf, "gone") || !strings.Contains(opf, `<spine toc="ncx">`) {
		t.Errorf("the package:\n%s", opf)
	}
	if m, _ := Measure(bytes.NewReader(written), int64(len(written))); len(m.MissingResources) != 0 {
		t.Errorf("measured after the fix: %+v", m)
	}
}

func TestFixDoesAllAtOnceAndNothingWhereNothingIsWrong(t *testing.T) {
	written, fixed := fix(t, largeBook(t, novel(2, 1000)), Findings...)
	if fixed.Any() {
		t.Errorf("fixed %+v in a book that is fine", fixed)
	}
	checkStructure(t, written)

	_, fixed = fix(t, fontBook(t), Findings...)
	if fixed.FontsRemoved != 3 || fixed.DocumentsSplit+fixed.ImagesScaled+fixed.ItemsRemoved != 0 {
		t.Errorf("fixed %+v", fixed)
	}
}

func TestFixRefuses(t *testing.T) {
	encrypted := build(t,
		container("p.opf"),
		entry{"p.opf", fixPackage("", "")},
		entry{encryptionPath, `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
			<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
			<EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`},
	)
	if _, err := Fix(bytes.NewReader(encrypted), int64(len(encrypted)), io.Discard, []string{LargeFonts}, modified); !errors.Is(err, ErrNotWritable) {
		t.Errorf("an encrypted EPUB: %v", err)
	}
	if _, err := Fix(bytes.NewReader([]byte("no zip")), 6, io.Discard, []string{LargeFonts}, modified); !errors.Is(err, ErrNotEPUB) {
		t.Errorf("no zip: %v", err)
	}
	if _, err := Fix(bytes.NewReader(encrypted), int64(len(encrypted)), io.Discard, []string{"subset-fonts"}, modified); err == nil {
		t.Error("an unknown fix was taken")
	}
}
