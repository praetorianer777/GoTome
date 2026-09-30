package mobi

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// palmDOC appends the decompressed record to out. The scheme is LZ77 with
// the window over everything decompressed so far in this record.
func palmDOC(out, rec []byte, limit int) ([]byte, error) {
	start := len(out)
	for i := 0; i < len(rec); {
		c := rec[i]
		i++
		switch {
		case c == 0 || (c >= 0x09 && c <= 0x7F):
			out = append(out, c)
		case c <= 0x08:
			// The next c bytes as they are.
			n := min(int(c), len(rec)-i)
			out = append(out, rec[i:i+n]...)
			i += n
		case c >= 0xC0:
			// A space and a character in one byte.
			out = append(out, ' ', c^0x80)
		default:
			if i >= len(rec) {
				return out, nil
			}
			pair := int(c)<<8 | int(rec[i])
			i++
			distance := (pair & 0x3FFF) >> 3
			length := pair&7 + 3
			if distance == 0 || distance > len(out)-start {
				return nil, fmt.Errorf("%w: a back reference points before the record", ErrNotMOBI)
			}
			// Byte by byte: the copy may overlap what it produces.
			from := len(out) - distance
			for k := range length {
				out = append(out, out[from+k])
			}
		}
		if len(out) > limit {
			return nil, fmt.Errorf("%w: more than %d bytes of text", ErrTooLarge, limit)
		}
	}
	return out, nil
}

// maxHuffDepth bounds how deep dictionary entries may refer to one another.
const maxHuffDepth = 32

// huffReader decompresses HUFF/CDIC records: a Huffman code whose symbols
// are phrases of a dictionary, and a phrase may itself be compressed.
type huffReader struct {
	// codes is indexed by the first byte of a code: its length, whether that
	// length is final, and the largest code of that length.
	codes [256]struct {
		length int
		final  bool
		max    uint64
	}
	minCode  [33]uint64
	maxCode  [33]uint64
	phrases  []phrase
	limit    int
	produced int
}

type phrase struct {
	data []byte
	// literal phrases are text; the others are compressed with the same code.
	literal bool
	// expanding is set while a phrase is being decompressed, so a phrase that
	// contains itself is found out instead of recursing for ever.
	expanding bool
}

var errBadHuffman = errors.New("the Huffman tables are broken")

func (h *header) huffman(limits Limits) (*huffReader, error) {
	first, count := h.u32(0x70), h.u32(0x74)
	if count < 2 || first == noIndex || count > uint32(len(h.db.records)) {
		return nil, fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	r := &huffReader{limit: limits.MaxTextBytes}
	if err := r.loadHUFF(h.db.record(first)); err != nil {
		return nil, err
	}
	for i := uint32(1); i < count; i++ {
		if err := r.loadCDIC(h.db.record(first + i)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *huffReader) loadHUFF(rec []byte) error {
	if len(rec) < 24 || string(rec[:8]) != "HUFF\x00\x00\x00\x18" {
		return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	off1, off2 := int(binary.BigEndian.Uint32(rec[8:])), int(binary.BigEndian.Uint32(rec[12:]))
	if off1 < 0 || off1+256*4 > len(rec) || off2 < 0 || off2+64*4 > len(rec) {
		return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	for i := range 256 {
		v := binary.BigEndian.Uint32(rec[off1+4*i:])
		length := int(v & 0x1F)
		if length == 0 {
			return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		r.codes[i].length = length
		r.codes[i].final = v&0x80 != 0
		r.codes[i].max = (uint64(v>>8)+1)<<(32-length) - 1
	}
	for length := 1; length <= 32; length++ {
		lo := binary.BigEndian.Uint32(rec[off2+8*(length-1):])
		hi := binary.BigEndian.Uint32(rec[off2+8*(length-1)+4:])
		r.minCode[length] = uint64(lo) << (32 - length)
		r.maxCode[length] = (uint64(hi)+1)<<(32-length) - 1
	}
	return nil
}

func (r *huffReader) loadCDIC(rec []byte) error {
	if len(rec) < 16 || string(rec[:8]) != "CDIC\x00\x00\x00\x10" {
		return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	total := int(binary.BigEndian.Uint32(rec[8:]))
	bits := binary.BigEndian.Uint32(rec[12:])
	if bits > 16 {
		return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	n := min(1<<bits, total-len(r.phrases))
	for i := range max(n, 0) {
		if 16+2*i+2 > len(rec) {
			return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		off := 16 + int(binary.BigEndian.Uint16(rec[16+2*i:]))
		if off+2 > len(rec) {
			return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		v := binary.BigEndian.Uint16(rec[off:])
		size := int(v & 0x7FFF)
		if off+2+size > len(rec) {
			return fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		r.phrases = append(r.phrases, phrase{data: rec[off+2 : off+2+size], literal: v&0x8000 != 0})
	}
	return nil
}

func (r *huffReader) decode(out, data []byte) ([]byte, error) {
	return r.unpack(out, data, 0)
}

func (r *huffReader) unpack(out, data []byte, depth int) ([]byte, error) {
	if depth > maxHuffDepth {
		return nil, fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
	}
	// The code is read through a window of 64 bits that moves on by 32 at a
	// time; the padding lets the last one be read whole.
	padded := make([]byte, len(data)+8)
	copy(padded, data)
	bitsLeft := len(data) * 8
	pos := 0
	x := binary.BigEndian.Uint64(padded)
	n := 32
	for {
		if n <= 0 {
			pos += 4
			if pos+8 > len(padded) {
				return out, nil
			}
			x = binary.BigEndian.Uint64(padded[pos:])
			n += 32
		}
		code := (x >> uint(n)) & 0xFFFFFFFF
		entry := r.codes[code>>24]
		length, maxCode := entry.length, entry.max
		if !entry.final {
			for length < 32 && code < r.minCode[length] {
				length++
			}
			maxCode = r.maxCode[length]
		}
		n -= length
		bitsLeft -= length
		if bitsLeft < 0 {
			return out, nil
		}
		if maxCode < code {
			return nil, fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		index := (maxCode - code) >> uint(32-length)
		if index >= uint64(len(r.phrases)) {
			return nil, fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
		}
		p := &r.phrases[index]
		if !p.literal {
			if p.expanding {
				return nil, fmt.Errorf("%w: %v", ErrNotMOBI, errBadHuffman)
			}
			p.expanding = true
			expanded, err := r.unpack(nil, p.data, depth+1)
			p.expanding = false
			if err != nil {
				return nil, err
			}
			p.data, p.literal = expanded, true
		}
		out = append(out, p.data...)
		r.produced += len(p.data)
		if r.produced > r.limit {
			return nil, fmt.Errorf("%w: more than %d bytes of text", ErrTooLarge, r.limit)
		}
	}
}
