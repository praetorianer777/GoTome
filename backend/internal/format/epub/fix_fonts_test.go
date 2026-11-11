package epub

import (
	"crypto/sha1"
	"slices"
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"

	"github.com/praetorianer777/gotome/backend/internal/format/fontsubset"
)

const (
	testUUID = "urn:uuid:5e2f0c1a-1111-4222-8333-444455556666"
	// bookText is what the documents of the font books say.
	bookText = "Zwei Äpfel fielen vom Baum."
)

func encryption(alg, name string) string {
	return `<enc:EncryptedData xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
    <enc:EncryptionMethod Algorithm="` + alg + `"/>
    <enc:CipherData><enc:CipherReference URI="` + name + `"/></enc:CipherData>
  </enc:EncryptedData>`
}

// fontEPUB is a book of two documents: one links a style sheet that
// declares the fonts (Serif is fonts/serif.ttf), the other links none.
func fontEPUB(t *testing.T, fonts []entry, encrypted ...string) []byte {
	return fontEPUBWithID(t, testUUID, fonts, encrypted...)
}

func fontEPUBWithID(t *testing.T, id string, fonts []entry, encrypted ...string) []byte {
	var items strings.Builder
	for i, f := range fonts {
		items.WriteString(`<item id="f` + string(rune('a'+i)) + `" href="` + strings.TrimPrefix(f.name, "OEBPS/") + `" media-type="application/x-font-ttf"/>`)
	}
	entries := []entry{
		{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		{"OEBPS/content.opf", strings.Replace(fixPackage(`
    <item id="book" href="text/book.xhtml" media-type="application/xhtml+xml"/>
    <item id="plain" href="text/plain.xhtml" media-type="application/xhtml+xml"/>
    <item id="css" href="style.css" media-type="text/css"/>`+items.String(), `
    <itemref idref="book"/><itemref idref="plain"/>`), testUUID, id, 1)},
		{"OEBPS/nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><nav><a href="text/book.xhtml#c2">Zwei</a></nav></body></html>`},
		{"OEBPS/toc.ncx", `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap/></ncx>`},
		{"OEBPS/text/book.xhtml", xhtmlHead + `<body><h2 id="c2">` + bookText + `</h2></body></html>`},
		{"OEBPS/text/plain.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>x</title></head><body><p>Qx&#xDF;</p></body></html>`},
		{"OEBPS/style.css", `@font-face { font-family: "Serif"; src: url("fonts/serif.ttf") format("truetype") }
@font-face { font-family: "Other"; src: url(fonts/other.ttf) }
body { font-family: Serif, serif }`},
	}
	entries = append(entries, fonts...)
	if len(encrypted) > 0 {
		entries = append(entries, entry{encryptionPath, `<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">` + strings.Join(encrypted, "\n") + `</encryption>`})
	}
	return build(t, entries...)
}

func glyphsIn(t *testing.T, font []byte) (*sfnt.Font, int) {
	t.Helper()
	plain, err := fontsubset.ToSFNT(font)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sfnt.Parse(plain)
	if err != nil {
		t.Fatalf("the font does not parse: %v", err)
	}
	return f, f.NumGlyphs()
}

func mapsRune(t *testing.T, f *sfnt.Font, r rune) bool {
	t.Helper()
	var buf sfnt.Buffer
	g, err := f.GlyphIndex(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	return g != 0
}

func TestFixSubsetsFontsToTheTextOfTheDocumentsThatDeclareThem(t *testing.T) {
	serif, other := fontsubset.Synthetic(6000), fontsubset.Synthetic(3000)
	data := fontEPUB(t, []entry{{"OEBPS/fonts/serif.ttf", string(serif)}, {"OEBPS/fonts/other.ttf", string(other)}})
	written, fixed := fix(t, data, SubsetFonts)
	if fixed.FontsSubset != 2 || fixed.FontsRemoved != 0 {
		t.Fatalf("fixed %+v", fixed)
	}
	for name, original := range map[string][]byte{"OEBPS/fonts/serif.ttf": serif, "OEBPS/fonts/other.ttf": other} {
		out := []byte(entryText(t, written, name))
		f, n := glyphsIn(t, out)
		if len(out)*5 > len(original) {
			t.Errorf("%s: %d bytes from %d", name, len(out), len(original))
		}
		if n < 20 || n > 140 {
			t.Errorf("%s: %d glyphs", name, n)
		}
		for _, r := range bookText {
			if !mapsRune(t, f, r) {
				t.Errorf("%s: %q is gone", name, r)
			}
		}
		if mapsRune(t, f, 0x400+10) {
			t.Errorf("%s keeps a glyph the book does not use", name)
		}
		if err := fontsubset.Verify(original, out, []rune(bookText)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	serifOut, _ := glyphsIn(t, []byte(entryText(t, written, "OEBPS/fonts/serif.ttf")))
	if mapsRune(t, serifOut, 'ß') {
		t.Error("a character only a document that declares no font uses was kept")
	}
	css := entryText(t, written, "OEBPS/style.css")
	opf := entryText(t, written, "OEBPS/content.opf")
	if !strings.Contains(css, "font-face") || !strings.Contains(opf, "fonts/serif.ttf") {
		t.Errorf("the declarations went:\n%s\n%s", css, opf)
	}
	if !slices.Equal(words(parse(t, data)), words(parse(t, written))) {
		t.Error("the text changed")
	}
	if m, _ := Measure(strings.NewReader(string(written)), int64(len(written))); m.Fonts != 2 || m.FontBytes > 200_000 {
		t.Errorf("measured after the fix: %+v", m)
	}
}

func obfuscated(font []byte, key []byte, length int) string {
	return string(xorPrefix(font, key, length))
}

func TestFixSubsetsObfuscatedFontsAndObfuscatesThemAgain(t *testing.T) {
	idpf := sha1.Sum([]byte(testUUID))
	adobe := []byte{0x5e, 0x2f, 0x0c, 0x1a, 0x11, 0x11, 0x42, 0x22, 0x83, 0x33, 0x44, 0x44, 0x55, 0x55, 0x66, 0x66}
	serif, other := fontsubset.Synthetic(4000), fontsubset.Synthetic(2500)
	data := fontEPUB(t, []entry{
		{"OEBPS/fonts/serif.ttf", obfuscated(serif, idpf[:], 1040)},
		{"OEBPS/fonts/other.ttf", obfuscated(other, adobe, 1024)},
	}, encryption(idpfAlgorithm, "OEBPS/fonts/serif.ttf"), encryption(adobeAlgorithm, "OEBPS/fonts/other.ttf"))
	written, fixed := fix(t, data, SubsetFonts)
	if fixed.FontsSubset != 2 {
		t.Fatalf("fixed %+v", fixed)
	}
	for name, c := range map[string]struct {
		key    []byte
		length int
		orig   []byte
	}{
		"OEBPS/fonts/serif.ttf": {idpf[:], 1040, serif},
		"OEBPS/fonts/other.ttf": {adobe, 1024, other},
	} {
		stored := []byte(entryText(t, written, name))
		if fontsubset.IsFont(stored) {
			t.Errorf("%s is no longer obfuscated", name)
		}
		plain := xorPrefix(stored, c.key, c.length)
		f, n := glyphsIn(t, plain)
		if n > 140 || !mapsRune(t, f, 'Ä') {
			t.Errorf("%s: %d glyphs", name, n)
		}
		if err := fontsubset.Verify(c.orig, plain, []rune(bookText)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	enc := entryText(t, written, encryptionPath)
	if !strings.Contains(enc, "fonts/serif.ttf") || !strings.Contains(enc, "fonts/other.ttf") {
		t.Errorf("encryption.xml:\n%s", enc)
	}
}

func TestFixLeavesAFontItCannotSubsetAndRemovesItOnlyWhileTooLarge(t *testing.T) {
	cff := "OTTO" + strings.Repeat("c", 1_100_000)
	data := fontEPUB(t, []entry{
		{"OEBPS/fonts/serif.ttf", string(fontsubset.Synthetic(8000))},
		{"OEBPS/fonts/other.ttf", cff},
	})
	written, fixed := fix(t, data, SubsetFonts)
	if fixed.FontsSubset != 1 || fixed.FontsRemoved != 1 {
		t.Fatalf("fixed %+v", fixed)
	}
	names := map[string]bool{}
	for _, f := range mustZip(t, written).File {
		names[f.Name] = true
	}
	if !names["OEBPS/fonts/serif.ttf"] || names["OEBPS/fonts/other.ttf"] {
		t.Errorf("entries %v", names)
	}
	css, opf := entryText(t, written, "OEBPS/style.css"), entryText(t, written, "OEBPS/content.opf")
	if !strings.Contains(css, `"Serif"`) || strings.Contains(css, "other.ttf") || strings.Contains(opf, "other.ttf") || !strings.Contains(opf, "serif.ttf") {
		t.Errorf("declarations:\n%s\n%s", css, opf)
	}

	small := fontEPUB(t, []entry{
		{"OEBPS/fonts/serif.ttf", string(fontsubset.Synthetic(8000))},
		{"OEBPS/fonts/other.ttf", "OTTO" + strings.Repeat("c", 5000)},
	})
	written, fixed = fix(t, small, SubsetFonts)
	if fixed.FontsSubset != 1 || fixed.FontsRemoved != 0 {
		t.Errorf("a small font that cannot be cut was not left: %+v", fixed)
	}
	if got := entryText(t, written, "OEBPS/fonts/other.ttf"); len(got) != 5004 {
		t.Errorf("the font changed: %d bytes", len(got))
	}
}

func TestFixRemovesWhatStaysOverTheLimitAfterSubsetting(t *testing.T) {
	_, fixed := fix(t, fontBook(t), SubsetFonts)
	if fixed.FontsSubset != 0 || fixed.FontsRemoved != 3 {
		t.Errorf("fixed %+v", fixed)
	}
}

func TestFixLeavesAnObfuscatedFontItCannotUnscramble(t *testing.T) {
	serif := fontsubset.Synthetic(2000)
	data := fontEPUBWithID(t, "urn:isbn:9783161484100", []entry{{"OEBPS/fonts/serif.ttf", string(serif)}},
		encryption(adobeAlgorithm, "OEBPS/fonts/serif.ttf"))
	written, fixed := fix(t, data, SubsetFonts)
	if fixed.FontsSubset != 0 {
		t.Errorf("a font scrambled with a key we do not have was cut: %+v", fixed)
	}
	if got := entryText(t, written, "OEBPS/fonts/serif.ttf"); got != string(serif) {
		t.Error("the font changed")
	}
}

func TestFixSubsetsOpenTypeFontsWithCFFOutlinesToo(t *testing.T) {
	serif, other := fontsubset.SyntheticCFF(5000), fontsubset.SyntheticCFF(2000)
	data := fontEPUB(t, []entry{{"OEBPS/fonts/serif.ttf", string(serif)}, {"OEBPS/fonts/other.ttf", string(other)}})
	written, fixed := fix(t, data, SubsetFonts)
	if fixed.FontsSubset != 2 || fixed.FontsRemoved != 0 {
		t.Fatalf("fixed %+v", fixed)
	}
	for name, original := range map[string][]byte{"OEBPS/fonts/serif.ttf": serif, "OEBPS/fonts/other.ttf": other} {
		out := []byte(entryText(t, written, name))
		f, _ := glyphsIn(t, out)
		if len(out)*5 > len(original) {
			t.Errorf("%s: %d bytes from %d", name, len(out), len(original))
		}
		for _, r := range bookText {
			if !mapsRune(t, f, r) {
				t.Errorf("%s: %q is gone", name, r)
			}
		}
		if err := fontsubset.Verify(original, out, []rune(bookText)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
