package epub

import (
	"slices"
	"strings"
	"testing"
)

func markupBook(t *testing.T, docs map[string]string, nav, ncx string) []byte {
	t.Helper()
	var items, spine strings.Builder
	names := make([]string, 0, len(docs))
	for name := range docs {
		names = append(names, name)
	}
	slices.Sort(names)
	entries := []entry{{"mimetype", "application/epub+zip"}, container("OEBPS/content.opf")}
	for i, name := range names {
		id := "d" + string(rune('a'+i))
		items.WriteString("\n    <item id=\"" + id + "\" href=\"text/" + name + "\" media-type=\"application/xhtml+xml\"/>")
		spine.WriteString("\n    <itemref idref=\"" + id + "\"/>")
	}
	entries = append(entries,
		entry{"OEBPS/content.opf", strings.Replace(fixPackage(items.String(), spine.String()), "text/book.xhtml#c2", "text/"+names[0], 1)},
		entry{"OEBPS/nav.xhtml", nav},
		entry{"OEBPS/toc.ncx", ncx},
	)
	for _, name := range names {
		entries = append(entries, entry{"OEBPS/text/" + name, docs[name]})
	}
	return build(t, entries...)
}

const (
	emptyNav = `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="text/one.xhtml">Eins</a></li></ol></nav></body></html>`
	emptyNCX = `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint id="p1"><navLabel><text>Eins</text></navLabel><content src="text/one.xhtml"/></navPoint>
</navMap></ncx>`
)

func TestFixRewritesDocumentsThatAreNotWellFormed(t *testing.T) {
	docs := map[string]string{
		// A void element left open, an entity no DTD declares, a bare
		// ampersand, an empty anchor and title as XML writes them, an end
		// tag out of order and an attribute without quotes.
		"one.xhtml": `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title/></head>
<body epub:type="bodymatter"><h1 id="k1" class=title>Erstes Kapitel</h1>
<p>Geralt&nbsp;ritt, Rittersporn sang &amp; Ciri schwieg.<br>Dann <b><i>kam</b></i> der Winter.</p>
<p><a id="mitte"/>Tom & Jerry.</p>
</body></html>`,
		// Text in a table after a cell, which an HTML parser moves before
		// the table: that is not what the document says, so it stays.
		"two.xhtml":   `<html xmlns="http://www.w3.org/1999/xhtml"><body><table><tr><td>Zelle<br></td>Streu</tr></table><p>Ende</p></body></html>`,
		"three.xhtml": xhtmlHead + `<body><p>Wohlgeformt&nbsp;und gut.</p></body></html>`,
	}
	data := markupBook(t, docs, emptyNav, emptyNCX)
	if m := measure(t, data); !slices.Equal(m.MalformedDocuments, []string{"OEBPS/text/one.xhtml", "OEBPS/text/two.xhtml"}) {
		t.Fatalf("malformed before: %v", m.MalformedDocuments)
	}
	before := words(parse(t, data))

	written, fixed := fix(t, data, MalformedDocument)
	if fixed.DocumentsRepaired != 1 || !fixed.Any() {
		t.Errorf("fixed %+v", fixed)
	}
	one := entryText(t, written, "OEBPS/text/one.xhtml")
	if err := checkWellFormed([]byte(one)); err != nil {
		t.Errorf("one.xhtml after the fix: %v\n%s", err, one)
	}
	for _, want := range []string{`<a id="mitte"></a>Tom &amp; Jerry`, `<title></title>`, `xmlns:epub="http://www.idpf.org/2007/ops"`, `epub:type="bodymatter"`, `<br/>`} {
		if !strings.Contains(one, want) {
			t.Errorf("one.xhtml lacks %s:\n%s", want, one)
		}
	}
	if got := entryText(t, written, "OEBPS/text/two.xhtml"); got != docs["two.xhtml"] {
		t.Errorf("two.xhtml was changed:\n%s", got)
	}
	if got := entryText(t, written, "OEBPS/text/three.xhtml"); got != docs["three.xhtml"] {
		t.Errorf("three.xhtml was changed:\n%s", got)
	}
	// The text of one.xhtml did not read before; now it does, all of it.
	want := append(strings.Fields("Erstes Kapitel Geralt ritt, Rittersporn sang & Ciri schwieg. Dann kam der Winter. Tom & Jerry."), before...)
	if after := words(parse(t, written)); !slices.Equal(after, want) {
		t.Errorf("text before %q\nafter %q", before, after)
	}
	if m := measure(t, written); !slices.Equal(m.MalformedDocuments, []string{"OEBPS/text/two.xhtml"}) {
		t.Errorf("malformed after: %v", m.MalformedDocuments)
	}
}

func TestFixMendsTablesOfContentsThatLeadNowhere(t *testing.T) {
	docs := map[string]string{
		"one.xhtml": xhtmlHead + `<body><h1 id="k1">Eins</h1><p id="k1a">Text.</p></body></html>`,
		"two.xhtml": xhtmlHead + `<body><h1 id="k2">Zwei</h1></body></html>`,
	}
	nav := `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="toc"><ol>
<li><a href="text/one.xhtml#k1">Eins</a>
<ol>
<li><a href="text/lost.xhtml">Verloren</a></li>
</ol>
</li>
<li><a href="text/two.xhtml#nirgends">Zwei</a></li>
<li><a href="text/gone.xhtml">Fort</a></li>
<li><span>Teil</span>
<ol><li><a href="text/gone.xhtml#x">Fort auch</a></li></ol>
</li>
<li><a href="text/gone.xhtml">Oben fort</a>
<ol><li><a href="text/one.xhtml#k1a">Unten da</a></li></ol>
</li>
</ol></nav>
<nav epub:type="landmarks"><ol><li><a epub:type="bodymatter" href="text/one.xhtml">Start</a></li></ol></nav>
</body></html>`
	ncx := `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint id="p1" playOrder="1"><navLabel><text>Eins</text></navLabel><content src="text/one.xhtml#k1"/></navPoint>
<navPoint id="p2" playOrder="2"><navLabel><text>Zwei</text></navLabel><content src='text/two.xhtml#nirgends'/></navPoint>
<navPoint id="p3" playOrder="3"><navLabel><text>Fort</text></navLabel><content src="text/gone.xhtml"/></navPoint>
<navPoint id="p4" playOrder="4"><navLabel><text>Oben fort</text></navLabel><content src="text/gone.xhtml#a"/>
  <navPoint id="p4a" playOrder="5"><navLabel><text>Unten da</text></navLabel><content src="text/one.xhtml#k1a"/></navPoint>
</navPoint>
<navPoint id="p5" playOrder="6"><navLabel><text>Ganz fort</text></navLabel><content src="text/gone.xhtml"/>
  <navPoint id="p5a" playOrder="7"><navLabel><text>Auch fort</text></navLabel><content src="text/lost.xhtml"/></navPoint>
</navPoint>
</navMap></ncx>`
	data := markupBook(t, docs, nav, ncx)
	if m := measure(t, data); len(m.BrokenTocEntries) != 6 {
		t.Fatalf("broken before: %v", m.BrokenTocEntries)
	}

	written, fixed := fix(t, data, BrokenTOC)
	// The nav: lost.xhtml, gone.xhtml, and the heading with its one entry
	// go; two.xhtml and the entry above "Unten da" point elsewhere. The
	// NCX: p3 and p5 with p5a go, p2 and p4 point elsewhere.
	if fixed.TocEntriesRemoved != 7 || fixed.TocEntriesRepointed != 4 {
		t.Errorf("fixed %+v", fixed)
	}
	if m := measure(t, written); len(m.BrokenTocEntries) != 0 || len(m.Findings()) != 0 {
		t.Errorf("measured after the fix: %+v", m)
	}
	gotNav := entryText(t, written, "OEBPS/nav.xhtml")
	wellFormed(t, "nav.xhtml", gotNav)
	wantNav := `<li><a href="text/one.xhtml#k1">Eins</a>
</li>
<li><a href="text/two.xhtml">Zwei</a></li>
<li><a href="text/one.xhtml#k1a">Oben fort</a>
<ol><li><a href="text/one.xhtml#k1a">Unten da</a></li></ol>
</li>
</ol></nav>`
	if !strings.Contains(gotNav, wantNav) || !strings.Contains(gotNav, `href="text/one.xhtml">Start`) {
		t.Errorf("nav.xhtml:\n%s", gotNav)
	}
	gotNCX := entryText(t, written, "OEBPS/toc.ncx")
	wellFormed(t, "toc.ncx", gotNCX)
	for _, gone := range []string{`id="p3"`, `id="p5"`, `id="p5a"`, "gone.xhtml", "lost.xhtml"} {
		if strings.Contains(gotNCX, gone) {
			t.Errorf("toc.ncx still has %s:\n%s", gone, gotNCX)
		}
	}
	if !strings.Contains(gotNCX, `<content src='text/two.xhtml'/>`) || strings.Count(gotNCX, `src="text/one.xhtml#k1a"`) != 2 {
		t.Errorf("toc.ncx:\n%s", gotNCX)
	}
	book := parse(t, written)
	if len(book.Chapters) == 0 {
		t.Error("no chapters after the fix")
	}
}

func TestFixLeavesATableOfContentsWithNothingThatLeads(t *testing.T) {
	docs := map[string]string{"one.xhtml": xhtmlHead + `<body><p>Text.</p></body></html>`}
	ncx := `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint id="p1"><navLabel><text>Fort</text></navLabel><content src="text/gone.xhtml"/></navPoint>
</navMap></ncx>`
	data := markupBook(t, docs, emptyNav, ncx)
	written, fixed := fix(t, data, BrokenTOC)
	if fixed.Any() {
		t.Errorf("fixed %+v", fixed)
	}
	if got := entryText(t, written, "OEBPS/toc.ncx"); got != ncx {
		t.Errorf("toc.ncx was changed:\n%s", got)
	}
}

func FuzzReserialize(f *testing.F) {
	f.Add([]byte(`<html><body><p>a<br>b &amp; c<a id="x"/></p></body></html>`))
	f.Add([]byte(`<?xml version="1.0" encoding="iso-8859-1"?><html><head><title/></head><body><b><i>x</b></i></body></html>`))
	f.Add([]byte(`<html><body><table><tr><td>a</td>b</tr></table><script>if (a < b) {}</script></body></html>`))
	f.Fuzz(func(t *testing.T, doc []byte) {
		if out, ok := reserialize(doc); ok {
			if err := checkWellFormed(out); err != nil {
				t.Fatalf("not well formed: %v\n%s", err, out)
			}
		}
		_, _ = parseTOC(doc, false)
		_, _ = parseTOC(doc, true)
	})
}
