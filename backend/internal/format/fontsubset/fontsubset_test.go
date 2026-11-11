package fontsubset

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// letters is a small font: .notdef, space, A, B, a dieresis and an A with a
// dieresis made of the two.
func letters(t *testing.T) []byte {
	glyphs := []testGlyph{
		{box(50, 0, 450, 700), 500},
		{nil, 250},
		{box(40, 0, 560, 700), 600},
		{box(60, 0, 580, 650), 620},
		{box(100, 800, 200, 900), 300},
		{composite([3]int{2, 0, 0}, [3]int{4, 120, 0}), 600},
		{box(10, 10, 90, 90), 400},
	}
	chars := map[rune]uint16{' ': 1, 'A': 2, 'B': 3, 0xA8: 4, 0xC4: 5, 'Z': 6}
	return makeFont(glyphs, chars, 4)
}

func mustParse(t *testing.T, b []byte) *sfnt.Font {
	t.Helper()
	plain, err := ToSFNT(b)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sfnt.Parse(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func glyphCount(t *testing.T, b []byte) int {
	return mustParse(t, b).NumGlyphs()
}

func hasRune(t *testing.T, b []byte, r rune) bool {
	var buf sfnt.Buffer
	g, err := mustParse(t, b).GlyphIndex(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	return g != 0
}

func TestSubsetKeepsTheGlyphsOfTheRunesAndTheirComponents(t *testing.T) {
	font := letters(t)
	out, err := Subset(font, []rune{'A', 0xC4})
	if err != nil {
		t.Fatal(err)
	}
	if got := glyphCount(t, out); got != 4 {
		t.Errorf("%d glyphs, want .notdef, A, the dieresis and A with dieresis", got)
	}
	for _, r := range []rune{'A', 0xC4} {
		if !hasRune(t, out, r) {
			t.Errorf("%q is gone", r)
		}
	}
	for _, r := range []rune{'B', 'Z', ' ', 0xA8} {
		if hasRune(t, out, r) {
			t.Errorf("%q is still there", r)
		}
	}
	if err := Verify(font, out, []rune{'A', 0xC4}); err != nil {
		t.Error(err)
	}
	if len(out) >= len(font) {
		t.Errorf("%d bytes, from %d", len(out), len(font))
	}
}

func TestSubsetOutputIsAFontTheTablesOfWhichAddUp(t *testing.T) {
	out, err := Subset(letters(t), []rune{'A', 'B', ' '})
	if err != nil {
		t.Fatal(err)
	}
	if len(out)%4 != 0 {
		t.Errorf("length %d", len(out))
	}
	if sum := checksum(out); sum != 0xB1B0AFBA {
		t.Errorf("the whole file adds up to %#x", sum)
	}
	tables, _, err := readFont(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"GSUB", "kern", "DSIG"} {
		if _, ok := tables.data[gone]; ok {
			t.Errorf("%s was kept", gone)
		}
	}
	for _, kept := range []string{"head", "cmap", "glyf", "loca", "hmtx", "hhea", "maxp", "post", "name", "OS/2"} {
		if _, ok := tables.data[kept]; !ok {
			t.Errorf("%s is gone", kept)
		}
	}
	if v := big.Uint32(tables.data["post"]); v != 0x00030000 {
		t.Errorf("post version %#x", v)
	}
	recs := out[12:]
	for i := 0; i < len(tables.data); i++ {
		rec := recs[16*i:]
		off, length := big.Uint32(rec[8:]), big.Uint32(rec[12:])
		if tag := string(rec[:4]); tag != "head" && checksum(out[off:off+length]) != big.Uint32(rec[4:]) {
			t.Errorf("checksum of %s", tag)
		}
	}
}

func TestSubsetWithNothingMappedKeepsOnlyNotdef(t *testing.T) {
	out, err := Subset(letters(t), []rune{'€', 'x'})
	if err != nil {
		t.Fatal(err)
	}
	if got := glyphCount(t, out); got != 1 {
		t.Errorf("%d glyphs", got)
	}
}

func bigFont(t *testing.T, n, metrics int) ([]byte, map[rune]uint16) {
	glyphs := []testGlyph{{box(50, 0, 450, 700), 500}}
	chars := map[rune]uint16{}
	for i := 1; i < n; i++ {
		glyphs = append(glyphs, testGlyph{box(i%50, 0, 100+i%300, 700), 400 + i%200})
		r := rune(0x100 + i)
		if i > 1500 {
			r = rune(0x20000 + i)
		}
		chars[r] = uint16(i)
	}
	chars['A'] = 7
	return makeFont(glyphs, chars, metrics), chars
}

func TestSubsetOfALargeFontWithLongLocaAndSharedAdvances(t *testing.T) {
	font, chars := bigFont(t, 4000, 3000)
	if l := big.Uint16(mustTable(t, font, "head")[50:]); l != 1 {
		t.Fatalf("test font should have a long loca, has %d", l)
	}
	keep := []rune{'A', 0x105, 0x7FF, 0x20000 + 3500, 0x20000 + 1600, 0x100 + 3999 - 2500}
	out, err := Subset(font, keep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out)*10 > len(font) {
		t.Errorf("%d bytes from %d", len(out), len(font))
	}
	if err := Verify(font, out, keep); err != nil {
		t.Error(err)
	}
	for r := range chars {
		if want := slicesContains(keep, r); hasRune(t, out, r) != want {
			t.Errorf("%U mapped: %v, wanted %v", r, !want, want)
		}
	}
	if hasRune(t, out, 0x20000+3500) != true {
		t.Error("a character beyond the BMP is gone")
	}
}

func slicesContains(s []rune, r rune) bool {
	for _, x := range s {
		if x == r {
			return true
		}
	}
	return false
}

func mustTable(t *testing.T, font []byte, tag string) []byte {
	tables, _, err := readFont(font)
	if err != nil {
		t.Fatal(err)
	}
	return tables.data[tag]
}

func TestSubsetOfAWOFFIsAWOFF(t *testing.T) {
	font, _ := bigFont(t, 300, 300)
	woff, err := writeWOFF(font)
	if err != nil {
		t.Fatal(err)
	}
	if len(woff) >= len(font) {
		t.Errorf("WOFF %d bytes, sfnt %d", len(woff), len(font))
	}
	out, err := Subset(woff, []rune{'A', 0x105})
	if err != nil {
		t.Fatal(err)
	}
	if string(out[:4]) != "wOFF" {
		t.Errorf("starts with %q", out[:4])
	}
	if err := Verify(woff, out, []rune{'A', 0x105}); err != nil {
		t.Error(err)
	}
	if got := glyphCount(t, out); got != 3 {
		t.Errorf("%d glyphs", got)
	}
}

func TestSubsetLeavesWhatItCannotReadAlone(t *testing.T) {
	cff := append([]byte("OTTO"), make([]byte, 40)...)
	for name, font := range map[string][]byte{
		"CFF":        cff,
		"WOFF2":      append([]byte("wOF2"), make([]byte, 40)...),
		"collection": append([]byte("ttcf"), make([]byte, 40)...),
	} {
		if _, err := Subset(font, []rune{'A'}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, font := range map[string][]byte{"short": {0, 1, 0, 0}, "junk": bytes.Repeat([]byte{7}, 300)} {
		if _, err := Subset(font, []rune{'A'}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// a glyph table that runs past its end
	font := letters(t)
	tables, _, _ := readFont(font)
	tables.data["loca"] = tables.data["loca"][:6]
	if _, err := Subset(writeSFNT(tables.data, tables.flavor), []rune{'A'}); !errors.Is(err, ErrCorrupt) {
		t.Errorf("short loca: %v", err)
	}
}

func TestVerifyCatchesAChangedOutline(t *testing.T) {
	font := letters(t)
	out, err := Subset(font, []rune{'A', 'B'})
	if err != nil {
		t.Fatal(err)
	}
	other := letters(t)
	tables, _, _ := readFont(other)
	glyf := bytes.Clone(tables.data["glyf"])
	glyf[padded(len(box(50, 0, 450, 700)))+19] ^= 0x7F
	tables.data["glyf"] = glyf
	if err := Verify(writeSFNT(tables.data, tables.flavor), out, []rune{'A', 'B'}); err == nil {
		t.Error("an outline that differs passed")
	}
}

func TestSubsetNeverPanicsOnDamagedFonts(t *testing.T) {
	font := letters(t)
	for i := range font {
		for _, b := range []byte{0x00, 0xFF, 0x80} {
			damaged := bytes.Clone(font)
			damaged[i] = b
			_, _ = Subset(damaged, []rune{'A', 0xC4})
		}
	}
	for n := range font {
		_, _ = Subset(font[:n], []rune{'A'})
	}
}
