package fontsubset

import (
	"bytes"
	"slices"
)

const (
	opCharset        = 15
	opEncoding       = 16
	opCharStrings    = 17
	opPrivate        = 18
	opSubrs          = 19
	opCharstringType = 0x0C06
	opROS            = 0x0C1E
	opFDArray        = 0x0C24
	opFDSelect       = 0x0C25

	maxCharstringDepth = 10
	maxCharstringSteps = 1 << 20
)

// dictEntry is an operator of a DICT with its operands as written.
type dictEntry struct {
	op       int
	operands [][]byte
}

func operandSize(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, ErrCorrupt
	}
	c := b[0]
	n := 0
	switch {
	case c >= 32 && c <= 246:
		n = 1
	case c >= 247 && c <= 254:
		n = 2
	case c == 28:
		n = 3
	case c == 29:
		n = 5
	case c == 30:
		for i := 1; i < len(b); i++ {
			if b[i]&0x0f == 0x0f || b[i]>>4 == 0x0f {
				return i + 1, nil
			}
		}
		return 0, ErrCorrupt
	default:
		return 0, ErrCorrupt
	}
	if n > len(b) {
		return 0, ErrCorrupt
	}
	return n, nil
}

// operandInt reads an integer operand; reals are not integers.
func operandInt(b []byte) (int, bool) {
	switch c := b[0]; {
	case c >= 32 && c <= 246:
		return int(c) - 139, true
	case c >= 247 && c <= 250:
		return (int(c)-247)*256 + int(b[1]) + 108, true
	case c >= 251 && c <= 254:
		return -(int(c)-251)*256 - int(b[1]) - 108, true
	case c == 28:
		return int(int16(big.Uint16(b[1:]))), true
	case c == 29:
		return int(int32(big.Uint32(b[1:]))), true
	}
	return 0, false
}

func putInt5(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func parseDict(b []byte) ([]dictEntry, error) {
	var out []dictEntry
	var pending [][]byte
	for i := 0; i < len(b); {
		c := b[i]
		if c > 21 {
			n, err := operandSize(b[i:])
			if err != nil {
				return nil, err
			}
			pending = append(pending, b[i:i+n])
			i += n
			continue
		}
		op := int(c)
		i++
		if c == 12 {
			if i >= len(b) {
				return nil, ErrCorrupt
			}
			op = 0x0C00 | int(b[i])
			i++
		}
		out = append(out, dictEntry{op, pending})
		pending = nil
	}
	if len(pending) > 0 {
		return nil, ErrCorrupt
	}
	return out, nil
}

func writeDict(entries []dictEntry) []byte {
	var out []byte
	for _, e := range entries {
		for _, o := range e.operands {
			out = append(out, o...)
		}
		if e.op >= 0x0C00 {
			out = append(out, 12, byte(e.op))
		} else {
			out = append(out, byte(e.op))
		}
	}
	return out
}

func cloneEntries(e []dictEntry) []dictEntry {
	out := make([]dictEntry, len(e))
	for i, x := range e {
		out[i] = dictEntry{x.op, slices.Clone(x.operands)}
	}
	return out
}

func findEntry(entries []dictEntry, op int) *dictEntry {
	for i := range entries {
		if entries[i].op == op {
			return &entries[i]
		}
	}
	return nil
}

// ints returns an entry's operands when all are integers.
func (e *dictEntry) ints() ([]int, bool) {
	out := make([]int, len(e.operands))
	for i, o := range e.operands {
		v, ok := operandInt(o)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

func setInts(entries []dictEntry, op int, vals ...int) {
	if e := findEntry(entries, op); e != nil {
		e.operands = nil
		for _, v := range vals {
			e.operands = append(e.operands, putInt5(v))
		}
	}
}

func dropEntry(entries []dictEntry, op int) []dictEntry {
	return slices.DeleteFunc(entries, func(e dictEntry) bool { return e.op == op })
}

func readIndex(b []byte, at int) ([][]byte, int, error) {
	if at < 0 || at+2 > len(b) {
		return nil, 0, ErrCorrupt
	}
	n := int(big.Uint16(b[at:]))
	if n == 0 {
		return nil, at + 2, nil
	}
	if at+3 > len(b) {
		return nil, 0, ErrCorrupt
	}
	size := int(b[at+2])
	if size < 1 || size > 4 {
		return nil, 0, ErrCorrupt
	}
	table := at + 3
	base := table + (n+1)*size - 1
	if base+1 > len(b) {
		return nil, 0, ErrCorrupt
	}
	off := func(i int) int {
		v := 0
		for _, c := range b[table+i*size : table+(i+1)*size] {
			v = v<<8 | int(c)
		}
		return v
	}
	prev := off(0)
	if prev != 1 {
		return nil, 0, ErrCorrupt
	}
	items := make([][]byte, n)
	for i := range n {
		next := off(i + 1)
		if next < prev || base+next > len(b) {
			return nil, 0, ErrCorrupt
		}
		items[i] = b[base+prev : base+next]
		prev = next
	}
	return items, base + prev, nil
}

// writeIndex writes an INDEX; size is the width of its offsets, 0 for the
// narrowest that fits.
func writeIndex(items [][]byte, size int) []byte {
	if len(items) == 0 {
		return []byte{0, 0}
	}
	total := 1
	for _, it := range items {
		total += len(it)
	}
	if size == 0 {
		size = 1
		for total >= 1<<(8*size) {
			size++
		}
	}
	out := []byte{byte(len(items) >> 8), byte(len(items)), byte(size)}
	put := func(v int) {
		for s := size - 1; s >= 0; s-- {
			out = append(out, byte(v>>(8*s)))
		}
	}
	at := 1
	put(at)
	for _, it := range items {
		at += len(it)
		put(at)
	}
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func parseCharset(cff []byte, off, n int) (sids []uint16, length int, err error) {
	switch {
	case off == 0:
		sids = make([]uint16, n)
		for g := 1; g < n && g <= 228; g++ {
			sids[g] = uint16(g)
		}
		return sids, 0, nil
	case off <= 2:
		return nil, 0, nil
	}
	if off >= len(cff) {
		return nil, 0, ErrCorrupt
	}
	at := off + 1
	sids = make([]uint16, 1, n)
	switch cff[off] {
	case 0:
		if at+2*(n-1) > len(cff) {
			return nil, 0, ErrCorrupt
		}
		for g := 1; g < n; g++ {
			sids = append(sids, big.Uint16(cff[at:]))
			at += 2
		}
	case 1, 2:
		wide := cff[off] == 2
		for len(sids) < n {
			if at+4 > len(cff) {
				return nil, 0, ErrCorrupt
			}
			first, left := int(big.Uint16(cff[at:])), int(cff[at+2])
			at += 3
			if wide {
				left = left<<8 | int(cff[at])
				at++
			}
			for i := 0; i <= left && len(sids) < n; i++ {
				sids = append(sids, uint16(first+i))
			}
		}
	default:
		return nil, 0, ErrCorrupt
	}
	return sids, at - off, nil
}

func parseFDSelect(cff []byte, off, n int) (fds []uint8, length int, err error) {
	if off+1 > len(cff) {
		return nil, 0, ErrCorrupt
	}
	fds = make([]uint8, n)
	switch cff[off] {
	case 0:
		if off+1+n > len(cff) {
			return nil, 0, ErrCorrupt
		}
		copy(fds, cff[off+1:])
		return fds, 1 + n, nil
	case 3:
		if off+3 > len(cff) {
			return nil, 0, ErrCorrupt
		}
		ranges := int(big.Uint16(cff[off+1:]))
		end := off + 3 + 3*ranges + 2
		if end > len(cff) {
			return nil, 0, ErrCorrupt
		}
		for i := range ranges {
			r := cff[off+3+3*i:]
			first, next := int(big.Uint16(r)), int(big.Uint16(r[3:]))
			if first > next || next > n {
				return nil, 0, ErrCorrupt
			}
			for g := first; g < next; g++ {
				fds[g] = r[2]
			}
		}
		return fds, end - off, nil
	}
	return nil, 0, ErrUnsupported
}

type cffPrivate struct {
	present  bool
	entries  []dictEntry
	subrs    [][]byte
	hasSubrs bool
}

type cffFont struct {
	header, names []byte
	strs          [][]byte
	top           []dictEntry
	gsubrs        [][]byte
	charstrings   [][]byte
	charsetRaw    []byte
	sids          []uint16
	cid           bool
	fdselRaw      []byte
	fdOf          []uint8
	fontDicts     [][]dictEntry
	privates      []cffPrivate
}

func intsOf(entries []dictEntry, op int) ([]int, bool) {
	e := findEntry(entries, op)
	if e == nil {
		return nil, false
	}
	return e.ints()
}

func parseCFF(b []byte) (*cffFont, error) {
	if len(b) < 4 || b[0] != 1 || int(b[2]) < 4 || int(b[2]) > len(b) {
		return nil, ErrCorrupt
	}
	f := &cffFont{header: b[:b[2]]}
	_, at, err := readIndex(b, int(b[2]))
	if err != nil {
		return nil, err
	}
	f.names = b[b[2]:at]
	tops, at, err := readIndex(b, at)
	if err != nil || len(tops) != 1 {
		return nil, ErrCorrupt
	}
	if f.strs, at, err = readIndex(b, at); err != nil {
		return nil, err
	}
	if f.gsubrs, _, err = readIndex(b, at); err != nil {
		return nil, err
	}
	if f.top, err = parseDict(tops[0]); err != nil {
		return nil, err
	}
	if v, ok := intsOf(f.top, opCharstringType); ok && (len(v) != 1 || v[0] != 2) {
		return nil, ErrUnsupported
	}
	cs, ok := intsOf(f.top, opCharStrings)
	if !ok || len(cs) != 1 {
		return nil, ErrCorrupt
	}
	if f.charstrings, _, err = readIndex(b, cs[0]); err != nil {
		return nil, err
	}
	n := len(f.charstrings)
	if n == 0 {
		return nil, ErrCorrupt
	}
	f.cid = findEntry(f.top, opROS) != nil
	charset := 0
	if v, ok := intsOf(f.top, opCharset); ok && len(v) == 1 {
		charset = v[0]
	}
	var length int
	if f.sids, length, err = parseCharset(b, charset, n); err != nil {
		return nil, err
	}
	if charset > 2 {
		f.charsetRaw = b[charset : charset+length]
	}
	loadPrivate := func(entries []dictEntry) (cffPrivate, error) {
		v, ok := intsOf(entries, opPrivate)
		if !ok || len(v) != 2 || v[0] <= 0 {
			return cffPrivate{}, nil
		}
		if v[1] < 0 || v[1]+v[0] > len(b) {
			return cffPrivate{}, ErrCorrupt
		}
		p := cffPrivate{present: true}
		if p.entries, err = parseDict(b[v[1] : v[1]+v[0]]); err != nil {
			return p, err
		}
		if so, ok := intsOf(p.entries, opSubrs); ok && len(so) == 1 {
			p.hasSubrs = true
			if p.subrs, _, err = readIndex(b, v[1]+so[0]); err != nil {
				return p, err
			}
		}
		return p, nil
	}
	if !f.cid {
		p, err := loadPrivate(f.top)
		if err != nil {
			return nil, err
		}
		f.privates = []cffPrivate{p}
		return f, nil
	}
	fa, ok := intsOf(f.top, opFDArray)
	fs, ok2 := intsOf(f.top, opFDSelect)
	if !ok || !ok2 || len(fa) != 1 || len(fs) != 1 {
		return nil, ErrCorrupt
	}
	dicts, _, err := readIndex(b, fa[0])
	if err != nil || len(dicts) == 0 || len(dicts) > 256 {
		return nil, ErrCorrupt
	}
	if f.fdOf, length, err = parseFDSelect(b, fs[0], n); err != nil {
		return nil, err
	}
	f.fdselRaw = b[fs[0] : fs[0]+length]
	for _, d := range dicts {
		entries, err := parseDict(d)
		if err != nil {
			return nil, err
		}
		p, err := loadPrivate(entries)
		if err != nil {
			return nil, err
		}
		f.fontDicts = append(f.fontDicts, entries)
		f.privates = append(f.privates, p)
	}
	for _, fd := range f.fdOf {
		if int(fd) >= len(f.privates) {
			return nil, ErrCorrupt
		}
	}
	return f, nil
}

func privateBlock(p cffPrivate) (dict, subrs []byte) {
	entries := cloneEntries(p.entries)
	if p.hasSubrs {
		setInts(entries, opSubrs, 0)
		setInts(entries, opSubrs, len(writeDict(entries)))
		subrs = writeIndex(p.subrs, 0)
	}
	return writeDict(entries), subrs
}

// assemble writes the CFF table: header, names, the Top DICT, strings,
// global subroutines, then charset, FDSelect, CharStrings and the Private
// DICTs with their subroutines. Offsets are written five bytes wide, so
// they can be filled in after the sizes are known.
func (f *cffFont) assemble() []byte {
	top := dropEntry(cloneEntries(f.top), opEncoding)
	gsub := writeIndex(f.gsubrs, 0)
	chars := writeIndex(f.charstrings, 0)
	strs := writeIndex(f.strs, 0)
	type block struct{ dict, subrs []byte }
	blocks := make([]block, len(f.privates))
	for i, p := range f.privates {
		if p.present {
			blocks[i].dict, blocks[i].subrs = privateBlock(p)
		}
	}
	fonts := make([][]dictEntry, len(f.fontDicts))
	fill := func(at []int) {
		setInts(top, opCharStrings, 0)
		if f.charsetRaw != nil {
			setInts(top, opCharset, 0)
		}
		if f.cid {
			setInts(top, opFDArray, 0)
			setInts(top, opFDSelect, 0)
		} else if f.privates[0].present {
			setInts(top, opPrivate, len(blocks[0].dict), 0)
		}
		for i := range f.fontDicts {
			fonts[i] = cloneEntries(f.fontDicts[i])
			if f.privates[i].present {
				off := 0
				if at != nil {
					off = at[i]
				}
				setInts(fonts[i], opPrivate, len(blocks[i].dict), off)
			}
		}
	}
	fill(nil)
	var fdarray []byte
	if f.cid {
		fdarray = writeIndex(dictBytes(fonts), 0)
	}
	topIndex := writeIndex([][]byte{writeDict(top)}, 4)
	at := len(f.header) + len(f.names) + len(topIndex) + len(strs) + len(gsub)
	charsetAt := at
	at += len(f.charsetRaw)
	selAt := at
	at += len(f.fdselRaw)
	charsAt := at
	at += len(chars)
	fdarrayAt := at
	at += len(fdarray)
	privAt := make([]int, len(blocks))
	for i, b := range blocks {
		privAt[i] = at
		at += len(b.dict) + len(b.subrs)
	}

	fill(privAt)
	setInts(top, opCharStrings, charsAt)
	if f.charsetRaw != nil {
		setInts(top, opCharset, charsetAt)
	}
	if f.cid {
		setInts(top, opFDArray, fdarrayAt)
		setInts(top, opFDSelect, selAt)
		fdarray = writeIndex(dictBytes(fonts), 0)
	} else if f.privates[0].present {
		setInts(top, opPrivate, len(blocks[0].dict), privAt[0])
	}
	topIndex = writeIndex([][]byte{writeDict(top)}, 4)

	var out bytes.Buffer
	out.Write(f.header)
	out.Write(f.names)
	out.Write(topIndex)
	out.Write(strs)
	out.Write(gsub)
	out.Write(f.charsetRaw)
	out.Write(f.fdselRaw)
	out.Write(chars)
	out.Write(fdarray)
	for _, b := range blocks {
		out.Write(b.dict)
		out.Write(b.subrs)
	}
	return out.Bytes()
}

func dictBytes(entries [][]dictEntry) [][]byte {
	out := make([][]byte, len(entries))
	for i, e := range entries {
		out[i] = writeDict(e)
	}
	return out
}

func subrBias(n int) int {
	switch {
	case n < 1240:
		return 107
	case n < 33900:
		return 1131
	}
	return 32768
}

// standardSID is the string a code has in the Standard Encoding, which the
// accent composition of endchar names its parts by.
func standardSID(code int) int {
	if code >= 32 && code <= 126 {
		return code - 31
	}
	high := []int{
		161, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175,
		177, 178, 179, 180, 182, 183, 184, 185, 186, 187, 188, 189, 191,
		193, 194, 195, 196, 197, 198, 199, 200, 202, 203, 205, 206, 207, 208,
		225, 227, 232, 233, 234, 235, 241, 245, 248, 249, 250, 251,
	}
	if i := slices.Index(high, code); i >= 0 {
		return 96 + i
	}
	return -1
}

// charstringRun follows a charstring and the subroutines it calls, to
// record which subroutines a glyph needs and which glyphs it is composed of.
type charstringRun struct {
	gsubrs, lsubrs [][]byte
	usedG, usedL   map[int]bool
	stack          []int
	stems          int
	steps          int
	seac           bool
	base, accent   int
	ended          bool
}

func (r *charstringRun) run(code []byte, depth int) error {
	if depth > maxCharstringDepth {
		return ErrCorrupt
	}
	for i := 0; i < len(code) && !r.ended; i++ {
		if r.steps++; r.steps > maxCharstringSteps {
			return ErrCorrupt
		}
		b := code[i]
		switch {
		case b == 28:
			if i+3 > len(code) {
				return ErrCorrupt
			}
			r.stack = append(r.stack, int(int16(big.Uint16(code[i+1:]))))
			i += 2
		case b >= 32 && b <= 246:
			r.stack = append(r.stack, int(b)-139)
		case b >= 247 && b <= 250:
			if i+2 > len(code) {
				return ErrCorrupt
			}
			r.stack = append(r.stack, (int(b)-247)*256+int(code[i+1])+108)
			i++
		case b >= 251 && b <= 254:
			if i+2 > len(code) {
				return ErrCorrupt
			}
			r.stack = append(r.stack, -(int(b)-251)*256-int(code[i+1])-108)
			i++
		case b == 255:
			if i+5 > len(code) {
				return ErrCorrupt
			}
			r.stack = append(r.stack, int(int32(big.Uint32(code[i+1:]))>>16))
			i += 4
		case b == 10 || b == 29:
			if len(r.stack) == 0 {
				return ErrCorrupt
			}
			idx := r.stack[len(r.stack)-1]
			r.stack = r.stack[:len(r.stack)-1]
			subrs, used := r.lsubrs, r.usedL
			if b == 29 {
				subrs, used = r.gsubrs, r.usedG
			}
			idx += subrBias(len(subrs))
			if idx < 0 || idx >= len(subrs) {
				return ErrCorrupt
			}
			used[idx] = true
			if err := r.run(subrs[idx], depth+1); err != nil {
				return err
			}
		case b == 11:
			return nil
		case b == 14:
			if n := len(r.stack); n >= 4 {
				r.seac, r.base, r.accent = true, r.stack[n-2], r.stack[n-1]
			}
			r.ended = true
		case b == 1 || b == 3 || b == 18 || b == 23:
			r.stems += len(r.stack) / 2
			r.stack = r.stack[:0]
		case b == 19 || b == 20:
			r.stems += len(r.stack) / 2
			r.stack = r.stack[:0]
			i += (r.stems + 7) / 8
		case b == 12:
			i++
			r.stack = r.stack[:0]
		default:
			r.stack = r.stack[:0]
		}
	}
	return nil
}

var returnOnly = []byte{11}

// subsetCFF keeps the glyph numbers, so that cmap, hmtx and the charset stay
// as they are, and empties every glyph the characters do not need: its
// charstring becomes endchar. Subroutines no kept glyph calls become return.
func subsetCFF(src tableSet, runes []rune) (map[string][]byte, error) {
	head, _ := src.get("head")
	maxp, _ := src.get("maxp")
	cmap, _ := src.get("cmap")
	raw, _ := src.get("CFF ")
	if len(head) < 54 || len(maxp) < 6 {
		return nil, ErrCorrupt
	}
	f, err := parseCFF(raw)
	if err != nil {
		return nil, err
	}
	numGlyphs := int(big.Uint16(maxp[4:]))
	if numGlyphs != len(f.charstrings) {
		return nil, ErrCorrupt
	}
	chars, err := parseCmap(cmap)
	if err != nil {
		return nil, err
	}
	used := map[rune]uint16{}
	keep := map[int]bool{0: true}
	for _, r := range runes {
		if g, ok := chars[r]; ok && int(g) < numGlyphs {
			used[r] = g
			keep[int(g)] = true
		}
	}
	usedG := map[int]bool{}
	usedL := make([]map[int]bool, len(f.privates))
	for i := range usedL {
		usedL[i] = map[int]bool{}
	}
	queue := make([]int, 0, len(keep))
	for g := range keep {
		queue = append(queue, g)
	}
	for len(queue) > 0 {
		g := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		fd := 0
		if f.cid {
			fd = int(f.fdOf[g])
		}
		run := &charstringRun{gsubrs: f.gsubrs, lsubrs: f.privates[fd].subrs, usedG: usedG, usedL: usedL[fd]}
		if err := run.run(f.charstrings[g], 0); err != nil {
			return nil, err
		}
		if !run.seac || f.cid {
			continue
		}
		for _, code := range []int{run.base, run.accent} {
			sid := standardSID(code)
			if sid < 0 || f.sids == nil {
				return nil, ErrUnsupported
			}
			part := slices.Index(f.sids, uint16(sid))
			if part < 0 {
				return nil, ErrCorrupt
			}
			if !keep[part] {
				keep[part] = true
				queue = append(queue, part)
			}
		}
	}

	// Glyphs after the last one kept go altogether.
	count := 1
	for g := range keep {
		count = max(count, g+1)
	}
	out := *f
	out.charstrings = make([][]byte, count)
	if f.charsetRaw != nil {
		out.charsetRaw = append([]byte{0}, nil...)
		for _, sid := range f.sids[1:count] {
			out.charsetRaw = append(out.charsetRaw, byte(sid>>8), byte(sid))
		}
	}
	if f.cid {
		out.fdOf = f.fdOf[:count]
		out.fdselRaw = append([]byte{0}, out.fdOf...)
	}
	for g := range out.charstrings {
		if keep[g] {
			out.charstrings[g] = f.charstrings[g]
		} else {
			out.charstrings[g] = []byte{14}
		}
	}
	out.strs = f.keepStrings(keep)
	stub := func(subrs [][]byte, used map[int]bool) [][]byte {
		s := make([][]byte, len(subrs))
		for i := range subrs {
			if used[i] {
				s[i] = subrs[i]
			} else {
				s[i] = returnOnly
			}
		}
		return s
	}
	out.gsubrs = stub(f.gsubrs, usedG)
	out.privates = slices.Clone(f.privates)
	for i := range out.privates {
		out.privates[i].subrs = stub(f.privates[i].subrs, usedL[i])
	}

	newCmap, err := buildCmap(used)
	if err != nil {
		return nil, err
	}
	hhea, _ := src.get("hhea")
	hmtx, _ := src.get("hmtx")
	if len(hhea) < 36 {
		return nil, ErrCorrupt
	}
	metrics := int(big.Uint16(hhea[34:]))
	if metrics == 0 || metrics > numGlyphs || len(hmtx) < 4*metrics+2*(numGlyphs-metrics) {
		return nil, ErrCorrupt
	}
	newHhea, newMaxp := bytes.Clone(hhea), bytes.Clone(maxp)
	newMetrics := min(metrics, count)
	big.PutUint16(newHhea[34:], uint16(newMetrics))
	big.PutUint16(newMaxp[4:], uint16(count))
	tables := map[string][]byte{
		"head": bytes.Clone(head), "maxp": newMaxp, "cmap": newCmap, "CFF ": out.assemble(),
		"hhea": newHhea, "hmtx": hmtx[:4*newMetrics+2*(count-newMetrics)],
	}
	for _, tag := range kept {
		if d, ok := src.get(tag); ok {
			tables[tag] = d
		}
	}
	if post, ok := src.get("post"); ok && len(post) >= 32 {
		p := bytes.Clone(post[:32])
		big.PutUint32(p, 0x00030000)
		tables["post"] = p
	}
	return tables, nil
}

// keepStrings empties the strings nothing refers to: the names of the
// glyphs that were emptied. Their numbers, which charset and the DICTs
// hold, stay.
func (f *cffFont) keepStrings(keep map[int]bool) [][]byte {
	const standard = 391
	used := map[int]bool{}
	mark := func(entries []dictEntry, ops map[int]int) {
		for _, e := range entries {
			n, ok := ops[e.op]
			if !ok {
				continue
			}
			for i, o := range e.operands {
				if v, isInt := operandInt(o); isInt && i < n {
					used[v-standard] = true
				}
			}
		}
	}
	sidOps := map[int]int{0: 1, 1: 1, 2: 1, 3: 1, 4: 1, 0x0C15: 1, 0x0C16: 1, 0x0C1E: 2, 0x0C26: 1}
	mark(f.top, sidOps)
	for _, d := range f.fontDicts {
		mark(d, sidOps)
	}
	for g := range keep {
		if g < len(f.sids) {
			used[int(f.sids[g])-standard] = true
		}
	}
	out := make([][]byte, len(f.strs))
	for i, s := range f.strs {
		if used[i] {
			out[i] = s
		}
	}
	return out
}
