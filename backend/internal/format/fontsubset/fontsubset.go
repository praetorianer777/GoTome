// Package fontsubset cuts a TrueType font down to the characters a book
// uses. It reads sfnt files with glyf outlines and WOFF files that wrap
// them, keeps the glyphs the characters map to (and what composite glyphs
// are made of), and writes the font again with a new cmap, so that it keeps
// its look at a fraction of its size. Layout tables keyed by glyph (GSUB,
// GPOS, kern, ...) are dropped: an e-reader needs the glyphs, not the
// shaping.
package fontsubset

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
)

var (
	// ErrUnsupported is a font this package does not subset: CFF outlines,
	// WOFF2, collections, or a font with no Unicode character map. It is
	// left as it is.
	ErrUnsupported = errors.New("fontsubset: font not supported")
	// ErrCorrupt is a font whose tables do not hold together.
	ErrCorrupt = errors.New("fontsubset: font is malformed")
)

const (
	maxTables     = 128
	maxTableBytes = 64 << 20
	maxGlyphDepth = 16
	maxCmapRunes  = 0x110000
)

// kept are the tables copied as they are; the others are either rewritten
// or key on glyph numbers and are left out.
var kept = []string{"OS/2", "name", "cvt ", "fpgm", "prep", "gasp"}

var big = binary.BigEndian

// IsFont reports whether data starts like a font of a flavour that Subset
// may be able to read: sfnt with TrueType outlines, or WOFF.
func IsFont(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true", "wOFF":
		return true
	}
	return false
}

// Subset returns font with only the glyphs of runes (those the font has),
// .notdef and the components of composite glyphs. A WOFF is returned as
// WOFF, an sfnt as sfnt.
func Subset(font []byte, runes []rune) ([]byte, error) {
	tables, woff, err := readFont(font)
	if err != nil {
		return nil, err
	}
	out, err := subsetTables(tables, runes)
	if err != nil {
		return nil, err
	}
	sfnt := writeSFNT(out, tables.flavor)
	if woff {
		return writeWOFF(sfnt)
	}
	return sfnt, nil
}

type tableSet struct {
	flavor [4]byte
	data   map[string][]byte
}

func (t tableSet) get(tag string) ([]byte, bool) {
	d, ok := t.data[tag]
	return d, ok
}

// ToSFNT returns the font as plain sfnt, unwrapping a WOFF.
func ToSFNT(font []byte) ([]byte, error) {
	tables, woff, err := readFont(font)
	if err != nil {
		return nil, err
	}
	if !woff {
		return font, nil
	}
	return writeSFNT(tables.data, tables.flavor), nil
}

func readFont(b []byte) (tableSet, bool, error) {
	if len(b) < 12 {
		return tableSet{}, false, ErrCorrupt
	}
	switch string(b[:4]) {
	case "\x00\x01\x00\x00", "true":
		t, err := readSFNT(b)
		return t, false, err
	case "wOFF":
		t, err := readWOFF(b)
		return t, true, err
	}
	return tableSet{}, false, ErrUnsupported
}

func readSFNT(b []byte) (tableSet, error) {
	n := int(big.Uint16(b[4:]))
	if n == 0 || n > maxTables || len(b) < 12+16*n {
		return tableSet{}, ErrCorrupt
	}
	t := tableSet{data: map[string][]byte{}}
	copy(t.flavor[:], b[:4])
	for i := range n {
		rec := b[12+16*i:]
		off, length := uint64(big.Uint32(rec[8:])), uint64(big.Uint32(rec[12:]))
		if off+length > uint64(len(b)) || length > maxTableBytes {
			return tableSet{}, ErrCorrupt
		}
		t.data[string(rec[:4])] = b[off : off+length]
	}
	return checkOutlines(t)
}

func readWOFF(b []byte) (tableSet, error) {
	if len(b) < 44 {
		return tableSet{}, ErrCorrupt
	}
	n := int(big.Uint16(b[12:]))
	if n == 0 || n > maxTables || len(b) < 44+20*n {
		return tableSet{}, ErrCorrupt
	}
	t := tableSet{data: map[string][]byte{}}
	copy(t.flavor[:], b[4:8])
	for i := range n {
		rec := b[44+20*i:]
		off, comp, orig := uint64(big.Uint32(rec[4:])), uint64(big.Uint32(rec[8:])), uint64(big.Uint32(rec[12:]))
		if off+comp > uint64(len(b)) || orig > maxTableBytes || comp > orig {
			return tableSet{}, ErrCorrupt
		}
		raw := b[off : off+comp]
		if comp < orig {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return tableSet{}, ErrCorrupt
			}
			raw, err = io.ReadAll(io.LimitReader(zr, int64(orig)+1))
			if err != nil || uint64(len(raw)) != orig {
				return tableSet{}, ErrCorrupt
			}
		}
		t.data[string(rec[:4])] = raw
	}
	return checkOutlines(t)
}

func checkOutlines(t tableSet) (tableSet, error) {
	if string(t.flavor[:]) == "OTTO" {
		return t, ErrUnsupported
	}
	for _, tag := range []string{"head", "maxp", "cmap", "loca", "glyf", "hhea", "hmtx"} {
		if _, ok := t.data[tag]; !ok {
			if _, cff := t.data["CFF "]; cff || tag == "glyf" || tag == "loca" {
				return t, ErrUnsupported
			}
			return t, ErrCorrupt
		}
	}
	return t, nil
}

func subsetTables(src tableSet, runes []rune) (map[string][]byte, error) {
	head, _ := src.get("head")
	maxp, _ := src.get("maxp")
	hhea, _ := src.get("hhea")
	hmtx, _ := src.get("hmtx")
	loca, _ := src.get("loca")
	glyf, _ := src.get("glyf")
	cmap, _ := src.get("cmap")
	if len(head) < 54 || len(maxp) < 6 || len(hhea) < 36 {
		return nil, ErrCorrupt
	}
	numGlyphs := int(big.Uint16(maxp[4:]))
	longLoca := int16(big.Uint16(head[50:])) != 0
	if numGlyphs == 0 {
		return nil, ErrCorrupt
	}
	offsets, err := glyphOffsets(loca, glyf, numGlyphs, longLoca)
	if err != nil {
		return nil, err
	}
	metrics := int(big.Uint16(hhea[34:]))
	if metrics == 0 || metrics > numGlyphs || len(hmtx) < 4*metrics+2*(numGlyphs-metrics) {
		return nil, ErrCorrupt
	}
	chars, err := parseCmap(cmap)
	if err != nil {
		return nil, err
	}

	keep := map[uint16]bool{0: true}
	used := map[rune]uint16{}
	for _, r := range runes {
		if g, ok := chars[r]; ok && int(g) < numGlyphs {
			keep[g] = true
			used[r] = g
		}
	}
	var stack []uint16
	for g := range keep {
		stack = append(stack, g)
	}
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		parts, err := components(glyf[offsets[g]:offsets[g+1]])
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			if int(p.glyph) >= numGlyphs {
				return nil, ErrCorrupt
			}
			if !keep[p.glyph] {
				keep[p.glyph] = true
				stack = append(stack, p.glyph)
			}
		}
	}
	order := make([]uint16, 0, len(keep))
	for g := range keep {
		order = append(order, g)
	}
	slices.Sort(order)
	renumber := make(map[uint16]uint16, len(order))
	for i, g := range order {
		renumber[g] = uint16(i)
	}

	var newGlyf bytes.Buffer
	starts := make([]uint32, 0, len(order)+1)
	for _, g := range order {
		starts = append(starts, uint32(newGlyf.Len()))
		data := bytes.Clone(glyf[offsets[g]:offsets[g+1]])
		parts, err := components(data)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			big.PutUint16(data[p.at:], renumber[p.glyph])
		}
		newGlyf.Write(data)
		for newGlyf.Len()%4 != 0 {
			newGlyf.WriteByte(0)
		}
	}
	starts = append(starts, uint32(newGlyf.Len()))

	newHead := bytes.Clone(head)
	var newLoca []byte
	if newGlyf.Len() < 1<<17 {
		newLoca = make([]byte, 2*len(starts))
		for i, s := range starts {
			big.PutUint16(newLoca[2*i:], uint16(s/2))
		}
		big.PutUint16(newHead[50:], 0)
	} else {
		newLoca = make([]byte, 4*len(starts))
		for i, s := range starts {
			big.PutUint32(newLoca[4*i:], s)
		}
		big.PutUint16(newHead[50:], 1)
	}

	newHmtx := make([]byte, 0, 4*len(order))
	for _, g := range order {
		gi := int(g)
		var adv, lsb uint16
		if gi < metrics {
			adv, lsb = big.Uint16(hmtx[4*gi:]), big.Uint16(hmtx[4*gi+2:])
		} else {
			adv = big.Uint16(hmtx[4*(metrics-1):])
			lsb = big.Uint16(hmtx[4*metrics+2*(gi-metrics):])
		}
		newHmtx = binary.BigEndian.AppendUint16(newHmtx, adv)
		newHmtx = binary.BigEndian.AppendUint16(newHmtx, lsb)
	}
	newHhea := bytes.Clone(hhea)
	big.PutUint16(newHhea[34:], uint16(len(order)))
	newMaxp := bytes.Clone(maxp)
	big.PutUint16(newMaxp[4:], uint16(len(order)))

	mapped := make(map[rune]uint16, len(used))
	for r, g := range used {
		mapped[r] = renumber[g]
	}
	newCmap, err := buildCmap(mapped)
	if err != nil {
		return nil, err
	}

	out := map[string][]byte{
		"head": newHead, "hhea": newHhea, "maxp": newMaxp, "hmtx": newHmtx,
		"loca": newLoca, "glyf": newGlyf.Bytes(), "cmap": newCmap,
	}
	for _, tag := range kept {
		if d, ok := src.get(tag); ok {
			out[tag] = d
		}
	}
	if post, ok := src.get("post"); ok && len(post) >= 32 {
		p := bytes.Clone(post[:32])
		big.PutUint32(p, 0x00030000)
		out["post"] = p
	}
	return out, nil
}

func glyphOffsets(loca, glyf []byte, numGlyphs int, long bool) ([]uint32, error) {
	width := 2
	if long {
		width = 4
	}
	if len(loca) < width*(numGlyphs+1) {
		return nil, ErrCorrupt
	}
	offsets := make([]uint32, numGlyphs+1)
	for i := range offsets {
		if long {
			offsets[i] = big.Uint32(loca[4*i:])
		} else {
			offsets[i] = 2 * uint32(big.Uint16(loca[2*i:]))
		}
		if i > 0 && offsets[i] < offsets[i-1] || uint64(offsets[i]) > uint64(len(glyf)) {
			return nil, ErrCorrupt
		}
	}
	return offsets, nil
}

type component struct {
	glyph uint16
	// at is where the glyph index lies in the glyph's data.
	at int
}

const (
	arg1And2AreWords = 0x0001
	weHaveAScale     = 0x0008
	moreComponents   = 0x0020
	weHaveXAndYScale = 0x0040
	weHaveATwoByTwo  = 0x0080
)

// components lists what a composite glyph is made of; a simple or empty
// glyph has none.
func components(data []byte) ([]component, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 10 {
		return nil, ErrCorrupt
	}
	if int16(big.Uint16(data)) >= 0 {
		return nil, nil
	}
	var out []component
	at := 10
	for {
		if at+4 > len(data) || len(out) > 4096 {
			return nil, ErrCorrupt
		}
		flags := big.Uint16(data[at:])
		out = append(out, component{glyph: big.Uint16(data[at+2:]), at: at + 2})
		at += 4
		if flags&arg1And2AreWords != 0 {
			at += 4
		} else {
			at += 2
		}
		switch {
		case flags&weHaveAScale != 0:
			at += 2
		case flags&weHaveXAndYScale != 0:
			at += 4
		case flags&weHaveATwoByTwo != 0:
			at += 8
		}
		if flags&moreComponents == 0 {
			break
		}
	}
	if at > len(data) {
		return nil, ErrCorrupt
	}
	return out, nil
}

// parseCmap reads the best Unicode subtable of format 4 or 12.
func parseCmap(cmap []byte) (map[rune]uint16, error) {
	if len(cmap) < 4 {
		return nil, ErrCorrupt
	}
	n := int(big.Uint16(cmap[2:]))
	if len(cmap) < 4+8*n {
		return nil, ErrCorrupt
	}
	best, bestScore := -1, 0
	for i := range n {
		rec := cmap[4+8*i:]
		platform, encoding := big.Uint16(rec), big.Uint16(rec[2:])
		off := int(big.Uint32(rec[4:]))
		if off+2 > len(cmap) {
			return nil, ErrCorrupt
		}
		format := big.Uint16(cmap[off:])
		if format != 4 && format != 12 {
			continue
		}
		score := 0
		switch {
		case platform == 3 && encoding == 10, platform == 0 && encoding == 4:
			score = 4
		case platform == 3 && encoding == 1:
			score = 3
		case platform == 0:
			score = 2
		}
		if score > bestScore {
			best, bestScore = off, score
		}
	}
	if best < 0 {
		return nil, ErrUnsupported
	}
	chars := map[rune]uint16{}
	sub := cmap[best:]
	if big.Uint16(sub) == 12 {
		if len(sub) < 16 {
			return nil, ErrCorrupt
		}
		groups := int(big.Uint32(sub[12:]))
		if groups < 0 || len(sub) < 16+12*groups {
			return nil, ErrCorrupt
		}
		for i := range groups {
			g := sub[16+12*i:]
			first, last, glyph := big.Uint32(g), big.Uint32(g[4:]), big.Uint32(g[8:])
			if first > last || last >= maxCmapRunes || uint64(glyph)+uint64(last-first) > 0xFFFF {
				return nil, ErrCorrupt
			}
			for c := first; c <= last; c++ {
				if len(chars) > maxCmapRunes {
					return nil, ErrCorrupt
				}
				if g := glyph + c - first; g != 0 {
					chars[rune(c)] = uint16(g)
				}
			}
		}
		return chars, nil
	}
	if len(sub) < 16 {
		return nil, ErrCorrupt
	}
	segs := int(big.Uint16(sub[6:])) / 2
	if len(sub) < 16+8*segs {
		return nil, ErrCorrupt
	}
	ends, starts := sub[14:], sub[16+2*segs:]
	deltas, ranges := sub[16+4*segs:], sub[16+6*segs:]
	for i := range segs {
		end, start := big.Uint16(ends[2*i:]), big.Uint16(starts[2*i:])
		delta, ro := big.Uint16(deltas[2*i:]), int(big.Uint16(ranges[2*i:]))
		if start > end {
			return nil, ErrCorrupt
		}
		for c := int(start); c <= int(end) && c < 0xFFFF; c++ {
			var g uint16
			if ro == 0 {
				g = uint16(c) + delta
			} else {
				at := 16 + 6*segs + 2*i + ro + 2*(c-int(start))
				if at+2 > len(sub) {
					return nil, ErrCorrupt
				}
				if g = big.Uint16(sub[at:]); g != 0 {
					g += delta
				}
			}
			if g != 0 {
				chars[rune(c)] = g
			}
		}
	}
	return chars, nil
}

type group struct {
	first, last rune
	glyph       uint16
}

func groupsOf(chars map[rune]uint16) []group {
	runes := make([]rune, 0, len(chars))
	for r := range chars {
		runes = append(runes, r)
	}
	slices.Sort(runes)
	var out []group
	for _, r := range runes {
		g := chars[r]
		if n := len(out); n > 0 && out[n-1].last+1 == r && out[n-1].glyph+uint16(r-out[n-1].first) == g {
			out[n-1].last = r
			continue
		}
		out = append(out, group{r, r, g})
	}
	return out
}

func buildCmap(chars map[rune]uint16) ([]byte, error) {
	bmpChars := map[rune]uint16{}
	all12 := map[rune]uint16{}
	astral := false
	for r, g := range chars {
		switch {
		case r >= 0xD800 && r < 0xE000, r == 0xFFFF:
		case r > 0xFFFF:
			astral = true
			all12[r] = g
		default:
			bmpChars[r] = g
			all12[r] = g
		}
	}
	bmp := groupsOf(bmpChars)
	segs := len(bmp) + 1
	if 16+8*segs > 0xFFFF {
		return nil, fmt.Errorf("%w: too many character ranges", ErrUnsupported)
	}
	f4 := make([]byte, 16+8*segs)
	big.PutUint16(f4, 4)
	big.PutUint16(f4[2:], uint16(len(f4)))
	big.PutUint16(f4[6:], uint16(2*segs))
	entry := 0
	for 1<<(entry+1) <= segs {
		entry++
	}
	search := 2 << entry
	big.PutUint16(f4[8:], uint16(search))
	big.PutUint16(f4[10:], uint16(entry))
	big.PutUint16(f4[12:], uint16(2*segs-search))
	all := append(bmp, group{0xFFFF, 0xFFFF, 0})
	for i, g := range all {
		big.PutUint16(f4[14+2*i:], uint16(g.last))
		big.PutUint16(f4[16+2*segs+2*i:], uint16(g.first))
		delta := g.glyph - uint16(g.first)
		if g.first == 0xFFFF {
			delta = 1
		}
		big.PutUint16(f4[16+4*segs+2*i:], delta)
	}

	type record struct {
		platform, encoding uint16
		table              int
	}
	tables := [][]byte{f4}
	records := []record{{0, 3, 0}, {3, 1, 0}}
	if astral {
		groups12 := groupsOf(all12)
		f12 := make([]byte, 16+12*len(groups12))
		big.PutUint16(f12, 12)
		big.PutUint32(f12[4:], uint32(len(f12)))
		big.PutUint32(f12[12:], uint32(len(groups12)))
		for i, g := range groups12 {
			big.PutUint32(f12[16+12*i:], uint32(g.first))
			big.PutUint32(f12[20+12*i:], uint32(g.last))
			big.PutUint32(f12[24+12*i:], uint32(g.glyph))
		}
		tables = append(tables, f12)
		records = []record{{0, 3, 0}, {0, 4, 1}, {3, 1, 0}, {3, 10, 1}}
	}
	head := 4 + 8*len(records)
	offsets := make([]int, len(tables))
	body := []byte(nil)
	for i, t := range tables {
		offsets[i] = head + len(body)
		body = append(body, t...)
	}
	out := make([]byte, head, head+len(body))
	big.PutUint16(out[2:], uint16(len(records)))
	for i, r := range records {
		big.PutUint16(out[4+8*i:], r.platform)
		big.PutUint16(out[6+8*i:], r.encoding)
		big.PutUint32(out[8+8*i:], uint32(offsets[r.table]))
	}
	return append(out, body...), nil
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += big.Uint32(w[:])
	}
	return sum
}

func padded(n int) int { return (n + 3) &^ 3 }

func writeSFNT(tables map[string][]byte, flavor [4]byte) []byte {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	n := len(tags)
	head := 12 + 16*n
	size := head
	for _, tag := range tags {
		size += padded(len(tables[tag]))
	}
	out := make([]byte, size)
	copy(out, flavor[:])
	big.PutUint16(out[4:], uint16(n))
	entry := 0
	for 1<<(entry+1) <= n {
		entry++
	}
	big.PutUint16(out[6:], uint16(16<<entry))
	big.PutUint16(out[8:], uint16(entry))
	big.PutUint16(out[10:], uint16(16*n-16<<entry))
	at := head
	adjust := -1
	for i, tag := range tags {
		data := tables[tag]
		rec := out[12+16*i:]
		copy(rec, tag)
		copy(out[at:], data)
		if tag == "head" && len(data) >= 12 {
			adjust = at + 8
			big.PutUint32(out[adjust:], 0)
		}
		big.PutUint32(rec[4:], checksum(out[at:at+len(data)]))
		big.PutUint32(rec[8:], uint32(at))
		big.PutUint32(rec[12:], uint32(len(data)))
		at += padded(len(data))
	}
	if adjust >= 0 {
		big.PutUint32(out[adjust:], 0xB1B0AFBA-checksum(out))
	}
	return out
}

func writeWOFF(sfnt []byte) ([]byte, error) {
	t, err := readSFNT(sfnt)
	if err != nil && !errors.Is(err, ErrUnsupported) {
		return nil, err
	}
	tags := make([]string, 0, len(t.data))
	for tag := range t.data {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	head := 44 + 20*len(tags)
	dir := make([]byte, head)
	var body bytes.Buffer
	for i, tag := range tags {
		data := t.data[tag]
		stored := data
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		_, _ = zw.Write(data)
		_ = zw.Close()
		if z.Len() < len(data) {
			stored = z.Bytes()
		}
		rec := dir[44+20*i:]
		copy(rec, tag)
		big.PutUint32(rec[4:], uint32(head+body.Len()))
		big.PutUint32(rec[8:], uint32(len(stored)))
		big.PutUint32(rec[12:], uint32(len(data)))
		big.PutUint32(rec[16:], checksum(data))
		body.Write(stored)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	copy(dir, "wOFF")
	copy(dir[4:], t.flavor[:])
	big.PutUint32(dir[8:], uint32(head+body.Len()))
	big.PutUint16(dir[12:], uint16(len(tags)))
	big.PutUint32(dir[16:], uint32(len(sfnt)))
	big.PutUint16(dir[20:], 1)
	return append(dir, body.Bytes()...), nil
}
