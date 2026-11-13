package fontsubset

import (
	"bytes"
	"errors"
	"testing"
)

// cffLetters has a glyph using a global and a local subroutine, subroutines
// nobody calls, and an Aacute made by endchar from A and acute.
func cffLetters() []byte {
	lsubrs := [][]byte{
		cffCode(300, csHLineTo, 500, csVLineTo, -300, csHLineTo, csReturn),
		cffCode(1, csHLineTo, csReturn),
	}
	gsubrs := [][]byte{
		cffCode(5, csHLineTo, csReturn),
		cffCode(100, csHLineTo, 100, csVLineTo, -100, csHLineTo, csReturn),
	}
	glyphs := []cffGlyph{
		{code: []byte{csEndChar}},
		{code: cffBox(40, 520, 700), sid: 34},
		{code: cffBox(100, 100, 100), sid: 125},
		{code: cffCode(50, 700, 65, 194, csEndChar), sid: 391},
		{code: cffCode(10, 0, csRMoveTo, -107, csCallSubr, csEndChar), sid: 35},
		{code: cffCode(0, 0, csRMoveTo, -106, csCallGSubr, csEndChar), sid: 59},
	}
	chars := map[rune]uint16{'A': 1, 0xB4: 2, 0xC1: 3, 'B': 4, 'Z': 5}
	return makeOTTO(buildCFF(glyphs, []string{"Aacute"}, gsubrs, lsubrs), len(glyphs), chars)
}

func cffOf(t *testing.T, font []byte) *cffFont {
	t.Helper()
	tables, _, err := readFont(font)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parseCFF(tables.data["CFF "])
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCFFSubsetKeepsGlyphNumbersAndEmptiesTheOthers(t *testing.T) {
	font := cffLetters()
	out, err := Subset(font, []rune{'B'})
	if err != nil {
		t.Fatal(err)
	}
	if got := glyphCount(t, out); got != 5 {
		t.Errorf("%d glyphs", got)
	}
	f := cffOf(t, out)
	for g, cs := range f.charstrings {
		if empty := bytes.Equal(cs, []byte{14}); empty != (g != 4) {
			t.Errorf("glyph %d empty: %v", g, empty)
		}
	}
	if !bytes.Equal(f.privates[0].subrs[1], []byte{11}) || bytes.Equal(f.privates[0].subrs[0], []byte{11}) {
		t.Errorf("local subroutines: %x", f.privates[0].subrs)
	}
	if !bytes.Equal(f.gsubrs[0], []byte{11}) || !bytes.Equal(f.gsubrs[1], []byte{11}) {
		t.Errorf("global subroutines no kept glyph calls were kept: %x", f.gsubrs)
	}
	if !hasRune(t, out, 'B') || hasRune(t, out, 'A') {
		t.Error("the character map is not B alone")
	}
	if err := Verify(font, out, []rune{'B'}); err != nil {
		t.Error(err)
	}
	if sum := checksum(out); sum != 0xB1B0AFBA {
		t.Errorf("checksum %#x", sum)
	}
	if len(out) >= len(font) {
		t.Errorf("%d bytes from %d", len(out), len(font))
	}
}

func TestCFFSubsetFollowsGlobalSubroutines(t *testing.T) {
	font := cffLetters()
	out, err := Subset(font, []rune{'Z'})
	if err != nil {
		t.Fatal(err)
	}
	f := cffOf(t, out)
	if !bytes.Equal(f.gsubrs[0], []byte{11}) || bytes.Equal(f.gsubrs[1], []byte{11}) {
		t.Errorf("global subroutines: %x", f.gsubrs)
	}
	if !bytes.Equal(f.privates[0].subrs[0], []byte{11}) {
		t.Error("an unused local subroutine was kept")
	}
	if err := Verify(font, out, []rune{'Z'}); err != nil {
		t.Error(err)
	}
}

func TestCFFSubsetKeepsTheBaseAndAccentOfAnAccentedGlyph(t *testing.T) {
	font := cffLetters()
	out, err := Subset(font, []rune{0xC1})
	if err != nil {
		t.Fatal(err)
	}
	f := cffOf(t, out)
	for g, want := range map[int]bool{1: true, 2: true, 3: true} {
		if kept := !bytes.Equal(f.charstrings[g], []byte{14}); kept != want {
			t.Errorf("glyph %d kept: %v", g, kept)
		}
	}
	if err := Verify(font, out, []rune{0xC1}); err != nil {
		t.Error(err)
	}
}

func TestCFFSubsetOfAWOFFAndOfALargeFont(t *testing.T) {
	font := SyntheticCFF(3000)
	keep := []rune("Zwei Äpfel")
	out, err := Subset(font, keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(font, out, keep); err != nil {
		t.Error(err)
	}
	t.Logf("CFF font of %d glyphs: %d bytes, subset %d bytes", glyphCount(t, font), len(font), len(out))
	if len(out)*2 > len(font) {
		t.Errorf("%d bytes from %d", len(out), len(font))
	}
	woff, err := writeWOFF(font)
	if err != nil {
		t.Fatal(err)
	}
	wout, err := Subset(woff, keep)
	if err != nil || string(wout[:4]) != "wOFF" {
		t.Fatalf("%v %q", err, wout[:4])
	}
	if err := Verify(woff, wout, keep); err != nil {
		t.Error(err)
	}
}

func TestCFFSubsetDropsTheGlyphsAfterTheLastOneKept(t *testing.T) {
	font := SyntheticCFF(500)
	out, err := Subset(font, []rune{'A', 'Z'})
	if err != nil {
		t.Fatal(err)
	}
	if got := glyphCount(t, out); got != int('Z'-' ')+2 {
		t.Errorf("%d glyphs", got)
	}
	if err := Verify(font, out, []rune{'A', 'Z'}); err != nil {
		t.Error(err)
	}
}

func TestCFFSubsetOfAFontWithLargeSubroutines(t *testing.T) {
	var lsubrs [][]byte
	for i := range 1500 {
		lsubrs = append(lsubrs, cffCode(i%90, csHLineTo, 50, csVLineTo, 70, csHLineTo, 80, csVLineTo, 60, csHLineTo, 61, csVLineTo, 62, csHLineTo, csReturn))
	}
	glyphs := []cffGlyph{{code: []byte{csEndChar}}}
	chars := map[rune]uint16{}
	for i := range 30 {
		chars[rune('A'+i)] = uint16(len(glyphs))
		glyphs = append(glyphs, cffGlyph{code: cffCode(0, 0, csRMoveTo, i*5-1131+0, csCallSubr, csEndChar), sid: 34 + i})
	}
	font := makeOTTO(buildCFF(glyphs, nil, nil, lsubrs), len(glyphs), chars)
	out, err := Subset(font, []rune("ABC"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(font, out, []rune("ABC")); err != nil {
		t.Error(err)
	}
	t.Logf("1,500 subroutines, 30 glyphs: %d bytes, subset %d bytes", len(font), len(out))
	if len(out)*4 > len(font) {
		t.Errorf("%d bytes from %d", len(out), len(font))
	}
}

func TestCFFSubsetNeverPanicsOnDamagedFonts(t *testing.T) {
	font := cffLetters()
	for i := range font {
		for _, b := range []byte{0x00, 0xFF, 0x80, 0x1D} {
			damaged := bytes.Clone(font)
			damaged[i] = b
			_, _ = Subset(damaged, []rune{'A', 0xC1, 'B', 'Z'})
		}
	}
	for n := range font {
		_, _ = Subset(font[:n], []rune{'A'})
	}
}

func TestCFF2AndOtherUnsupportedFontsAreLeftAlone(t *testing.T) {
	tables, _, _ := readFont(cffLetters())
	delete(tables.data, "CFF ")
	tables.data["CFF2"] = []byte{2, 0, 5, 0, 0}
	if _, err := Subset(writeSFNT(tables.data, tables.flavor), []rune{'A'}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("CFF2: %v", err)
	}
}

func TestCmapOfOldFormatsIsRead(t *testing.T) {
	mac := make([]byte, 262)
	big.PutUint16(mac[0:], 0)
	big.PutUint16(mac[2:], 262)
	mac[6+'A'] = 7
	table := []byte{0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 12}
	chars, err := parseCmap(append(table, mac...))
	if err != nil || chars['A'] != 7 {
		t.Errorf("format 0: %v %v", chars, err)
	}
	six := []byte{0, 6, 0, 18, 0, 0, 0x00, 0x41, 0, 2, 0, 5, 0, 6}
	table = []byte{0, 0, 0, 1, 0, 3, 0, 1, 0, 0, 0, 12}
	chars, err = parseCmap(append(table, six...))
	if err != nil || chars['A'] != 5 || chars['B'] != 6 || len(chars) != 2 {
		t.Errorf("format 6: %v %v", chars, err)
	}
}

func cidFont(t *testing.T, fdsel []byte) []byte {
	t.Helper()
	private := func(subrs ...[]byte) cffPrivate {
		return cffPrivate{present: true, hasSubrs: true, subrs: subrs, entries: []dictEntry{
			{op: 20, operands: [][]byte{cffNum(0)}},
			{op: opSubrs, operands: [][]byte{putInt5(0)}},
		}}
	}
	fontDict := []dictEntry{{op: opPrivate, operands: [][]byte{putInt5(0), putInt5(0)}}}
	codes := [][]byte{
		{csEndChar},
		cffBox(40, 520, 700),
		cffCode(0, 0, csRMoveTo, -106, csCallSubr, csEndChar),
		cffBox(10, 100, 100),
		cffCode(10, 0, csRMoveTo, -107, csCallSubr, csEndChar),
		cffCode(0, 0, csRMoveTo, -106, csCallSubr, csEndChar),
	}
	f := &cffFont{
		header: []byte{1, 0, 4, 4},
		names:  writeIndex([][]byte{[]byte("SyntheticCID")}, 0),
		top: []dictEntry{
			{op: opROS, operands: [][]byte{putInt5(391), putInt5(392), putInt5(0)}},
			{op: opCharset, operands: [][]byte{putInt5(0)}},
			{op: opCharStrings, operands: [][]byte{putInt5(0)}},
			{op: opFDArray, operands: [][]byte{putInt5(0)}},
			{op: opFDSelect, operands: [][]byte{putInt5(0)}},
		},
		strs:        [][]byte{[]byte("Adobe"), []byte("Identity")},
		charstrings: codes,
		charsetRaw:  []byte{0, 0, 1, 0, 2, 0, 3, 0, 4, 0, 5},
		cid:         true,
		fdselRaw:    fdsel,
		fontDicts:   [][]dictEntry{fontDict, cloneEntries(fontDict)},
		privates: []cffPrivate{
			private(cffCode(100, csHLineTo, csReturn), cffCode(7, csHLineTo, csReturn)),
			private(cffCode(200, csHLineTo, csReturn), cffCode(9, csHLineTo, csReturn)),
		},
	}
	chars := map[rune]uint16{'A': 1, 'B': 2, 'C': 3, 'D': 4, 'E': 5}
	return makeOTTO(f.assemble(), len(codes), chars)
}

var (
	fdselFormat0 = []byte{0, 0, 0, 0, 1, 1, 1}
	fdselFormat3 = []byte{3, 0, 2, 0, 0, 0, 0, 3, 1, 0, 6}
)

func TestCIDKeyedFontsAreSubsetPerFontDict(t *testing.T) {
	for name, fdsel := range map[string][]byte{"format 0": fdselFormat0, "format 3": fdselFormat3} {
		t.Run(name, func(t *testing.T) {
			font := cidFont(t, fdsel)
			orig := cffOf(t, font)
			if !orig.cid || !bytes.Equal(orig.fdOf, []uint8{0, 0, 0, 1, 1, 1}) {
				t.Fatalf("cid %v, font dicts %v", orig.cid, orig.fdOf)
			}
			keep := []rune{'C', 'D'}
			out, err := Subset(font, keep)
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(font, out, keep); err != nil {
				t.Error(err)
			}
			f := cffOf(t, out)
			if len(f.fontDicts) != 2 || len(f.privates) != 2 {
				t.Fatalf("%d font dicts, %d privates", len(f.fontDicts), len(f.privates))
			}
			if want := []uint8{0, 0, 0, 1, 1}; !bytes.Equal(f.fdOf, want) {
				t.Errorf("FDSelect %v, want %v", f.fdOf, want)
			}
			if len(f.charstrings) != 5 {
				t.Errorf("%d glyphs kept up to the last one wanted", len(f.charstrings))
			}
			for g, cs := range f.charstrings {
				if empty := bytes.Equal(cs, []byte{14}); empty != (g != 3 && g != 4) {
					t.Errorf("glyph %d empty: %v", g, empty)
				}
			}
			if !bytes.Equal(f.privates[0].subrs[0], []byte{11}) || !bytes.Equal(f.privates[0].subrs[1], []byte{11}) {
				t.Errorf("font dict 0 keeps subroutines no kept glyph calls: %x", f.privates[0].subrs)
			}
			if bytes.Equal(f.privates[1].subrs[0], []byte{11}) || !bytes.Equal(f.privates[1].subrs[1], []byte{11}) {
				t.Errorf("font dict 1 subroutines: %x", f.privates[1].subrs)
			}
		})
	}
}

func TestParseFDSelect(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		n       int
		want    []uint8
		length  int
		wantErr error
	}{
		{"format 0", fdselFormat0, 6, []uint8{0, 0, 0, 1, 1, 1}, 7, nil},
		{"format 3", fdselFormat3, 6, []uint8{0, 0, 0, 1, 1, 1}, 11, nil},
		{"format 3 with a range per glyph", []byte{3, 0, 3, 0, 0, 2, 0, 1, 0, 0, 2, 1, 0, 3}, 3, []uint8{2, 0, 1}, 14, nil},
		{"unknown format", []byte{1, 0, 0}, 2, nil, 0, ErrUnsupported},
		{"format 0 cut short", []byte{0, 0, 1}, 4, nil, 0, ErrCorrupt},
		{"format 3 cut short", []byte{3, 0, 2, 0, 0, 0}, 6, nil, 0, ErrCorrupt},
		{"format 3 ranges run backwards", []byte{3, 0, 2, 0, 3, 0, 0, 1, 0, 0, 6}, 6, nil, 0, ErrCorrupt},
		{"format 3 past the glyphs", []byte{3, 0, 1, 0, 0, 0, 0, 9, 0}, 6, nil, 0, ErrCorrupt},
		{"nothing there", nil, 1, nil, 0, ErrCorrupt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, length, err := parseFDSelect(c.in, 0, c.n)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error %v, want %v", err, c.wantErr)
			}
			if err == nil && (!bytes.Equal(got, c.want) || length != c.length) {
				t.Errorf("%v, %d bytes; want %v, %d", got, length, c.want, c.length)
			}
		})
	}
}

func TestDictBytesWritesEachDictOnItsOwn(t *testing.T) {
	a := []dictEntry{{op: opPrivate, operands: [][]byte{cffNum(5), cffNum(9)}}}
	b := []dictEntry{{op: 20, operands: [][]byte{cffNum(3)}}}
	got := dictBytes([][]dictEntry{a, nil, b})
	if len(got) != 3 || len(got[1]) != 0 {
		t.Fatalf("%x", got)
	}
	for i, in := range [][]dictEntry{a, nil, b} {
		if !bytes.Equal(got[i], writeDict(in)) {
			t.Errorf("dict %d: %x", i, got[i])
		}
	}
	back, err := parseDict(got[0])
	if err != nil || len(back) != 1 || back[0].op != opPrivate {
		t.Errorf("%v %v", back, err)
	}
}
