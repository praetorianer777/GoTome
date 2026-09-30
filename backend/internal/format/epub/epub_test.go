package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

// entry is one file of a test EPUB, in archive order.
type entry struct{ name, content string }

func build(t testing.TB, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func parse(t testing.TB, data []byte) *Book {
	t.Helper()
	book, err := Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return book
}

func container(opf string) entry {
	return entry{containerPath, `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="` + opf + `" media-type="application/oebps-package+xml"/></rootfiles>
</container>`}
}

const epub3OPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:isbn:978-3-16-148410-0</dc:identifier>
    <dc:identifier>urn:uuid:123e4567-e89b-12d3-a456-426614174000</dc:identifier>
    <dc:title id="t1">The Way of   Kings</dc:title>
    <meta refines="#t1" property="title-type">main</meta>
    <dc:title id="t2">Book One of the Stormlight Archive</dc:title>
    <meta refines="#t2" property="title-type">subtitle</meta>
    <dc:creator id="c1">Brandon Sanderson</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c1" property="file-as">Sanderson, Brandon</meta>
    <dc:contributor id="c2">Michael Kramer</dc:contributor>
    <meta refines="#c2" property="role" scheme="marc:relators">nrt</meta>
    <dc:language>en-US</dc:language>
    <dc:publisher>Tor Books</dc:publisher>
    <dc:date>2010-08-31</dc:date>
    <dc:description>&lt;p&gt;Roshar is a world of &lt;b&gt;stone&lt;/b&gt; &amp;amp; storms.&lt;/p&gt;</dc:description>
    <dc:subject>Fantasy</dc:subject>
    <dc:subject>Epic</dc:subject>
    <meta property="belongs-to-collection" id="s1">The Stormlight Archive</meta>
    <meta refines="#s1" property="collection-type">series</meta>
    <meta refines="#s1" property="group-position">1</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="text/ch%202.xhtml" media-type="application/xhtml+xml"/>
    <item id="img" href="images/front.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="css" href="style.css" media-type="text/css"/>
  </manifest>
  <spine>
    <itemref idref="nav"/>
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
    <itemref idref="ch1"/>
    <itemref idref="missing"/>
  </spine>
</package>`

func epub3(t testing.TB) []byte {
	return build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", epub3OPF},
		entry{"OEBPS/nav.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body>
			<nav epub:type="landmarks"><a href="text/ch1.xhtml">Start</a></nav>
			<nav epub:type="toc"><ol>
				<li><a href="text/ch1.xhtml">Prelude to the <em>Stormlight</em> Archive</a></li>
				<li><a href="text/ch1.xhtml#part2">A section inside it</a></li>
				<li><a href="text/ch%202.xhtml">Stormblessed</a></li>
			</ol></nav></body></html>`},
		entry{"OEBPS/text/ch1.xhtml", `<?xml version="1.0"?><html><head><title>ignored title</title>
			<style>p { color: red }</style><script>alert("x")</script></head>
			<body><h1>Prelude</h1>
			<p>Kalak rounded a rocky   stone ridge<br/>and stumbled.</p>
			<p>Caf&eacute; &amp; &#8220;quotes&#8221; stay; soft&shy;hyphens go.</p>
			<script type="text/javascript">document.write("never")</script>
			<svg><text>drawing label</text></svg>
			<table><tr><td>one</td><td>two</td></tr></table>
			</body></html>`},
		entry{"OEBPS/text/ch 2.xhtml", `<html><body><h2>Chapter heading</h2><p>Second chapter.</p></body></html>`},
		entry{"OEBPS/images/front.jpg", "\xff\xd8\xffcover-bytes"},
		entry{"OEBPS/style.css", "p { margin: 0 }"},
	)
}

func TestEPUB3Metadata(t *testing.T) {
	md := parse(t, epub3(t)).Metadata

	if md.Version != "3.0" || md.Title != "The Way of Kings" || md.Subtitle != "Book One of the Stormlight Archive" {
		t.Errorf("version %q, title %q, subtitle %q", md.Version, md.Title, md.Subtitle)
	}
	if got := md.Authors(); len(got) != 1 || got[0] != "Brandon Sanderson" {
		t.Errorf("authors = %v", got)
	}
	if len(md.Contributors) != 2 ||
		md.Contributors[0] != (Contributor{"Brandon Sanderson", "Sanderson, Brandon", "aut"}) ||
		md.Contributors[1] != (Contributor{"Michael Kramer", "", "nrt"}) {
		t.Errorf("contributors = %+v", md.Contributors)
	}
	wantIDs := []Identifier{{"isbn", "978-3-16-148410-0"}, {"uuid", "123e4567-e89b-12d3-a456-426614174000"}}
	if len(md.Identifiers) != 2 || md.Identifiers[0] != wantIDs[0] || md.Identifiers[1] != wantIDs[1] {
		t.Errorf("identifiers = %+v", md.Identifiers)
	}
	if md.Language != "en-US" || md.Publisher != "Tor Books" || md.Published != "2010-08-31" {
		t.Errorf("language %q, publisher %q, published %q", md.Language, md.Publisher, md.Published)
	}
	if md.Description != "Roshar is a world of stone & storms." {
		t.Errorf("description = %q", md.Description)
	}
	if strings.Join(md.Subjects, ",") != "Fantasy,Epic" {
		t.Errorf("subjects = %v", md.Subjects)
	}
	if md.Series != "The Stormlight Archive" || md.SeriesIndex == nil || *md.SeriesIndex != 1 {
		t.Errorf("series %q index %v", md.Series, md.SeriesIndex)
	}
}

func TestEPUB3TextAndChapters(t *testing.T) {
	book := parse(t, epub3(t))

	// The navigation document, the repeated itemref and the dangling one are
	// not chapters.
	if len(book.Chapters) != 2 {
		t.Fatalf("%d chapters, want 2: %+v", len(book.Chapters), book.Chapters)
	}
	ch1, ch2 := book.Chapters[0], book.Chapters[1]

	wantText := "Prelude\n\nKalak rounded a rocky stone ridge\nand stumbled.\n\n" +
		"Café & “quotes” stay; softhyphens go.\n\none two"
	if ch1.Text != wantText {
		t.Errorf("chapter 1 text:\n%q\nwant:\n%q", ch1.Text, wantText)
	}
	if ch1.Title != "Prelude to the Stormlight Archive" || ch1.Href != "OEBPS/text/ch1.xhtml" {
		t.Errorf("chapter 1 title %q href %q", ch1.Title, ch1.Href)
	}
	if ch2.Title != "Stormblessed" || ch2.Href != "OEBPS/text/ch 2.xhtml" || ch2.Text != "Chapter heading\n\nSecond chapter." {
		t.Errorf("chapter 2 = %+v", ch2)
	}

	whole := []rune(book.Text())
	for _, ch := range book.Chapters {
		end := ch.Offset + utf8.RuneCountInString(ch.Text)
		if end > len(whole) || string(whole[ch.Offset:end]) != ch.Text {
			t.Errorf("offset %d does not locate chapter %q in the whole text", ch.Offset, ch.Href)
		}
	}
	for _, leak := range []string{"alert", "color", "never", "drawing label", "ignored title", "<", "margin"} {
		if strings.Contains(book.Text(), leak) {
			t.Errorf("the text contains %q", leak)
		}
	}
}

func TestEPUB3Cover(t *testing.T) {
	cover := parse(t, epub3(t)).Cover
	if cover == nil || cover.MediaType != "image/jpeg" || !bytes.HasSuffix(cover.Data, []byte("cover-bytes")) {
		t.Errorf("cover = %+v", cover)
	}
}

const epub2OPF = `<?xml version="1.0" encoding="iso-8859-1"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="BookId">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>Die Verwandlung</dc:title>
    <dc:creator opf:role="aut" opf:file-as="Kafka, Franz">Franz Kafka</dc:creator>
    <dc:creator opf:role="trl">Jemand ` + "\xfc" + `bersetzt</dc:creator>
    <dc:identifier id="BookId" opf:scheme="ISBN">3150091004</dc:identifier>
    <dc:identifier>B000FC1PJI</dc:identifier>
    <dc:language>de</dc:language>
    <dc:date>1915</dc:date>
    <meta name="calibre:series" content="Erz` + "\xe4" + `hlungen"/>
    <meta name="calibre:series_index" content="2.5"/>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="cover-img" href="Cover.PNG" media-type="image/png"/>
    <item id="a" href="a.html" media-type="application/xhtml+xml"/>
    <item id="b" href="b.html" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="a"/><itemref idref="b"/></spine>
</package>`

func epub2(t testing.TB) []byte {
	return build(t,
		entry{"mimetype", "application/epub+zip"},
		container("content.opf"),
		entry{"content.opf", epub2OPF},
		entry{"toc.ncx", `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
			<navPoint id="p1"><navLabel><text>Erster Teil</text></navLabel><content src="a.html"/>
				<navPoint id="p2"><navLabel><text>Unterabschnitt</text></navLabel><content src="b.html#x"/></navPoint>
			</navPoint></navMap></ncx>`},
		// Referenced as Cover.PNG: files made on case-insensitive systems do this.
		entry{"cover.png", "\x89PNG-bytes"},
		entry{"a.html", `<html><body><p>Als Gregor Samsa eines Morgens erwachte.</p></body></html>`},
		// Not well formed: an unclosed paragraph and a bare ampersand.
		entry{"b.html", `<html><body><h3>Zwei</h3><p>Tom & Jerry<p>danach</body>`},
	)
}

func TestEPUB2(t *testing.T) {
	book := parse(t, epub2(t))
	md := book.Metadata

	if md.Version != "2.0" || md.Title != "Die Verwandlung" || md.Language != "de" || md.Published != "1915" {
		t.Errorf("metadata = %+v", md)
	}
	want := []Contributor{{"Franz Kafka", "Kafka, Franz", "aut"}, {"Jemand übersetzt", "", "trl"}}
	if len(md.Contributors) != 2 || md.Contributors[0] != want[0] || md.Contributors[1] != want[1] {
		t.Errorf("contributors = %+v", md.Contributors)
	}
	if len(md.Identifiers) != 2 || md.Identifiers[0] != (Identifier{"isbn", "3150091004"}) || md.Identifiers[1] != (Identifier{"", "B000FC1PJI"}) {
		t.Errorf("identifiers = %+v", md.Identifiers)
	}
	if md.Series != "Erzählungen" || md.SeriesIndex == nil || *md.SeriesIndex != 2.5 {
		t.Errorf("series %q index %v", md.Series, md.SeriesIndex)
	}
	if book.Cover == nil || book.Cover.MediaType != "image/png" {
		t.Errorf("cover = %+v", book.Cover)
	}

	if len(book.Chapters) != 2 {
		t.Fatalf("%d chapters, want 2", len(book.Chapters))
	}
	if book.Chapters[0].Title != "Erster Teil" || book.Chapters[1].Title != "Unterabschnitt" {
		t.Errorf("titles = %q, %q", book.Chapters[0].Title, book.Chapters[1].Title)
	}
	if got := book.Chapters[1].Text; got != "Zwei\n\nTom & Jerry\n\ndanach" {
		t.Errorf("text of the malformed document = %q", got)
	}
}

func TestHeadingNamesAChapterWithoutTOCEntry(t *testing.T) {
	book := parse(t, build(t,
		container("p.opf"),
		entry{"p.opf", `<package version="3.0"><metadata><title>T</title></metadata>
			<manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest>
			<spine><itemref idref="a"/></spine></package>`},
		entry{"a.xhtml", `<html><body><p>Preface text.</p><h2>The <i>Real</i> Heading</h2><h1>Later</h1></body></html>`},
	))
	if len(book.Chapters) != 1 || book.Chapters[0].Title != "The Real Heading" {
		t.Errorf("chapters = %+v", book.Chapters)
	}
	if book.Cover != nil {
		t.Errorf("cover = %+v, want none", book.Cover)
	}
}

func TestDRM(t *testing.T) {
	files := func(algorithm string) []byte {
		return build(t,
			container("p.opf"),
			entry{"p.opf", `<package version="2.0"><metadata><title>Locked</title></metadata>
				<manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest>
				<spine><itemref idref="a"/></spine></package>`},
			entry{"a.xhtml", "<html><body><p>Readable text.</p></body></html>"},
			entry{encryptionPath, `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
				<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
				<EncryptionMethod Algorithm="` + algorithm + `"/></EncryptedData></encryption>`},
		)
	}

	locked := parse(t, files("http://www.w3.org/2001/04/xmlenc#aes128-cbc"))
	if !locked.DRM || len(locked.Chapters) != 0 || locked.Metadata.Title != "Locked" {
		t.Errorf("encrypted book: DRM %v, %d chapters, title %q", locked.DRM, len(locked.Chapters), locked.Metadata.Title)
	}

	fonts := parse(t, files("http://www.idpf.org/2008/embedding"))
	if fonts.DRM || len(fonts.Chapters) != 1 {
		t.Errorf("font obfuscation alone: DRM %v, %d chapters", fonts.DRM, len(fonts.Chapters))
	}
}

func TestNotAnEPUB(t *testing.T) {
	cases := map[string][]byte{
		"not a zip":            []byte("plain text"),
		"empty":                nil,
		"zip without EPUB":     build(t, entry{"readme.txt", "hello"}),
		"container to nothing": build(t, container("missing.opf")),
		"package is not XML":   build(t, container("p.opf"), entry{"p.opf", "\x00\x01 not xml <"}),
		"container leaves":     build(t, container("../../etc/passwd"), entry{"etc/passwd", "x"}),
	}
	for name, data := range cases {
		if _, err := Parse(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrNotEPUB) {
			t.Errorf("%s: err = %v, want ErrNotEPUB", name, err)
		}
	}
}

func TestReferencesStayInsideTheArchive(t *testing.T) {
	book := parse(t, build(t,
		container("OEBPS/p.opf"),
		entry{"OEBPS/p.opf", `<package version="3.0"><metadata><title>T</title>
			<meta name="cover" content="c"/></metadata><manifest>
			<item id="up" href="../../../etc/passwd" media-type="application/xhtml+xml"/>
			<item id="abs" href="/etc/passwd" media-type="application/xhtml+xml"/>
			<item id="url" href="https://example.com/a.xhtml" media-type="application/xhtml+xml"/>
			<item id="ok" href="../shared/ok.xhtml" media-type="application/xhtml+xml"/>
			<item id="c" href="../../cover.png" media-type="image/png"/>
			</manifest><spine><itemref idref="up"/><itemref idref="abs"/><itemref idref="url"/><itemref idref="ok"/></spine></package>`},
		entry{"shared/ok.xhtml", "<html><body><p>inside</p></body></html>"},
		entry{"etc/passwd", "<p>root:x:0:0</p>"},
		entry{"cover.png", "png"},
	))
	if len(book.Chapters) != 1 || book.Chapters[0].Text != "inside" {
		t.Errorf("chapters = %+v", book.Chapters)
	}
	if book.Cover != nil {
		t.Errorf("a cover outside the archive root was read: %+v", book.Cover)
	}

	for href, ok := range map[string]bool{
		"a.xhtml": true, "sub/a.xhtml#frag": true, "../a.xhtml": true,
		"../../a.xhtml": false, "/a.xhtml": false, "file:///etc/passwd": false, "": false, "#frag": false,
	} {
		if _, err := resolve("OEBPS/p.opf", href); (err == nil) != ok {
			t.Errorf("resolve(%q): err = %v, want ok=%v", href, err, ok)
		}
	}
}

func TestLimits(t *testing.T) {
	big := strings.Repeat("<p>"+strings.Repeat("word ", 200)+"</p>", 50)
	data := build(t,
		container("p.opf"),
		entry{"p.opf", `<package version="3.0"><metadata><title>T</title></metadata><manifest>
			<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
			<item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
			</manifest><spine><itemref idref="a"/><itemref idref="b"/></spine></package>`},
		entry{"a.xhtml", "<html><body>" + big + "</body></html>"},
		entry{"b.xhtml", "<html><body>" + big + "</body></html>"},
	)
	r := bytes.NewReader(data)

	cases := map[string]Limits{
		"entries":    {MaxEntries: 3, MaxEntryBytes: 1 << 20, MaxTextBytes: 1 << 20},
		"entry size": {MaxEntries: 100, MaxEntryBytes: 1000, MaxTextBytes: 1 << 20},
		"text size":  {MaxEntries: 100, MaxEntryBytes: 1 << 20, MaxTextBytes: 60000},
	}
	for name, limits := range cases {
		if _, err := ParseWithLimits(r, int64(len(data)), limits); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: err = %v, want ErrTooLarge", name, err)
		}
	}
	if _, err := ParseWithLimits(r, int64(len(data)), Limits{MaxEntries: 100, MaxEntryBytes: 1 << 20, MaxTextBytes: 1 << 20}); err != nil {
		t.Errorf("within the limits: %v", err)
	}
}

// A zip entry can declare any size; what counts is what comes out. This entry
// is a few kilobytes on disk and sixteen megabytes once inflated.
func TestZipBombIsStoppedByWhatItInflatesTo(t *testing.T) {
	data := build(t,
		container("p.opf"),
		entry{"p.opf", `<package version="3.0"><metadata><title>T</title></metadata><manifest>
			<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
			</manifest><spine><itemref idref="a"/></spine></package>`},
		entry{"a.xhtml", "<html><body><p>" + strings.Repeat("a", 16<<20) + "</p></body></html>"},
	)
	if len(data) > 64<<10 {
		t.Fatalf("the fixture is %d bytes; it should compress to almost nothing", len(data))
	}
	limits := Limits{MaxEntries: 100, MaxEntryBytes: 1 << 20, MaxTextBytes: 1 << 20}
	if _, err := ParseWithLimits(bytes.NewReader(data), int64(len(data)), limits); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add(epub3(f))
	f.Add(epub2(f))
	f.Add([]byte("PK\x03\x04"))
	limits := Limits{MaxEntries: 200, MaxEntryBytes: 1 << 20, MaxTextBytes: 4 << 20}
	f.Fuzz(func(t *testing.T, data []byte) {
		book, err := ParseWithLimits(bytes.NewReader(data), int64(len(data)), limits)
		if err != nil {
			return
		}
		if !utf8.ValidString(book.Text()) {
			t.Error("the text is not valid UTF-8")
		}
	})
}

// The archive around a document rarely survives mutation, so the documents
// are fuzzed on their own as well.
func FuzzDocuments(f *testing.F) {
	f.Add([]byte(`<html><body><h1>T</h1><p>a<br/>b &amp; c</p><script>x</script></body></html>`), []byte(epub3OPF))
	f.Add([]byte(`<nav epub:type="toc"><a href="a#b">x</a></nav>`), []byte(epub2OPF))
	f.Fuzz(func(t *testing.T, doc, opf []byte) {
		text, heading := markup.Text(doc)
		if !utf8.ValidString(text) || !utf8.ValidString(heading) {
			t.Error("extracted text is not valid UTF-8")
		}
		if strings.Contains(text, "\n\n\n") || strings.HasPrefix(text, " ") || strings.HasSuffix(text, " ") {
			t.Errorf("whitespace is not collapsed: %q", text)
		}
		navLinks(doc)
		if pkg, err := parsePackage(opf); err == nil {
			pkg.metadata()
			pkg.spineItems()
		}
	})
}
