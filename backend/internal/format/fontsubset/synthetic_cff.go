package fontsubset

// cffNum is a Type 2 charstring number.
func cffNum(v int) []byte {
	if v >= -107 && v <= 107 {
		return []byte{byte(v + 139)}
	}
	return []byte{28, byte(v >> 8), byte(v)}
}

func cffCode(parts ...any) []byte {
	var out []byte
	for _, p := range parts {
		switch p := p.(type) {
		case int:
			out = append(out, cffNum(p)...)
		case byte:
			out = append(out, p)
		}
	}
	return out
}

const (
	csRMoveTo   byte = 21
	csHLineTo   byte = 6
	csVLineTo   byte = 7
	csReturn    byte = 11
	csEndChar   byte = 14
	csCallSubr  byte = 10
	csCallGSubr byte = 29
)

// cffBox is a glyph of one rectangle.
func cffBox(x, w, h int) []byte {
	return cffCode(x, 0, csRMoveTo, w, csHLineTo, h, csVLineTo, -w, csHLineTo, csEndChar)
}

type cffGlyph struct {
	code []byte
	sid  int
}

// buildCFF writes a CFF table with a charset of format 0, one Private DICT
// and the subroutines given.
func buildCFF(glyphs []cffGlyph, strings []string, gsubrs, lsubrs [][]byte) []byte {
	f := &cffFont{
		header: []byte{1, 0, 4, 4},
		names:  writeIndex([][]byte{[]byte("Synthetic")}, 0),
		top: []dictEntry{
			{op: opCharset, operands: [][]byte{putInt5(0)}},
			{op: opCharStrings, operands: [][]byte{putInt5(0)}},
			{op: opPrivate, operands: [][]byte{putInt5(0), putInt5(0)}},
		},
		gsubrs:   gsubrs,
		privates: []cffPrivate{{present: true, entries: []dictEntry{{op: 20, operands: [][]byte{cffNum(0)}}, {op: opSubrs, operands: [][]byte{putInt5(0)}}}, subrs: lsubrs, hasSubrs: true}},
	}
	var strs [][]byte
	for _, s := range strings {
		strs = append(strs, []byte(s))
	}
	f.strs = strs
	charset := []byte{0}
	for i, g := range glyphs {
		f.charstrings = append(f.charstrings, g.code)
		if i > 0 {
			charset = append(charset, byte(g.sid>>8), byte(g.sid))
		}
	}
	f.charsetRaw = charset
	return f.assemble()
}

// makeOTTO wraps a CFF table in an OpenType font with a glyph of the given
// advance for each charstring and the characters mapped.
func makeOTTO(cff []byte, numGlyphs int, chars map[rune]uint16) []byte {
	head := make([]byte, 54)
	big.PutUint32(head, 0x00010000)
	big.PutUint32(head[12:], 0x5F0F3CF5)
	big.PutUint16(head[18:], 1000)
	hhea := make([]byte, 36)
	big.PutUint32(hhea, 0x00010000)
	big.PutUint16(hhea[34:], uint16(numGlyphs))
	hmtx := make([]byte, 4*numGlyphs)
	for g := range numGlyphs {
		big.PutUint16(hmtx[4*g:], uint16(500+g%100))
	}
	maxp := make([]byte, 6)
	big.PutUint32(maxp, 0x00005000)
	big.PutUint16(maxp[4:], uint16(numGlyphs))
	cmap, err := buildCmap(chars)
	if err != nil {
		panic(err)
	}
	post := make([]byte, 32)
	big.PutUint32(post, 0x00030000)
	return writeSFNT(map[string][]byte{
		"head": head, "hhea": hhea, "hmtx": hmtx, "maxp": maxp, "cmap": cmap, "CFF ": cff, "post": post,
		"name": {0, 0, 0, 0, 0, 6}, "OS/2": make([]byte, 78), "GSUB": make([]byte, 10),
	}, [4]byte{'O', 'T', 'T', 'O'})
}

// SyntheticCFF is an OpenType font with CFF outlines made in code, for tests
// of the packages that use this one: the glyphs, characters and layout of
// Synthetic, each drawn by its own charstring that also calls a subroutine,
// and extra more glyphs for the characters from U+0400 up.
func SyntheticCFF(extra int) []byte {
	// subroutine 0 of each kind draws the tail of a box
	lsubrs := [][]byte{cffCode(100, csHLineTo, 100, csVLineTo, -100, csHLineTo, csReturn), cffCode(1, csHLineTo, csReturn)}
	gsubrs := [][]byte{cffCode(5, csHLineTo, csReturn)}
	glyphs := []cffGlyph{{code: []byte{csEndChar}}}
	chars := map[rune]uint16{}
	add := func(r rune, code []byte) {
		chars[r] = uint16(len(glyphs))
		glyphs = append(glyphs, cffGlyph{code: code, sid: 391 + len(glyphs)})
	}
	var names []string
	for r := rune(0x20); r < 0x7F; r++ {
		add(r, cffCode(int(r)%20, 0, csRMoveTo, -107, csCallSubr, csEndChar))
	}
	for _, r := range "äöüÄÖÜß" {
		add(r, cffCode(10, 0, csRMoveTo, -107, csCallSubr, csEndChar))
	}
	for i := range extra {
		add(rune(0x400+i), cffCode(i%40, 0, csRMoveTo, 100+i%300, csHLineTo, 700, csVLineTo, -100-i%300, csHLineTo, -107, csCallSubr, csEndChar))
	}
	for i := 1; i < len(glyphs); i++ {
		names = append(names, "g"+string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676%26)))
	}
	return makeOTTO(buildCFF(glyphs, names, gsubrs, lsubrs), len(glyphs), chars)
}
