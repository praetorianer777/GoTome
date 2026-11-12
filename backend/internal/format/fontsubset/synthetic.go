package fontsubset

import (
	"bytes"
	"encoding/binary"
)

// Synthetic is a TrueType font made in code, for tests of the packages that
// use this one: a glyph of a box for each of the printable ASCII characters
// and the German umlauts and ß (the umlauts composite glyphs), and extra
// more glyphs for the characters from U+0400 up, which make it large.
func Synthetic(extra int) []byte {
	glyphs := []testGlyph{{box(50, 0, 450, 700), 500}}
	chars := map[rune]uint16{}
	add := func(r rune, g []byte, advance int) {
		chars[r] = uint16(len(glyphs))
		glyphs = append(glyphs, testGlyph{g, advance})
	}
	for r := rune(0x20); r < 0x7F; r++ {
		if r == ' ' {
			add(r, nil, 250)
			continue
		}
		add(r, box(40+int(r)%20, 0, 400+int(r)%100, 600+int(r)%50), 450+int(r)%150)
	}
	dots := uint16(len(glyphs))
	glyphs = append(glyphs, testGlyph{box(100, 800, 300, 900), 300})
	for _, r := range "äöüÄÖÜ" {
		base := chars[map[rune]rune{'ä': 'a', 'ö': 'o', 'ü': 'u', 'Ä': 'A', 'Ö': 'O', 'Ü': 'U'}[r]]
		add(r, composite([3]int{int(base), 0, 0}, [3]int{int(dots), 80, 0}), 500)
	}
	add('ß', box(30, 0, 520, 720), 560)
	for i := range extra {
		add(rune(0x400+i), box(i%40, 0, 100+i%300, 700), 400+i%200)
	}
	return makeFont(glyphs, chars, len(glyphs)*2/3)
}

func i16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(int16(v))) }

// box is a simple glyph: one rectangle.
func box(x0, y0, x1, y1 int) []byte {
	var g []byte
	g = append(g, i16(1)...)
	for _, v := range []int{x0, y0, x1, y1} {
		g = append(g, i16(v)...)
	}
	g = append(g, i16(3)...)
	g = append(g, i16(0)...)
	g = append(g, 1, 1, 1, 1)
	xs := []int{x0, x1 - x0, 0, x0 - x1}
	ys := []int{y0, 0, y1 - y0, 0}
	for _, v := range xs {
		g = append(g, i16(v)...)
	}
	for _, v := range ys {
		g = append(g, i16(v)...)
	}
	return g
}

// composite puts the glyphs at offsets.
func composite(parts ...[3]int) []byte {
	g := append(i16(-1), make([]byte, 8)...)
	for i, p := range parts {
		flags := 0x0001 | 0x0002
		if i < len(parts)-1 {
			flags |= moreComponents
		}
		g = append(g, i16(flags)...)
		g = append(g, i16(p[0])...)
		g = append(g, i16(p[1])...)
		g = append(g, i16(p[2])...)
	}
	return g
}

type testGlyph struct {
	data    []byte
	advance int
}

// makeFont writes a TrueType font whose glyph i is glyphs[i], with the
// characters given mapped to glyph numbers; metrics counts the glyphs that
// have a metrics record of their own.
func makeFont(glyphs []testGlyph, chars map[rune]uint16, metrics int) []byte {
	var glyf bytes.Buffer
	offsets := []uint32{0}
	for _, g := range glyphs {
		glyf.Write(g.data)
		for glyf.Len()%4 != 0 {
			glyf.WriteByte(0)
		}
		offsets = append(offsets, uint32(glyf.Len()))
	}
	long := glyf.Len() >= 1<<17
	var loca []byte
	for _, o := range offsets {
		if long {
			loca = binary.BigEndian.AppendUint32(loca, o)
		} else {
			loca = binary.BigEndian.AppendUint16(loca, uint16(o/2))
		}
	}
	head := make([]byte, 54)
	big.PutUint32(head, 0x00010000)
	big.PutUint32(head[12:], 0x5F0F3CF5)
	big.PutUint16(head[18:], 1000)
	big.PutUint16(head[36:], 0)
	big.PutUint16(head[38:], 0)
	big.PutUint16(head[40:], 1000)
	big.PutUint16(head[42:], 1000)
	big.PutUint16(head[44:], 0)
	big.PutUint16(head[46:], 8)
	big.PutUint16(head[48:], 2)
	if long {
		big.PutUint16(head[50:], 1)
	}
	hhea := make([]byte, 36)
	big.PutUint32(hhea, 0x00010000)
	big.PutUint16(hhea[4:], 800)
	big.PutUint16(hhea[34:], uint16(metrics))
	var hmtx []byte
	for i, g := range glyphs {
		if i < metrics {
			hmtx = binary.BigEndian.AppendUint16(hmtx, uint16(g.advance))
		}
		hmtx = binary.BigEndian.AppendUint16(hmtx, uint16(i))
	}
	if metrics < len(glyphs) {
		// glyphs without a record share the last advance
		for i := metrics; i < len(glyphs); i++ {
			glyphs[i].advance = glyphs[metrics-1].advance
		}
	}
	maxp := make([]byte, 32)
	big.PutUint32(maxp, 0x00010000)
	big.PutUint16(maxp[4:], uint16(len(glyphs)))
	cmap, err := buildCmap(chars)
	if err != nil {
		panic(err)
	}
	post := make([]byte, 32)
	big.PutUint32(post, 0x00030000)
	name := []byte{0, 0, 0, 0, 0, 6}
	tables := map[string][]byte{
		"head": head, "hhea": hhea, "hmtx": hmtx, "maxp": maxp, "loca": loca, "glyf": glyf.Bytes(),
		"cmap": cmap, "post": post, "name": name,
		"GSUB": make([]byte, 10), "kern": make([]byte, 8), "DSIG": make([]byte, 8), "OS/2": make([]byte, 78),
	}
	return writeSFNT(tables, [4]byte{0, 1, 0, 0})
}
