// Package textproc turns the text extraction reads from a file into chunks:
// pieces of about ChunkSize characters, cut at paragraph breaks, each with
// where it stands in the book and the language it is written in. Search,
// near-duplicate detection and embeddings all read chunks.
package textproc

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ChunkSize is the length a chunk grows to, in characters, before the next
// paragraph starts a new one. A paragraph longer than that is cut at a space.
const ChunkSize = 8000

// Section is a stretch of a file's text as extraction yields it: an EPUB's
// chapter, a PDF's page.
type Section struct {
	// Label names the section for a person: a chapter's title, a page's
	// number. It may be empty.
	Label string
	// Page is the section's page number, for formats whose sections are
	// pages; 0 when they are not.
	Page int
	Text string
}

// Chunk is one piece of a book's text.
type Chunk struct {
	// Position counts the chunks of a book from 0.
	Position int
	// Chapter is the label of the section the chunk starts in.
	Chapter string
	// PageFrom and PageTo are the pages the chunk spans, from the sections'
	// pages or, when they have none, estimated from the characters before it.
	// 0 when neither is known.
	PageFrom, PageTo int
	// Offset is where the chunk starts in the whole text, in characters,
	// with sections joined by a blank line.
	Offset int
	// Lang is the chunk's language as Detect says it.
	Lang string
	Text string
}

// sectionSeparator stands between two sections in the whole text, as in
// epub.Book.Text.
const sectionSeparator = "\n\n"

// Options steer Split.
type Options struct {
	// CharsPerPage estimates pages for sections that have none; 0 leaves
	// them unknown.
	CharsPerPage int
	// LangHint is the book's language as its metadata says, used for a
	// chunk whose own text is too short or too mixed to tell.
	LangHint string
}

// Split normalises the sections' text and cuts it into chunks. Chunks run
// across sections, so a book of many short chapters is not many short chunks.
func Split(sections []Section, opts Options) []Chunk {
	type para struct {
		text    string
		chapter string
		page    int
		offset  int
	}
	var paras []para
	offset := 0
	for i, s := range sections {
		if i > 0 {
			offset += len(sectionSeparator)
		}
		text := Normalize(s.Text)
		pos := 0
		for p := range strings.SplitSeq(text, "\n\n") {
			if strings.TrimSpace(p) != "" {
				paras = append(paras, para{text: p, chapter: s.Label, page: s.Page, offset: offset + pos})
			}
			pos += utf8.RuneCountInString(p) + 2
		}
		offset += utf8.RuneCountInString(text)
	}

	page := func(p para, length int) (int, int) {
		switch {
		case p.page > 0:
			return p.page, p.page
		case opts.CharsPerPage > 0:
			return p.offset/opts.CharsPerPage + 1, (p.offset+max(length, 1)-1)/opts.CharsPerPage + 1
		}
		return 0, 0
	}

	var out []Chunk
	var cur *Chunk
	var b strings.Builder
	n := 0
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = b.String()
		cur.Position = len(out)
		out = append(out, *cur)
		cur, n = nil, 0
		b.Reset()
	}
	add := func(p para, text string, length int) {
		from, to := page(p, length)
		if cur == nil {
			cur = &Chunk{Chapter: p.chapter, Offset: p.offset, PageFrom: from, PageTo: to}
		} else {
			b.WriteString("\n\n")
			cur.PageTo = max(cur.PageTo, to)
		}
		b.WriteString(text)
		n += length
	}
	for _, p := range paras {
		l := utf8.RuneCountInString(p.text)
		if n > 0 && n+l > ChunkSize {
			flush()
		}
		for l > ChunkSize {
			head, rest, headLen := cutAtSpace(p.text, ChunkSize)
			add(p, head, headLen)
			flush()
			p.offset += utf8.RuneCountInString(p.text) - utf8.RuneCountInString(rest)
			p.text, l = rest, utf8.RuneCountInString(rest)
		}
		if l > 0 {
			add(p, p.text, l)
		}
	}
	flush()

	book := Detect(joinSample(out), opts.LangHint)
	for i := range out {
		out[i].Lang = Detect(out[i].Text, book)
	}
	return out
}

// cutAtSpace splits s after at most limit characters, at the last space
// before that if there is one, and says how many characters the head has.
func cutAtSpace(s string, limit int) (head, rest string, headLen int) {
	i, count := 0, 0
	for i < len(s) && count < limit {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		count++
	}
	if sp := strings.LastIndexByte(s[:i], ' '); sp > 0 {
		head = s[:sp]
		return head, s[sp+1:], utf8.RuneCountInString(head)
	}
	return s[:i], s[i:], count
}

// joinSample is the start of a few chunks spread over the book, enough to
// tell its language.
func joinSample(chunks []Chunk) string {
	var b strings.Builder
	step := max(len(chunks)/8, 1)
	for i := 0; i < len(chunks); i += step {
		t := chunks[i].Text
		if len(t) > 2000 {
			t = t[:2000]
		}
		b.WriteString(t)
		b.WriteByte(' ')
	}
	return b.String()
}

// Normalize puts text into one form: NFC, line breaks as "\n", no control
// characters but the line break and the tab, no space at the end of a line,
// and at most one blank line between paragraphs.
func Normalize(text string) string {
	text = norm.NFC.String(strings.ToValidUTF8(text, "�"))
	var b strings.Builder
	b.Grow(len(text))
	newlines := 0
	pendingSpace := ""
	for _, r := range text {
		switch {
		case r == '\r':
			continue
		case r == '\n':
			pendingSpace = ""
			newlines++
			continue
		case r == ' ' || r == '\t':
			pendingSpace += string(r)
			continue
		case unicode.IsControl(r):
			continue
		}
		if newlines > 0 {
			if b.Len() > 0 {
				b.WriteString(strings.Repeat("\n", min(newlines, 2)))
			}
			newlines = 0
			pendingSpace = strings.TrimLeft(pendingSpace, " \t")
		}
		if b.Len() > 0 {
			b.WriteString(pendingSpace)
		}
		pendingSpace = ""
		b.WriteRune(r)
	}
	return b.String()
}
