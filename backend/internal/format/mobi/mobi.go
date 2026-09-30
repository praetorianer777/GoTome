// Package mobi reads Mobipocket files, MOBI and the Kindle formats built on
// it (AZW, AZW3): metadata, cover and the text. Like the EPUB reader it writes
// nothing and trusts nothing: every offset in the file is checked before it is
// followed, and decompression stops at a limit.
package mobi

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

var (
	// ErrNotMOBI is returned for a file that is no Mobipocket book.
	ErrNotMOBI = errors.New("not a MOBI file")
	// ErrTooLarge is returned when a file exceeds a limit.
	ErrTooLarge = errors.New("MOBI exceeds a size limit")
)

// Limits bound what reading one file may cost.
type Limits struct {
	// MaxFileBytes is the size of file that is read at all.
	MaxFileBytes int64
	// MaxTextBytes is the decompressed text of the whole book.
	MaxTextBytes int
}

// DefaultLimits are far above any real book.
var DefaultLimits = Limits{MaxFileBytes: 512 << 20, MaxTextBytes: 128 << 20}

// Book is what one file holds.
type Book struct {
	Metadata Metadata
	// Text is the book's text without markup. Paragraphs are separated by a
	// blank line. Empty when the content is encrypted.
	Text string
	// Cover is nil when the file names none.
	Cover *Cover
	// DRM reports that the text is encrypted. Metadata and cover are still
	// read: Kindle files keep both in the clear.
	DRM bool
	// KF8 is true for the newer Kindle format, AZW3.
	KF8 bool
	// ContentHash is the SHA-256 of the book's markup. The metadata lives in
	// another record, so two files that differ only in it have the same one.
	// Nil when the content is encrypted.
	ContentHash []byte
}

// Metadata is what the file says about the book.
type Metadata struct {
	Title        string
	Authors      []string
	Contributors []string
	Publisher    string
	// Description has its markup removed.
	Description string
	// Language is a BCP 47 tag, when the file gives one.
	Language string
	// Published is the date as written.
	Published string
	Subjects  []string
	ISBN      string
	ASIN      string
}

// Cover is the cover image as stored in the file.
type Cover struct {
	MediaType string
	Data      []byte
}

// Compression schemes of the text records.
const (
	compressionNone    = 1
	compressionPalmDOC = 2
	compressionHuffman = 17480
)

// EXTH record types.
const (
	exthAuthor      = 100
	exthPublisher   = 101
	exthDescription = 103
	exthISBN        = 104
	exthSubject     = 105
	exthPublished   = 106
	exthContributor = 108
	exthASIN        = 113
	exthCoverOffset = 201
	exthTitle       = 503
	exthLanguage    = 524
)

// noIndex is what the format writes for a record number that is not there.
const noIndex = 0xFFFFFFFF

// Parse reads a file under DefaultLimits.
func Parse(r io.ReaderAt, size int64) (*Book, error) {
	return ParseWithLimits(r, size, DefaultLimits)
}

// ParseWithLimits reads a file.
func ParseWithLimits(r io.ReaderAt, size int64, limits Limits) (*Book, error) {
	if size > limits.MaxFileBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, size)
	}
	data := make([]byte, size)
	if _, err := r.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	db, err := openPalmDB(data)
	if err != nil {
		return nil, err
	}
	h, err := readHeader(db)
	if err != nil {
		return nil, err
	}

	book := &Book{KF8: h.version >= 8, DRM: h.encryption != 0}
	exth := h.exth()
	book.Metadata = h.metadata(exth)
	book.Cover = h.cover(exth)
	if book.DRM {
		return book, nil
	}
	raw, err := h.text(limits)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	book.ContentHash = sum[:]
	book.Text, _ = markup.Text(raw)
	return book, nil
}

// palmDB is the container every Mobipocket file is: a list of records.
type palmDB struct {
	records [][]byte
}

func openPalmDB(data []byte) (*palmDB, error) {
	const headerLen = 78
	if len(data) < headerLen || string(data[60:68]) != "BOOKMOBI" {
		return nil, ErrNotMOBI
	}
	n := int(binary.BigEndian.Uint16(data[76:]))
	if n == 0 || headerLen+8*n > len(data) {
		return nil, fmt.Errorf("%w: record list does not fit the file", ErrNotMOBI)
	}
	offsets := make([]int, n+1)
	for i := range n {
		offsets[i] = int(binary.BigEndian.Uint32(data[headerLen+8*i:]))
	}
	offsets[n] = len(data)
	db := &palmDB{records: make([][]byte, n)}
	for i := range n {
		start, end := offsets[i], offsets[i+1]
		if start < headerLen+8*n || start > end || end > len(data) {
			return nil, fmt.Errorf("%w: record %d lies outside the file", ErrNotMOBI, i)
		}
		db.records[i] = data[start:end:end]
	}
	return db, nil
}

func (db *palmDB) record(i uint32) []byte {
	if i >= uint32(len(db.records)) {
		return nil
	}
	return db.records[i]
}

// header is record 0: the PalmDOC header, then the MOBI header.
type header struct {
	db          *palmDB
	rec0        []byte
	compression uint16
	textLength  uint32
	textRecords uint16
	encryption  uint16
	// mobiEnd is where the MOBI header ends in record 0. A field past it is
	// not in this file's version of the header.
	mobiEnd  int
	encoding uint32
	version  uint32
}

func readHeader(db *palmDB) (*header, error) {
	rec0 := db.record(0)
	if len(rec0) < 24 || string(rec0[16:20]) != "MOBI" {
		return nil, fmt.Errorf("%w: no MOBI header", ErrNotMOBI)
	}
	h := &header{
		db:          db,
		rec0:        rec0,
		compression: binary.BigEndian.Uint16(rec0[0:]),
		textLength:  binary.BigEndian.Uint32(rec0[4:]),
		textRecords: binary.BigEndian.Uint16(rec0[8:]),
		encryption:  binary.BigEndian.Uint16(rec0[12:]),
		mobiEnd:     min(len(rec0), 16+int(binary.BigEndian.Uint32(rec0[20:]))),
	}
	h.encoding = h.u32(0x1C)
	h.version = h.u32(0x24)
	return h, nil
}

// u32 reads a field of the MOBI header by its offset in record 0, or 0 when
// the header is too short to have it.
func (h *header) u32(off int) uint32 {
	if off+4 > h.mobiEnd {
		return 0
	}
	return binary.BigEndian.Uint32(h.rec0[off:])
}

func (h *header) u16(off int) uint16 {
	if off+2 > h.mobiEnd {
		return 0
	}
	return binary.BigEndian.Uint16(h.rec0[off:])
}

// exth returns the EXTH records by type, in the order they appear.
func (h *header) exth() map[uint32][][]byte {
	out := map[uint32][][]byte{}
	if h.u32(0x80)&0x40 == 0 {
		return out
	}
	data := h.rec0[h.mobiEnd:]
	if len(data) < 12 || string(data[:4]) != "EXTH" {
		return out
	}
	count := binary.BigEndian.Uint32(data[8:])
	pos := 12
	for range count {
		if pos+8 > len(data) {
			break
		}
		typ := binary.BigEndian.Uint32(data[pos:])
		size := int(binary.BigEndian.Uint32(data[pos+4:]))
		if size < 8 || size > len(data)-pos {
			break
		}
		out[typ] = append(out[typ], data[pos+8:pos+size])
		pos += size
	}
	return out
}

func (h *header) decode(b []byte) string {
	if h.encoding == 1252 {
		if s, err := charmap.Windows1252.NewDecoder().Bytes(b); err == nil {
			return strings.TrimSpace(string(s))
		}
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(b), string(utf8.RuneError)))
}

func (h *header) metadata(exth map[uint32][][]byte) Metadata {
	first := func(typ uint32) string {
		for _, v := range exth[typ] {
			if s := h.decode(v); s != "" {
				return s
			}
		}
		return ""
	}
	all := func(typ uint32) []string {
		var out []string
		for _, v := range exth[typ] {
			if s := h.decode(v); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	m := Metadata{
		Title:        first(exthTitle),
		Authors:      all(exthAuthor),
		Contributors: all(exthContributor),
		Publisher:    first(exthPublisher),
		Published:    first(exthPublished),
		Subjects:     all(exthSubject),
		ISBN:         first(exthISBN),
		ASIN:         first(exthASIN),
		Language:     first(exthLanguage),
	}
	if d := first(exthDescription); d != "" {
		m.Description, _ = markup.Text([]byte(d))
	}
	if m.Title == "" {
		off, n := int(h.u32(0x54)), int(h.u32(0x58))
		if off > 0 && n > 0 && off <= len(h.rec0) && n <= len(h.rec0)-off {
			m.Title = h.decode(h.rec0[off : off+n])
		}
	}
	if m.Language == "" {
		m.Language = localeLanguages[h.u32(0x5C)&0xFF]
	}
	return m
}

// localeLanguages maps the language part of the header's Windows locale to
// a language tag, for the languages books are most often in.
var localeLanguages = map[uint32]string{
	0x04: "zh", 0x07: "de", 0x09: "en", 0x0A: "es", 0x0C: "fr", 0x10: "it",
	0x11: "ja", 0x13: "nl", 0x15: "pl", 0x16: "pt", 0x19: "ru", 0x1D: "sv",
	0x06: "da", 0x14: "no", 0x0B: "fi", 0x0E: "hu", 0x05: "cs", 0x1F: "tr",
}

func (h *header) cover(exth map[uint32][][]byte) *Cover {
	values := exth[exthCoverOffset]
	first := h.u32(0x6C)
	if len(values) == 0 || len(values[0]) != 4 || first == noIndex {
		return nil
	}
	off := binary.BigEndian.Uint32(values[0])
	if off == noIndex || off > noIndex-first {
		return nil
	}
	data := h.db.record(first + off)
	media := imageType(data)
	if media == "" {
		return nil
	}
	return &Cover{MediaType: media, Data: data}
}

func imageType(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("GIF8")):
		return "image/gif"
	}
	return ""
}

// text returns the book's markup: the text records, each stripped of the
// data trailing it and decompressed, and for KF8 only the first flow, which
// is the book; the others are its style sheets and drawings.
func (h *header) text(limits Limits) ([]byte, error) {
	extra := uint16(0)
	if h.mobiEnd >= 0xE4+16 {
		extra = h.u16(0xF2)
	}
	var huff *huffReader
	if h.compression == compressionHuffman {
		var err error
		if huff, err = h.huffman(limits); err != nil {
			return nil, err
		}
	}

	var out []byte
	for i := uint32(1); i <= uint32(h.textRecords); i++ {
		rec := h.db.record(i)
		if rec == nil {
			break
		}
		rec = rec[:len(rec)-trailingSize(rec, extra)]
		var err error
		switch h.compression {
		case compressionNone:
			out = append(out, rec...)
		case compressionPalmDOC:
			out, err = palmDOC(out, rec, limits.MaxTextBytes)
		case compressionHuffman:
			out, err = huff.decode(out, rec)
		default:
			return nil, fmt.Errorf("%w: unknown compression %d", ErrNotMOBI, h.compression)
		}
		if err != nil {
			return nil, err
		}
		if len(out) > limits.MaxTextBytes {
			return nil, fmt.Errorf("%w: more than %d bytes of text", ErrTooLarge, limits.MaxTextBytes)
		}
	}
	if h.textLength > 0 && int64(h.textLength) < int64(len(out)) {
		out = out[:h.textLength]
	}
	if h.version >= 8 {
		out = h.firstFlow(out)
	}
	if h.encoding == 1252 {
		if decoded, err := charmap.Windows1252.NewDecoder().Bytes(out); err == nil {
			out = decoded
		}
	}
	return out, nil
}

// firstFlow cuts a KF8 book's markup down to its first flow, as the FDST
// record lists them. Without that record the markup is the one flow.
func (h *header) firstFlow(raw []byte) []byte {
	fdst := h.db.record(h.u32(0xC0))
	if len(fdst) < 20 || string(fdst[:4]) != "FDST" {
		return raw
	}
	off := int(binary.BigEndian.Uint32(fdst[4:]))
	if off+8 > len(fdst) || off < 12 {
		return raw
	}
	start, end := binary.BigEndian.Uint32(fdst[off:]), binary.BigEndian.Uint32(fdst[off+4:])
	if start > end || int64(end) > int64(len(raw)) {
		return raw
	}
	return raw[start:end]
}

// trailingSize is how many bytes at the end of a text record are not text.
// Each bit of flags above the lowest announces an entry that ends with its
// own size, written backwards; the lowest bit, bytes of a character that
// continues into the next record.
func trailingSize(rec []byte, flags uint16) int {
	size := 0
	for f := flags >> 1; f != 0; f >>= 1 {
		if f&1 == 0 {
			continue
		}
		n, value := 0, 0
		for pos := len(rec) - size - 1; pos >= 0; pos-- {
			b := rec[pos]
			value |= int(b&0x7F) << (7 * n)
			n++
			if b&0x80 != 0 || n >= 4 {
				break
			}
		}
		size += value
		if size > len(rec) {
			return len(rec)
		}
	}
	if flags&1 != 0 && size < len(rec) {
		size += int(rec[len(rec)-size-1]&0x3) + 1
	}
	return min(size, len(rec))
}
