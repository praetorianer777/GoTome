// Package markup turns HTML into the text a reader sees. EPUB chapters and the
// markup inside MOBI files both go through it.
package markup

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// skipped elements carry no text of the book: code, styling, the document's
// own metadata, and drawings whose text nodes are labels.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Noscript: true,
	atom.Svg: true, atom.Math: true, atom.Template: true, atom.Iframe: true,
	atom.Object: true,
}

// blocks end the paragraph before them and start a new one.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Li: true, atom.Ul: true,
	atom.Ol: true, atom.Blockquote: true, atom.Section: true, atom.Article: true,
	atom.Table: true, atom.Tr: true, atom.Pre: true, atom.Hr: true, atom.Dt: true,
	atom.Dd: true, atom.Dl: true, atom.Figure: true, atom.Figcaption: true,
	atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Body: true,
	atom.Nav: true, atom.Address: true, atom.Caption: true,
}

// pageBreak is how Mobipocket markup ends a page, which ends a paragraph too.
const pageBreak = "mbp:pagebreak"

var headings = map[atom.Atom]bool{atom.H1: true, atom.H2: true, atom.H3: true}

// Written as numbers: the characters themselves are invisible in an editor.
const (
	nbsp           = 0x00A0
	softHyphen     = 0x00AD
	zeroWidthSpace = 0x200B
	byteOrderMark  = 0xFEFF
)

// replacement stands in for bytes that are not UTF-8.
var replacement = []byte(string(utf8.RuneError))

// Builder joins text nodes the way a reader sees them: whitespace
// collapsed, a blank line between blocks, a line break where the markup has one.
type Builder struct {
	sb    strings.Builder
	space bool
	line  bool
	para  bool
}

// Write adds a piece of text.
func (t *Builder) Write(s string) {
	for _, r := range s {
		if unicode.IsSpace(r) || r == nbsp {
			t.space = true
			continue
		}
		// Soft hyphens and zero-width characters are typesetting, and a word
		// that carries one would not match the same word without it.
		if r == softHyphen || r == zeroWidthSpace || r == byteOrderMark {
			continue
		}
		if t.sb.Len() > 0 {
			switch {
			case t.para:
				t.sb.WriteString("\n\n")
			case t.line:
				t.sb.WriteByte('\n')
			case t.space:
				t.sb.WriteByte(' ')
			}
		}
		t.space, t.line, t.para = false, false, false
		t.sb.WriteRune(r)
	}
}

// String is the text so far.
func (t *Builder) String() string { return t.sb.String() }

// Text returns a document's text without markup, and its first heading. It
// reads HTML as browsers do, so a document that is not well formed still
// yields its text.
func Text(doc []byte) (text, heading string) {
	doc = bytes.ToValidUTF8(doc, replacement)
	z := html.NewTokenizer(bytes.NewReader(doc))

	var body, head Builder
	skipDepth := 0
	var skipTag atom.Atom
	inHeading := false
	var headingTag atom.Atom
	headingDone := false

	for {
		switch z.Next() {
		case html.ErrorToken:
			return body.sb.String(), head.sb.String()

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			s := string(z.Text())
			body.Write(s)
			if inHeading {
				head.Write(s)
			}

		case html.StartTagToken:
			name, _ := z.TagName()
			tag := atom.Lookup(name)
			if string(name) == pageBreak {
				body.para = true
			}
			if skipDepth > 0 {
				if tag == skipTag {
					skipDepth++
				}
				continue
			}
			if skipped[tag] {
				skipDepth, skipTag = 1, tag
				continue
			}
			if blocks[tag] {
				body.para = true
			}
			if tag == atom.Br {
				body.line = true
			}
			// Cells of a row read as words on a line, not as one word.
			if tag == atom.Td || tag == atom.Th {
				body.space = true
			}
			if headings[tag] && !headingDone && !inHeading {
				inHeading, headingTag = true, tag
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := atom.Lookup(name)
			if skipDepth > 0 {
				if tag == skipTag {
					skipDepth--
				}
				continue
			}
			if blocks[tag] {
				body.para = true
			}
			if inHeading && tag == headingTag {
				inHeading = false
				headingDone = head.sb.Len() > 0
			}

		case html.SelfClosingTagToken:
			if skipDepth > 0 {
				continue
			}
			name, _ := z.TagName()
			switch tag := atom.Lookup(name); {
			case string(name) == pageBreak:
				body.para = true
			case tag == atom.Br:
				body.line = true
			case blocks[tag]:
				body.para = true
			}
		}
	}
}
