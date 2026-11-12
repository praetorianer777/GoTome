package epub

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func kepubify(t *testing.T, data []byte) ([]byte, Kepubified) {
	t.Helper()
	var out bytes.Buffer
	res, err := Kepubify(bytes.NewReader(data), int64(len(data)), &out, modified)
	if err != nil {
		t.Fatalf("Kepubify: %v", err)
	}
	checkStructure(t, out.Bytes())
	return out.Bytes(), res
}

func TestKepubifyWrapsSentencesInNumberedSpans(t *testing.T) {
	data := largeBook(t, `<body><div id="top"><h2 id="c1">Kapitel 1</h2>
<p>Ciri ritt durch den Schnee. Die Nacht war lang! Wer folgte ihr? &#8222;Niemand.&#8220; Sie ritt weiter.</p>
<p>Ein <em>zweiter</em> Absatz, mit 3.14 darin.</p>
<pre>keine   Spans. Hier.</pre>
<table><tr><td>Zelle eins. Zelle zwei.</td></tr></table>
</div></body></html>`)
	out, res := kepubify(t, data)
	if res.Documents != 2 || res.Skipped != 0 || res.Spans == 0 {
		t.Fatalf("result = %+v", res)
	}
	doc := entryText(t, out, "OEBPS/text/book.xhtml")
	wellFormed(t, "book.xhtml", doc)

	for _, want := range []string{
		`<span class="koboSpan" id="kobo.2.1">Ciri ritt durch den Schnee.</span> <span class="koboSpan" id="kobo.2.2">Die Nacht war lang!</span> <span class="koboSpan" id="kobo.2.3">Wer folgte ihr?</span>`,
		`<span class="koboSpan" id="kobo.1.1">Kapitel 1</span>`,
		`<span class="koboSpan" id="kobo.3.1">Ein </span><em><span class="koboSpan" id="kobo.3.2">zweiter</span></em>`,
		`3.14 darin.</span>`,
		`<pre>keine   Spans. Hier.</pre>`,
		`<td><span class="koboSpan" id="kobo.4.1">Zelle eins.</span> <span class="koboSpan" id="kobo.4.2">Zelle zwei.</span></td>`,
		`<div id="book-columns"><div id="book-inner"><div id="top">`,
		`</div></div></body>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %s\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "<head>") && !strings.Contains(doc, `id="kobostylehacks"`) {
		t.Errorf("the style is missing")
	}
	spans := regexp.MustCompile(`id="(kobo\.\d+\.\d+)"`).FindAllStringSubmatch(doc, -1)
	seen := map[string]bool{}
	for _, m := range spans {
		if seen[m[1]] {
			t.Errorf("id %s stands twice", m[1])
		}
		seen[m[1]] = true
	}
}

func TestKepubifyKeepsTextIDsAndEverythingElse(t *testing.T) {
	data := largeBook(t, novel(3, 400))
	out, _ := kepubify(t, data)

	before, err := Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("the KEPUB does not open: %v", err)
	}
	if strings.Join(words(before), " ") != strings.Join(words(after), " ") {
		t.Errorf("the text changed")
	}
	for _, name := range []string{"OEBPS/content.opf", "OEBPS/style.css", "OEBPS/nav.xhtml", "OEBPS/toc.ncx"} {
		if entryText(t, data, name) != entryText(t, out, name) {
			t.Errorf("%s changed", name)
		}
	}
	target(t, out, "OEBPS/nav.xhtml", "text/book.xhtml#c3")
	if _, err := Measure(bytes.NewReader(out), int64(len(out))); err != nil {
		t.Errorf("Measure: %v", err)
	}
}

func TestKepubifyLeavesADocumentThatIsNotXMLAlone(t *testing.T) {
	broken := xhtmlHead + `<body><p>Offen. Nicht zu.<br></body></html>`
	data := largeBook(t, strings.TrimPrefix(broken, xhtmlHead))
	out, res := kepubify(t, data)
	if res.Skipped != 1 {
		t.Errorf("result = %+v", res)
	}
	if got := entryText(t, out, "OEBPS/text/book.xhtml"); got != entryText(t, data, "OEBPS/text/book.xhtml") {
		t.Errorf("the document changed: %s", got)
	}
}

func TestKepubifyRefusesWhatIsNotAnEPUB(t *testing.T) {
	var out bytes.Buffer
	if _, err := Kepubify(strings.NewReader("not a zip"), 9, &out, modified); err == nil {
		t.Error("a text was converted")
	}
}

func TestSentences(t *testing.T) {
	var parts []string
	for _, s := range sentences("  Eins. Zwei?! \"Drei.\" Vier") {
		parts = append(parts, s.text)
	}
	want := []string{"Eins.", "Zwei?!", `"Drei."`, "Vier"}
	if strings.Join(parts, "|") != strings.Join(want, "|") {
		t.Errorf("sentences = %q, want %q", parts, want)
	}
	var back strings.Builder
	text := " a. b  c. "
	for _, s := range sentences(text) {
		back.WriteString(s.lead + s.text + s.trail)
	}
	if back.String() != text {
		t.Errorf("pieces together = %q", back.String())
	}
}

func TestKepubName(t *testing.T) {
	for in, want := range map[string]string{
		"Der Schwalbenturm.epub":       "Der Schwalbenturm.kepub.epub",
		"Der Schwalbenturm.kepub.epub": "Der Schwalbenturm.kepub.epub",
		"Buch":                         "Buch.kepub.epub",
	} {
		if got := KepubName(in); got != want {
			t.Errorf("KepubName(%q) = %q, want %q", in, got, want)
		}
	}
}
