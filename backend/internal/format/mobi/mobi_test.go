package mobi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// fixture describes a Mobipocket file for build to write. The files are made
// here because no tool to make them runs in the gate; mobitool, which reads
// them, checks that they are what the format says.
type fixture struct {
	fullName    string
	exth        []exthRecord
	markup      string
	compression int
	encoding    uint32
	locale      uint32
	cover       []byte
	encryption  uint16
	extraFlags  uint16
	recordSize  int
	// css makes the file KF8, with the markup and the style sheet as two flows.
	css string
}

type exthRecord struct {
	typ   uint32
	value []byte
}

func text(typ uint32, s string) exthRecord { return exthRecord{typ, []byte(s)} }

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func (f fixture) build(t testing.TB) []byte {
	t.Helper()
	if f.recordSize == 0 {
		f.recordSize = 4096
	}
	if f.encoding == 0 {
		f.encoding = 65001
	}
	raw := []byte(f.markup)
	if f.encoding == 1252 {
		raw = toCP1252(t, f.markup)
	}
	flowEnd := len(raw)
	raw = append(raw, f.css...)

	var records [][]byte
	records = append(records, nil) // record 0, written last
	var huffRecords [][]byte
	var huff *testHuff
	if f.compression == compressionHuffman {
		huff = newTestHuff()
		huffRecords = huff.records()
	}
	textRecords := 0
	for start := 0; start < len(raw); start += f.recordSize {
		chunk := raw[start:min(start+f.recordSize, len(raw))]
		var rec []byte
		switch f.compression {
		case compressionNone:
			rec = slices.Clone(chunk)
		case compressionPalmDOC:
			rec = compressPalmDOC(chunk)
		case compressionHuffman:
			rec = huff.encode(chunk)
		}
		if f.extraFlags&1 != 0 {
			// No character runs over into the next record.
			rec = append(rec, 0x00)
		}
		if f.extraFlags&2 != 0 {
			// An entry of three bytes and the byte that gives its size.
			rec = append(rec, 'x', 'y', 'z', 0x84)
		}
		records = append(records, rec)
		textRecords++
	}
	huffIndex := uint32(noIndex)
	if huff != nil {
		huffIndex = uint32(len(records))
		records = append(records, huffRecords...)
	}
	firstImage := uint32(noIndex)
	exth := f.exth
	if f.cover != nil {
		firstImage = uint32(len(records))
		records = append(records, f.cover)
		exth = append(exth, exthRecord{exthCoverOffset, u32(0)})
	}
	fdst := uint32(noIndex)
	if f.css != "" {
		fdst = uint32(len(records))
		rec := []byte("FDST")
		rec = append(rec, u32(12)...)
		rec = append(rec, u32(2)...)
		rec = append(rec, u32(0)...)
		rec = append(rec, u32(uint32(flowEnd))...)
		rec = append(rec, u32(uint32(flowEnd))...)
		rec = append(rec, u32(uint32(len(raw)))...)
		records = append(records, rec)
	}
	records = append(records, []byte{0xE9, 0x8E, 0x0D, 0x0A}) // end of file

	const mobiLen = 0xE8
	rec0 := make([]byte, 16+mobiLen)
	binary.BigEndian.PutUint16(rec0[0:], uint16(f.compression))
	binary.BigEndian.PutUint32(rec0[4:], uint32(len(raw)))
	binary.BigEndian.PutUint16(rec0[8:], uint16(textRecords))
	binary.BigEndian.PutUint16(rec0[10:], uint16(f.recordSize))
	binary.BigEndian.PutUint16(rec0[12:], f.encryption)
	copy(rec0[16:], "MOBI")
	binary.BigEndian.PutUint32(rec0[0x14:], mobiLen)
	binary.BigEndian.PutUint32(rec0[0x18:], 2)
	binary.BigEndian.PutUint32(rec0[0x1C:], f.encoding)
	binary.BigEndian.PutUint32(rec0[0x20:], 0x1234)
	version := uint32(6)
	if f.css != "" {
		version = 8
	}
	binary.BigEndian.PutUint32(rec0[0x24:], version)
	for off := 0x28; off < 0x50; off += 4 {
		binary.BigEndian.PutUint32(rec0[off:], noIndex)
	}
	binary.BigEndian.PutUint32(rec0[0x50:], uint32(textRecords+1))
	binary.BigEndian.PutUint32(rec0[0x5C:], f.locale)
	binary.BigEndian.PutUint32(rec0[0x68:], version)
	binary.BigEndian.PutUint32(rec0[0x6C:], firstImage)
	binary.BigEndian.PutUint32(rec0[0x70:], huffIndex)
	if huff != nil {
		binary.BigEndian.PutUint32(rec0[0x74:], uint32(len(huffRecords)))
	}
	binary.BigEndian.PutUint32(rec0[0x80:], 0x40)
	if f.css != "" {
		binary.BigEndian.PutUint32(rec0[0xC0:], fdst)
	} else {
		binary.BigEndian.PutUint16(rec0[0xC0:], 1)
		binary.BigEndian.PutUint16(rec0[0xC2:], uint16(textRecords))
	}
	binary.BigEndian.PutUint32(rec0[0xE4:], noIndex)
	binary.BigEndian.PutUint16(rec0[0xF2:], f.extraFlags)

	ex := []byte("EXTH")
	var body []byte
	for _, r := range exth {
		body = append(body, u32(r.typ)...)
		body = append(body, u32(uint32(8+len(r.value)))...)
		body = append(body, r.value...)
	}
	ex = append(ex, u32(uint32(12+len(body)))...)
	ex = append(ex, u32(uint32(len(exth)))...)
	ex = append(ex, body...)
	for len(ex)%4 != 0 {
		ex = append(ex, 0)
	}
	rec0 = append(rec0, ex...)
	binary.BigEndian.PutUint32(rec0[0x54:], uint32(len(rec0)))
	binary.BigEndian.PutUint32(rec0[0x58:], uint32(len(f.fullName)))
	rec0 = append(rec0, f.fullName...)
	rec0 = append(rec0, 0, 0)
	for len(rec0)%4 != 0 {
		rec0 = append(rec0, 0)
	}
	records[0] = rec0

	header := make([]byte, 78)
	copy(header, "Test Book")
	copy(header[60:], "BOOKMOBI")
	binary.BigEndian.PutUint16(header[76:], uint16(len(records)))
	offset := 78 + 8*len(records) + 2
	for i, r := range records {
		header = append(header, u32(uint32(offset))...)
		header = append(header, 0, byte(i>>16), byte(i>>8), byte(i))
		offset += len(r)
	}
	header = append(header, 0, 0)
	for _, r := range records {
		header = append(header, r...)
	}
	return header
}

func toCP1252(t testing.TB, s string) []byte {
	t.Helper()
	var out []byte
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		case r == 0x2019:
			out = append(out, 0x92)
		default:
			t.Fatalf("%q has no place in the fixture's code page", r)
		}
	}
	return out
}

// compressPalmDOC is a plain encoder for the fixtures: back references where
// the text repeats, a space and a letter in one byte, and runs of bytes that
// need escaping.
func compressPalmDOC(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		best, bestDist := 0, 0
		for d := 1; d <= min(i, 2047); d++ {
			n := 0
			for n < 10 && i+n < len(src) && src[i+n] == src[i-d+n] {
				n++
			}
			if n > best {
				best, bestDist = n, d
			}
		}
		c := src[i]
		switch {
		case best >= 3:
			v := 0x8000 | bestDist<<3 | (best - 3)
			out = append(out, byte(v>>8), byte(v))
			i += best
		case c == ' ' && i+1 < len(src) && src[i+1] >= 0x40 && src[i+1] <= 0x7F:
			out = append(out, src[i+1]^0x80)
			i += 2
		case c == 0 || (c >= 0x09 && c <= 0x7F):
			out = append(out, c)
			i++
		default:
			n := 0
			for n < 8 && i+n < len(src) && (src[i+n] >= 0x80 || (src[i+n] >= 1 && src[i+n] <= 8)) {
				n++
			}
			out = append(out, byte(n))
			out = append(out, src[i:i+n]...)
			i += n
		}
	}
	return out
}

// testHuff is a HUFF/CDIC code of eight bits: every byte of the compressed
// text names a phrase. Phrase 255-b is the byte b, except that the code
// byte 0x00 names a compressed phrase, "the ", to exercise the expansion.
type testHuff struct{}

func newTestHuff() *testHuff { return &testHuff{} }

func (h *testHuff) records() [][]byte {
	// The two tables, then the same two little-endian, as Kindle tools write
	// them and libmobi expects the record to be long enough for.
	huff := []byte("HUFF\x00\x00\x00\x18")
	huff = append(huff, u32(24)...)
	huff = append(huff, u32(24+1024)...)
	huff = append(huff, u32(24+1024+256)...)
	huff = append(huff, u32(24+1024+256+1024)...)
	for range 256 {
		huff = append(huff, u32(255<<8|0x80|8)...)
	}
	huff = append(huff, make([]byte, 64*4)...)
	for range 256 {
		huff = binary.LittleEndian.AppendUint32(huff, 255<<8|0x80|8)
	}
	huff = append(huff, make([]byte, 64*4)...)

	cdic := []byte("CDIC\x00\x00\x00\x10")
	cdic = append(cdic, u32(256)...)
	cdic = append(cdic, u32(8)...)
	var table, phrases []byte
	for r := range 256 {
		table = binary.BigEndian.AppendUint16(table, uint16(2*256+len(phrases)))
		var data []byte
		flag := uint16(0x8000)
		if r == 255 {
			// The code 0x00: "the ", itself compressed.
			for _, b := range []byte("the ") {
				data = append(data, 255-b)
			}
			flag = 0
		} else {
			data = []byte{byte(r)}
		}
		phrases = binary.BigEndian.AppendUint16(phrases, flag|uint16(len(data)))
		phrases = append(phrases, data...)
	}
	cdic = append(cdic, table...)
	cdic = append(cdic, phrases...)
	return [][]byte{huff, cdic}
}

func (h *testHuff) encode(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		if bytes.HasPrefix(src[i:], []byte("the ")) {
			out = append(out, 0x00)
			i += 4
			continue
		}
		out = append(out, 255-src[i])
		i++
	}
	return out
}

func coverJPEG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 60, 90))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(1, 1, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const persuasion = `<html><head><guide></guide></head><body>` +
	`<h1>Chapter 1</h1><p>Sir Walter Elliot, of Kellynch Hall, in Somersetshire, was a man who, for his own amusement, never took up any book but the Baronetage; there he found occupation for an idle hour, and consolation in a distressed one.</p>` +
	`<mbp:pagebreak/><h1>Chapter 2</h1><p>Mr Shepherd, a civil, cautious lawyer, who, whatever might be his hold or his views on Sir Walter, would rather have the disagreeable prompted by anybody else, excused himself from offering the slightest hint.</p>` +
	`</body></html>`

const persuasionText = "Chapter 1\n\n" +
	"Sir Walter Elliot, of Kellynch Hall, in Somersetshire, was a man who, for his own amusement, never took up any book but the Baronetage; there he found occupation for an idle hour, and consolation in a distressed one.\n\n" +
	"Chapter 2\n\n" +
	"Mr Shepherd, a civil, cautious lawyer, who, whatever might be his hold or his views on Sir Walter, would rather have the disagreeable prompted by anybody else, excused himself from offering the slightest hint."

func persuasionFixture(t testing.TB) fixture {
	return fixture{
		fullName: "Persuasion (Penguin)",
		exth: []exthRecord{
			text(exthTitle, "Persuasion"),
			text(exthAuthor, "Jane Austen"),
			text(exthContributor, "calibre (5.0) [https://calibre-ebook.com]"),
			text(exthPublisher, "Penguin"),
			text(exthDescription, "<p>Her <b>last</b> novel.</p>"),
			text(exthISBN, "978-0-14-143968-6"),
			text(exthASIN, "B000JQUO4C"),
			text(exthSubject, "Fiction"),
			text(exthSubject, "Romance"),
			text(exthPublished, "1817-12-20T00:00:00+00:00"),
			text(exthLanguage, "en"),
		},
		markup:      persuasion,
		compression: compressionPalmDOC,
		cover:       coverJPEG(t),
		extraFlags:  3,
		recordSize:  100,
	}
}

func parse(t *testing.T, data []byte) *Book {
	t.Helper()
	book, err := Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return book
}

func TestMOBIWithPalmDOC(t *testing.T) {
	f := persuasionFixture(t)
	book := parse(t, f.build(t))
	want := Metadata{
		Title: "Persuasion", Authors: []string{"Jane Austen"},
		Contributors: []string{"calibre (5.0) [https://calibre-ebook.com]"},
		Publisher:    "Penguin", Description: "Her last novel.", Language: "en",
		Published: "1817-12-20T00:00:00+00:00", Subjects: []string{"Fiction", "Romance"},
		ISBN: "978-0-14-143968-6", ASIN: "B000JQUO4C",
	}
	if !equalMetadata(book.Metadata, want) {
		t.Errorf("metadata =\n%+v\nwant\n%+v", book.Metadata, want)
	}
	if book.Text != persuasionText {
		t.Errorf("text =\n%q\nwant\n%q", book.Text, persuasionText)
	}
	if book.Cover == nil || book.Cover.MediaType != "image/jpeg" || !bytes.Equal(book.Cover.Data, f.cover) {
		t.Errorf("cover = %+v", book.Cover)
	}
	if book.DRM || book.KF8 {
		t.Errorf("DRM %v, KF8 %v; want neither", book.DRM, book.KF8)
	}
}

func equalMetadata(a, b Metadata) bool {
	return a.Title == b.Title && slices.Equal(a.Authors, b.Authors) && slices.Equal(a.Contributors, b.Contributors) &&
		a.Publisher == b.Publisher && a.Description == b.Description && a.Language == b.Language &&
		a.Published == b.Published && slices.Equal(a.Subjects, b.Subjects) && a.ISBN == b.ISBN && a.ASIN == b.ASIN
}

func TestEveryCompressionYieldsTheSameText(t *testing.T) {
	for name, compression := range map[string]int{
		"none": compressionNone, "PalmDOC": compressionPalmDOC, "HUFF/CDIC": compressionHuffman,
	} {
		t.Run(name, func(t *testing.T) {
			for _, size := range []int{4096, 37} {
				f := fixture{fullName: "Persuasion", markup: persuasion, compression: compression, recordSize: size}
				if got := parse(t, f.build(t)).Text; got != persuasionText {
					t.Errorf("records of %d bytes: text =\n%q", size, got)
				}
			}
		})
	}
}

func TestOldFilesInWindows1252(t *testing.T) {
	f := fixture{
		fullName: "Caf\xe9", encoding: 1252, locale: 0x0C, compression: compressionPalmDOC,
		exth:   []exthRecord{{exthAuthor, []byte("Ren\xe9e")}},
		markup: "<p>Un caf\u00e9 cr\u00e8me, s\u2019il vous pla\u00eet.</p>",
	}
	book := parse(t, f.build(t))
	if book.Metadata.Title != "Caf\u00e9" || book.Metadata.Authors[0] != "Ren\u00e9e" || book.Metadata.Language != "fr" {
		t.Errorf("metadata = %+v", book.Metadata)
	}
	if book.Text != "Un caf\u00e9 cr\u00e8me, s\u2019il vous pla\u00eet." {
		t.Errorf("text = %q", book.Text)
	}
}

func TestKF8ReadsTheBookAndNotItsStyleSheet(t *testing.T) {
	f := fixture{
		fullName: "Persuasion", compression: compressionPalmDOC, recordSize: 64,
		markup: persuasion, css: "p { text-indent: 1.5em; } h1 { page-break-before: always; }",
	}
	book := parse(t, f.build(t))
	if !book.KF8 || book.Text != persuasionText {
		t.Errorf("KF8 %v, text =\n%q", book.KF8, book.Text)
	}
}

func TestProtectedFileKeepsMetadataAndCoverButNoText(t *testing.T) {
	f := persuasionFixture(t)
	f.encryption = 2
	book := parse(t, f.build(t))
	if !book.DRM || book.Text != "" || book.Metadata.Title != "Persuasion" || book.Cover == nil {
		t.Errorf("DRM %v, text %q, title %q, cover %v", book.DRM, book.Text, book.Metadata.Title, book.Cover != nil)
	}
}

func TestNotAMOBI(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":     nil,
		"text":      []byte(strings.Repeat("not a book ", 20)),
		"a zip":     append([]byte("PK\x03\x04"), make([]byte, 100)...),
		"no header": fixture{markup: "x", compression: compressionNone}.build(t)[:80],
	} {
		if _, err := Parse(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrNotMOBI) {
			t.Errorf("%s: err = %v, want ErrNotMOBI", name, err)
		}
	}
}

func TestLimits(t *testing.T) {
	f := fixture{fullName: "Long", markup: "<p>" + strings.Repeat("a", 5000) + "</p>", compression: compressionPalmDOC}
	data := f.build(t)
	limits := Limits{MaxFileBytes: 1 << 20, MaxTextBytes: 1000}
	if _, err := ParseWithLimits(bytes.NewReader(data), int64(len(data)), limits); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too much text: %v, want ErrTooLarge", err)
	}
	limits = Limits{MaxFileBytes: 100, MaxTextBytes: 1 << 20}
	if _, err := ParseWithLimits(bytes.NewReader(data), int64(len(data)), limits); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large a file: %v, want ErrTooLarge", err)
	}
}

// TestMobitoolReadsTheFixtures checks the fixtures, and with them this
// reader's idea of the format, against libmobi.
func TestMobitoolReadsTheFixtures(t *testing.T) {
	mobitool, err := exec.LookPath("mobitool")
	if err != nil {
		t.Skip("mobitool is not installed here; the toolchain image has it")
	}
	full := persuasionFixture(t)
	huff := full
	huff.compression = compressionHuffman
	kf8 := full
	kf8.css = "p { margin: 0 }"
	for name, f := range map[string]fixture{"PalmDOC": full, "HUFF/CDIC": huff, "KF8": kf8} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "book.mobi")
			if err := os.WriteFile(path, f.build(t), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(mobitool, path).CombinedOutput()
			if err != nil {
				t.Fatalf("mobitool: %v\n%s", err, out)
			}
			for _, want := range []string{"Persuasion", "Jane Austen", "Penguin", "9780141439686"} {
				if !strings.Contains(strings.ReplaceAll(string(out), "-", ""), strings.ReplaceAll(want, "-", "")) {
					t.Errorf("mobitool does not see %q:\n%s", want, out)
				}
			}
			if out, err := exec.Command(mobitool, "-d", "-o", dir, path).CombinedOutput(); err != nil {
				t.Fatalf("mobitool -d: %v\n%s", err, out)
			}
			rawml, err := os.ReadFile(filepath.Join(dir, "book.rawml"))
			if err != nil {
				t.Fatal(err)
			}
			if want := persuasion + f.css; string(rawml) != want {
				t.Errorf("mobitool decompresses\n%q\nwant\n%q", rawml, want)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	full := persuasionFixture(f)
	f.Add(full.build(f))
	huff := full
	huff.compression = compressionHuffman
	f.Add(huff.build(f))
	kf8 := full
	kf8.css = "p{}"
	f.Add(kf8.build(f))
	limits := Limits{MaxFileBytes: 1 << 20, MaxTextBytes: 1 << 20}
	f.Fuzz(func(t *testing.T, data []byte) {
		book, err := ParseWithLimits(bytes.NewReader(data), int64(len(data)), limits)
		if err != nil {
			return
		}
		if !utf8.ValidString(book.Text) || !utf8.ValidString(book.Metadata.Title) {
			t.Error("text that is not valid UTF-8")
		}
	})
}

// The compressed records are where the arithmetic is; fuzzed on their own,
// the mutations reach it without having to keep a whole file intact.
func FuzzDecompress(f *testing.F) {
	f.Add(compressPalmDOC([]byte(persuasion)), newTestHuff().encode([]byte(persuasion)))
	f.Fuzz(func(t *testing.T, palm, huffman []byte) {
		_, _ = palmDOC(nil, palm, 1<<20)
		records := newTestHuff().records()
		r := &huffReader{limit: 1 << 20}
		if err := r.loadHUFF(records[0]); err != nil {
			t.Fatal(err)
		}
		if err := r.loadCDIC(records[1]); err != nil {
			t.Fatal(err)
		}
		_, _ = r.decode(nil, huffman)
		_ = trailingSize(palm, binary.BigEndian.Uint16(append(slices.Clone(huffman), 0, 0)))
	})
}

var update = flag.Bool("update", false, "write the files in testdata anew")

// The files in testdata are the fixtures of other packages' tests, which
// cannot build them. They are what build makes, and stay so.
func TestFixturesInTestdata(t *testing.T) {
	persuasion := persuasionFixture(t)
	kf8 := persuasion
	kf8.css = "p { text-indent: 1.5em }"
	protected := persuasion
	protected.encryption = 2
	for name, f := range map[string]fixture{
		"persuasion.mobi": persuasion, "persuasion.azw3": kf8, "protected.azw": protected,
	} {
		path := filepath.Join("testdata", name)
		data := f.build(t)
		if *update {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		stored, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(stored, data) {
			t.Errorf("%s is not what the fixture builds (%v); run go test -run TestFixturesInTestdata -update", path, err)
		}
	}
}
