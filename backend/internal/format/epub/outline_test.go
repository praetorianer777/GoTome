package epub

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestOutlineStartsChaptersWhereTheirDocumentsStartBySize(t *testing.T) {
	doc := func(n int) string { return "<html><body>" + strings.Repeat("x", n) + "</body></html>" }
	one, two, three := doc(100), doc(300), doc(600)
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">x</dc:identifier><dc:title>T</dc:title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
    <item id="c" href="c.xhtml" media-type="application/xhtml+xml"/>
    <item id="n" href="notes.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="a"/><itemref idref="b"/><itemref idref="c"/><itemref idref="n" linear="no"/>
  </spine>
</package>`},
		entry{"OEBPS/nav.xhtml", `<html xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol>
<li><a href="a.xhtml">Chapter 1</a></li><li><a href="b.xhtml">Chapter 2</a></li><li><a href="c.xhtml">Chapter 3</a></li>
</ol></nav></body></html>`},
		entry{"OEBPS/a.xhtml", one}, entry{"OEBPS/b.xhtml", two}, entry{"OEBPS/c.xhtml", three},
		entry{"OEBPS/notes.xhtml", doc(5000)},
	)
	got, err := Outline(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	total := float64(len(one) + len(two) + len(three))
	want := []OutlineEntry{
		{"Chapter 1", 0},
		{"Chapter 2", float64(len(one)) / total},
		{"Chapter 3", float64(len(one)+len(two)) / total},
	}
	if len(got) != len(want) {
		t.Fatalf("outline %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Title != want[i].Title || math.Abs(got[i].Start-want[i].Start) > 1e-9 {
			t.Errorf("entry %d is %v, want %v", i, got[i], want[i])
		}
	}
}
