package mobi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ErrProtected is a book whose text is encrypted: there is nothing to
// write an EPUB from.
var ErrProtected = errors.New("the book is protected by DRM")

// ToEPUBVersion is the version of what ToEPUB writes.
const ToEPUBVersion = 1

// Converted says what ToEPUB wrote.
type Converted struct {
	// Sections are the content documents, Images the images in them and
	// TOCEntries the entries of the table of contents.
	Sections   int `json:"sections"`
	Images     int `json:"images"`
	TOCEntries int `json:"tocEntries"`
	// LinksDropped are links to places the converter cannot find in the book,
	// which stay as plain text.
	LinksDropped int `json:"linksDropped"`
}

const (
	// sectionBytes is the most a section holds before the next block starts
	// another, well under what makes an EPUB heavy on an e-reader.
	sectionBytes = 200 << 10
	// minSection is the least a chapter break leaves in a section, so that
	// a book that breaks pages after every verse is not a thousand files.
	minSection = 2 << 10
)

// ToEPUB writes an EPUB from a Mobipocket book (MOBI, AZW, AZW3) to w: its
// text as XHTML documents cut at page breaks and chapter headings, the
// images they show, a table of contents from the headings, the metadata
// and the cover. Only the structure of the text is kept, not its looks
// (fonts, colours, margins), and links that name a place by its position
// in a KF8 file are left as text. The same book gives the same bytes.
func ToEPUB(r io.ReaderAt, size int64, w io.Writer, modified time.Time) (Converted, error) {
	book, h, raw, err := read(r, size, DefaultLimits)
	if err != nil {
		return Converted{}, err
	}
	if book.DRM {
		return Converted{}, ErrProtected
	}
	if !book.KF8 {
		raw = anchorFilepos(raw)
	}
	markup := h.utf8(raw)

	doc, err := html.ParseWithOptions(bytes.NewReader(markup), html.ParseOptionEnableScripting(false))
	if err != nil {
		return Converted{}, fmt.Errorf("%w: %v", ErrNotMOBI, err)
	}
	c := &converter{h: h, images: map[int]*picture{}, ids: map[string]bool{}}
	body := findBody(doc)
	if body == nil {
		return Converted{}, fmt.Errorf("%w: no text", ErrNotMOBI)
	}
	units := c.units(c.clean(body))
	if len(units) == 0 {
		return Converted{}, fmt.Errorf("%w: no text", ErrNotMOBI)
	}
	sections := c.cut(units)
	if cover := book.Cover; cover != nil {
		c.cover = c.imageAt(h.coverRecord(h.exth()), cover.Data)
	}
	return c.write(w, book, sections, modified)
}

// findBody is the body element of a parsed document.
func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

var fileposRef = regexp.MustCompile(`(?i)<a\s[^>]*?filepos\s*=\s*["']?0*(\d+)`)

// anchorFilepos puts an anchor, <a id="filepos-N"/>, at every position of
// the markup that a link of the older format (filepos=N) names, so that
// the link can lead there once the markup is cut into documents.
func anchorFilepos(raw []byte) []byte {
	set := map[int]bool{}
	for _, m := range fileposRef.FindAllSubmatch(raw, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n > 0 && n < len(raw) {
			set[n] = true
		}
	}
	if len(set) == 0 {
		return raw
	}
	at := map[int][]int{}
	for n := range set {
		pos := n
		// A position inside a tag is the start of that tag.
		if open := bytes.LastIndexByte(raw[:pos], '<'); open >= 0 && bytes.LastIndexByte(raw[:pos], '>') < open {
			pos = open
		}
		at[pos] = append(at[pos], n)
	}
	positions := make([]int, 0, len(at))
	for p := range at {
		positions = append(positions, p)
	}
	sort.Ints(positions)
	var out bytes.Buffer
	prev := 0
	for _, p := range positions {
		out.Write(raw[prev:p])
		slices.Sort(at[p])
		for _, n := range at[p] {
			fmt.Fprintf(&out, `<a id="filepos-%d"></a>`, n)
		}
		prev = p
	}
	out.Write(raw[prev:])
	return out.Bytes()
}

// coverRecord is the number of the record the EXTH cover offset names, or -1.
func (h *header) coverRecord(exth map[uint32][][]byte) int {
	values := exth[exthCoverOffset]
	first := h.u32(0x6C)
	if len(values) == 0 || len(values[0]) != 4 || first == noIndex {
		return -1
	}
	off := binary.BigEndian.Uint32(values[0])
	if off == noIndex || off > noIndex-first {
		return -1
	}
	return int(first + off)
}

// picture is an image of the book that the converter writes.
type picture struct {
	record      int
	name, media string
	data        []byte
}

type converter struct {
	h      *header
	images map[int]*picture
	// ids are the ids written so far.
	ids map[string]bool
	// cover is the cover's image, if the book has one.
	cover *picture
	// headings numbers the ids headings are given.
	headings int
	dropped  int
}

// imageAt is the image in a record, shared by every use of it; nil where
// the record holds none the converter knows.
func (c *converter) imageAt(record int, data []byte) *picture {
	if record < 0 {
		return nil
	}
	if im, ok := c.images[record]; ok {
		return im
	}
	if data == nil {
		data = c.h.db.record(uint32(record))
	}
	var ext string
	media := imageType(data)
	switch media {
	case "image/jpeg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/gif":
		ext = "gif"
	default:
		c.images[record] = nil
		return nil
	}
	im := &picture{record: record, name: fmt.Sprintf("image-%04d.%s", record, ext), media: media, data: data}
	c.images[record] = im
	return im
}

// imageFor resolves an img element's reference: the record number of the
// older format (recindex, counted from the first image), or the base 32
// number of a KF8 kindle:embed address.
func (c *converter) imageFor(n *html.Node) *picture {
	first := c.h.u32(0x6C)
	if first == noIndex {
		return nil
	}
	for _, a := range n.Attr {
		switch a.Key {
		case "recindex", "hirecindex":
			if i, err := strconv.Atoi(strings.TrimSpace(a.Val)); err == nil && i > 0 {
				return c.imageAt(int(first)+i-1, nil)
			}
		case "src":
			if rest, ok := strings.CutPrefix(a.Val, "kindle:embed:"); ok {
				code, _, _ := strings.Cut(rest, "?")
				if i, err := strconv.ParseInt(code, 32, 32); err == nil && i > 0 {
					return c.imageAt(int(first)+int(i)-1, nil)
				}
			}
		}
	}
	return nil
}

// keep are the elements the converter writes as they are.
var keep = map[atom.Atom]bool{
	atom.A: true, atom.Abbr: true, atom.B: true, atom.Blockquote: true, atom.Br: true,
	atom.Caption: true, atom.Cite: true, atom.Code: true, atom.Dd: true, atom.Dfn: true,
	atom.Div: true, atom.Dl: true, atom.Dt: true, atom.Em: true, atom.H1: true, atom.H2: true,
	atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Hr: true, atom.I: true,
	atom.Img: true, atom.Li: true, atom.Ol: true, atom.P: true, atom.Pre: true, atom.Q: true,
	atom.S: true, atom.Small: true, atom.Span: true, atom.Strong: true, atom.Sub: true,
	atom.Sup: true, atom.Table: true, atom.Tbody: true, atom.Td: true, atom.Tfoot: true,
	atom.Th: true, atom.Thead: true, atom.Tr: true, atom.U: true, atom.Ul: true,
}

// droppedElements are the elements whose content is not text of the book.
var droppedElements = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Title: true, atom.Meta: true,
	atom.Link: true, atom.Object: true, atom.Iframe: true, atom.Svg: true, atom.Math: true,
	atom.Noscript: true, atom.Template: true, atom.Audio: true, atom.Video: true,
}

const pageBreak = "mbp:pagebreak"

var validID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// clean copies a node and what is below it as XHTML the converter writes:
// the elements of keep with the few attributes they need, pictures resolved
// to files, links to places in the book kept and others unwrapped, and a
// page break kept as a comment node to cut at.
func (c *converter) clean(n *html.Node) []*html.Node {
	switch n.Type {
	case html.TextNode:
		return []*html.Node{{Type: html.TextNode, Data: xmlText(n.Data)}}
	case html.ElementNode:
	case html.DocumentNode:
		return c.children(n)
	default:
		return nil
	}
	if n.Data == pageBreak {
		// An HTML parser takes <mbp:pagebreak/> for the start of an element
		// that holds the rest of the book.
		return append([]*html.Node{{Type: html.CommentNode, Data: "pagebreak"}}, c.children(n)...)
	}
	if droppedElements[n.DataAtom] {
		return nil
	}
	switch n.DataAtom {
	case atom.Body:
		return c.children(n)
	case atom.Img:
		im := c.imageFor(n)
		if im == nil {
			return nil
		}
		return []*html.Node{{Type: html.ElementNode, Data: "img", DataAtom: atom.Img, Attr: []html.Attribute{
			{Key: "src", Val: "images/" + im.name}, {Key: "alt", Val: xmlText(attrOf(n, "alt"))},
		}}}
	case atom.Center:
		out := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div, Attr: []html.Attribute{{Key: "style", Val: "text-align: center"}}}
		c.adopt(out, c.children(n))
		return []*html.Node{out}
	case atom.Strike, atom.Del:
		out := &html.Node{Type: html.ElementNode, Data: "s", DataAtom: atom.S}
		c.adopt(out, c.children(n))
		return []*html.Node{out}
	case atom.Big, atom.Font:
		return c.children(n)
	}
	if !keep[n.DataAtom] {
		return c.children(n)
	}
	out := &html.Node{Type: html.ElementNode, Data: n.Data, DataAtom: n.DataAtom}
	switch n.DataAtom {
	case atom.A:
		href := attrOf(n, "href")
		switch {
		case attrOf(n, "filepos") != "":
			n0, err := strconv.Atoi(strings.TrimSpace(attrOf(n, "filepos")))
			if err != nil {
				href = ""
				break
			}
			href = "#filepos-" + strconv.Itoa(n0)
		case strings.HasPrefix(href, "#"), strings.HasPrefix(href, "http://"), strings.HasPrefix(href, "https://"), strings.HasPrefix(href, "mailto:"):
		default:
			href = ""
			if attrOf(n, "href") != "" {
				c.dropped++
			}
		}
		if href != "" {
			out.Attr = append(out.Attr, html.Attribute{Key: "href", Val: href})
		}
		if id := firstNonEmpty(attrOf(n, "id"), attrOf(n, "name")); id != "" {
			c.setID(out, id)
		}
	default:
		if id := attrOf(n, "id"); id != "" {
			c.setID(out, id)
		}
	}
	for _, name := range []string{"colspan", "rowspan"} {
		if v := attrOf(n, name); n.DataAtom == atom.Td || n.DataAtom == atom.Th {
			if i, err := strconv.Atoi(v); err == nil && i > 1 && i < 1000 {
				out.Attr = append(out.Attr, html.Attribute{Key: name, Val: strconv.Itoa(i)})
			}
		}
	}
	c.adopt(out, c.children(n))
	return []*html.Node{out}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// setID gives the node an id unless it is not a valid one or is taken.
func (c *converter) setID(n *html.Node, id string) {
	if !validID.MatchString(id) || c.ids[id] {
		return
	}
	c.ids[id] = true
	n.Attr = append(n.Attr, html.Attribute{Key: "id", Val: id})
}

func (c *converter) children(n *html.Node) []*html.Node {
	var out []*html.Node
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		out = append(out, c.clean(ch)...)
	}
	return out
}

func (c *converter) adopt(parent *html.Node, children []*html.Node) {
	for _, ch := range children {
		parent.AppendChild(ch)
	}
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// xmlText removes the characters XML 1.0 does not allow.
func xmlText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t', r == '\n', r == '\r', r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			if r == utf8.RuneError {
				return -1
			}
			return r
		}
		return -1
	}, s)
}

var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Ul: true, atom.Ol: true, atom.Table: true,
	atom.Blockquote: true, atom.Pre: true, atom.Hr: true, atom.Dl: true,
}

func isBlock(n *html.Node) bool {
	return n.Type == html.ElementNode && blockElements[n.DataAtom]
}

func isBreak(n *html.Node) bool { return n.Type == html.CommentNode && n.Data == "pagebreak" }

// holdsBlocks reports whether a division holds blocks or page breaks, so
// that it is only a wrapper.
func holdsBlocks(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if isBlock(ch) || isBreak(ch) {
			return true
		}
		if ch.Type == html.ElementNode && !isBlock(ch) && holdsBlocks(ch) {
			return true
		}
	}
	return false
}

// unit is a block of the text that is written as a whole.
type unit struct {
	html     []byte
	level    int
	heading  string
	headID   string
	pageEnds bool
	ids      []string
}

// units flattens the cleaned nodes into blocks: wrapping divisions are
// opened, and runs of text and inline elements between blocks make a
// paragraph of their own.
func (c *converter) units(nodes []*html.Node) []unit {
	var out []unit
	var loose []*html.Node
	flush := func() {
		if len(loose) == 0 {
			return
		}
		p := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
		c.adopt(p, loose)
		loose = nil
		if blank(p) {
			return
		}
		out = append(out, c.unitOf(p))
	}
	var walk func(nodes []*html.Node)
	walk = func(nodes []*html.Node) {
		for _, n := range nodes {
			switch {
			case isBreak(n):
				flush()
				out = append(out, unit{pageEnds: true})
			case n.Type == html.ElementNode && n.DataAtom == atom.Div && holdsBlocks(n):
				flush()
				var kids []*html.Node
				for ch := n.FirstChild; ch != nil; {
					next := ch.NextSibling
					n.RemoveChild(ch)
					kids = append(kids, ch)
					ch = next
				}
				if id := attrOf(n, "id"); id != "" {
					kids = append([]*html.Node{{Type: html.ElementNode, Data: "a", DataAtom: atom.A, Attr: []html.Attribute{{Key: "id", Val: id}}}}, kids...)
				}
				walk(kids)
				flush()
			case isBlock(n):
				flush()
				if !blank(n) || n.DataAtom == atom.Hr {
					out = append(out, c.unitOf(n))
				}
			default:
				loose = append(loose, n)
			}
		}
	}
	walk(nodes)
	flush()
	return out
}

// blank reports whether a node holds no text and nothing to show.
func blank(n *html.Node) bool {
	switch {
	case n.Type == html.TextNode:
		return strings.TrimSpace(n.Data) == ""
	case n.Type == html.ElementNode && (n.DataAtom == atom.Img || n.DataAtom == atom.Hr):
		return false
	case n.Type == html.ElementNode && n.DataAtom == atom.A && attrOf(n, "id") != "":
		return false
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if !blank(ch) {
			return false
		}
	}
	return true
}

func (c *converter) unitOf(n *html.Node) unit {
	u := unit{}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3:
		u.level = int(n.Data[1] - '0')
		u.heading = strings.Join(strings.Fields(textOf(n)), " ")
		if u.heading != "" {
			id := attrOf(n, "id")
			if id == "" {
				for {
					c.headings++
					id = "toc-" + strconv.Itoa(c.headings)
					if !c.ids[id] {
						break
					}
				}
				c.ids[id] = true
				n.Attr = append(n.Attr, html.Attribute{Key: "id", Val: id})
			}
			u.headID = id
		}
	}
	var buf bytes.Buffer
	_ = html.Render(&buf, n)
	u.html = buf.Bytes()
	collectIDs(n, &u.ids)
	return u
}

func collectIDs(n *html.Node, into *[]string) {
	if n.Type == html.ElementNode {
		if id := attrOf(n, "id"); id != "" {
			*into = append(*into, id)
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		collectIDs(ch, into)
	}
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		sb.WriteString(textOf(ch))
	}
	return sb.String()
}

// section is one content document.
type section struct {
	units []unit
	size  int
}

// cut divides the blocks into documents: at page breaks and before a
// chapter heading, as long as the document holds a few kilobytes, and
// wherever it grows too large.
func (c *converter) cut(units []unit) []*section {
	var out []*section
	cur := &section{}
	finish := func() {
		if len(cur.units) > 0 {
			out = append(out, cur)
		}
		cur = &section{}
	}
	for _, u := range units {
		switch {
		case u.pageEnds:
			if cur.size >= minSection {
				finish()
			}
			continue
		case u.level > 0 && u.level <= 2 && cur.size >= minSection:
			finish()
		case cur.size > 0 && cur.size+len(u.html) > sectionBytes:
			finish()
		}
		cur.units = append(cur.units, u)
		cur.size += len(u.html)
	}
	finish()
	return out
}

// tocEntry is an entry of the table of contents.
type tocEntry struct {
	title, href string
	level       int
	children    []*tocEntry
}

var hrefInSection = regexp.MustCompile(`<a href="#([^"]*)"`)

// write puts the EPUB together.
func (c *converter) write(w io.Writer, book *Book, sections []*section, modified time.Time) (Converted, error) {
	m := book.Metadata
	title := m.Title
	if title == "" {
		title = "Untitled"
	}
	lang := m.Language
	if lang == "" {
		lang = "und"
	}
	where := map[string]int{}
	for i, s := range sections {
		for _, u := range s.units {
			for _, id := range u.ids {
				where[id] = i
			}
		}
	}
	name := func(i int) string { return fmt.Sprintf("section-%04d.xhtml", i+1) }
	var flat []*tocEntry
	for i, s := range sections {
		for _, u := range s.units {
			if u.headID != "" {
				flat = append(flat, &tocEntry{title: u.heading, href: name(i) + "#" + u.headID, level: u.level})
			}
		}
	}
	if len(flat) == 0 {
		for i := range sections {
			t := title
			if len(sections) > 1 {
				t = strconv.Itoa(i + 1)
			}
			flat = append(flat, &tocEntry{title: t, href: name(i), level: 1})
		}
	}
	toc := nest(flat)

	zw := zip.NewWriter(w)
	var werr error
	put := func(entry string, data []byte, method uint16) {
		if werr != nil {
			return
		}
		o, err := zw.CreateHeader(&zip.FileHeader{Name: entry, Method: method, Modified: modified})
		if err == nil {
			_, err = o.Write(data)
		}
		werr = err
	}
	put("mimetype", []byte("application/epub+zip"), zip.Store)
	put("META-INF/container.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
`), zip.Deflate)

	for i, s := range sections {
		var body bytes.Buffer
		for _, u := range s.units {
			body.Write(u.html)
			body.WriteByte('\n')
		}
		text := hrefInSection.ReplaceAllStringFunc(body.String(), func(m string) string {
			id := hrefInSection.FindStringSubmatch(m)[1]
			target, ok := where[unescapeAttr(id)]
			switch {
			case !ok:
				c.dropped++
				return "<a"
			case target == i:
				return m
			}
			return `<a href="` + name(target) + "#" + id + `"`
		})
		var doc bytes.Buffer
		fmt.Fprintf(&doc, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" lang=%q xml:lang=%q>
<head><meta charset="utf-8"/><title>%s</title></head>
<body>
%s</body>
</html>
`, lang, lang, escape(title), text)
		put("OEBPS/"+name(i), doc.Bytes(), zip.Deflate)
	}

	used := c.usedImages()
	for _, im := range used {
		put("OEBPS/images/"+im.name, im.data, zip.Deflate)
	}
	put("OEBPS/nav.xhtml", navDocument(lang, title, toc), zip.Deflate)
	put("OEBPS/toc.ncx", ncxDocument(title, identifierOf(book), toc), zip.Deflate)
	put("OEBPS/content.opf", c.packageDocument(book, title, lang, len(sections), name, used, modified), zip.Deflate)
	if werr != nil {
		return Converted{}, werr
	}
	if err := zw.Close(); err != nil {
		return Converted{}, err
	}
	return Converted{Sections: len(sections), Images: len(used), TOCEntries: len(flat), LinksDropped: c.dropped}, nil
}

func unescapeAttr(s string) string {
	return html.UnescapeString(s)
}

// usedImages are the images the text shows and the cover, in the order of
// their records.
func (c *converter) usedImages() []*picture {
	var out []*picture
	for _, im := range c.images {
		if im != nil {
			out = append(out, im)
		}
	}
	slices.SortFunc(out, func(a, b *picture) int { return a.record - b.record })
	return out
}

func nest(flat []*tocEntry) []*tocEntry {
	var root []*tocEntry
	var stack []*tocEntry
	for _, e := range flat {
		for len(stack) > 0 && stack[len(stack)-1].level >= e.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			root = append(root, e)
		} else {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, e)
		}
		stack = append(stack, e)
	}
	return root
}

func escape(s string) string {
	var sb strings.Builder
	_ = xmlEscape(&sb, s)
	return sb.String()
}

func xmlEscape(w io.Writer, s string) error {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	_, err := io.WriteString(w, r.Replace(xmlText(s)))
	return err
}

func navDocument(lang, title string, toc []*tocEntry) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang=%q xml:lang=%q>
<head><meta charset="utf-8"/><title>%s</title></head>
<body>
<nav epub:type="toc" id="toc">
`, lang, lang, escape(title))
	var list func(entries []*tocEntry)
	list = func(entries []*tocEntry) {
		sb.WriteString("<ol>\n")
		for _, e := range entries {
			fmt.Fprintf(&sb, `<li><a href="%s">%s</a>`, escape(e.href), escape(e.title))
			if len(e.children) > 0 {
				sb.WriteString("\n")
				list(e.children)
			}
			sb.WriteString("</li>\n")
		}
		sb.WriteString("</ol>\n")
	}
	list(toc)
	sb.WriteString("</nav>\n</body>\n</html>\n")
	return []byte(sb.String())
}

func ncxDocument(title, id string, toc []*tocEntry) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
<head><meta name="dtb:uid" content="%s"/></head>
<docTitle><text>%s</text></docTitle>
<navMap>
`, escape(id), escape(title))
	order := 0
	var points func(entries []*tocEntry)
	points = func(entries []*tocEntry) {
		for _, e := range entries {
			order++
			fmt.Fprintf(&sb, `<navPoint id="np-%d" playOrder="%d"><navLabel><text>%s</text></navLabel><content src="%s"/>`, order, order, escape(e.title), escape(e.href))
			points(e.children)
			sb.WriteString("</navPoint>\n")
		}
	}
	points(toc)
	sb.WriteString("</navMap>\n</ncx>\n")
	return []byte(sb.String())
}

// identifierOf is the book's identifier in the EPUB: the same for every
// conversion of the same text.
func identifierOf(book *Book) string {
	return "urn:uuid:" + uuid.NewSHA1(uuid.NameSpaceOID, book.ContentHash).String()
}

func (c *converter) packageDocument(book *Book, title, lang string, sections int, name func(int) string, images []*picture, modified time.Time) []byte {
	m := book.Metadata
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid" xml:lang=%q>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">%s</dc:identifier>
`, lang, escape(identifierOf(book)))
	if m.ISBN != "" {
		fmt.Fprintf(&sb, "    <dc:identifier>urn:isbn:%s</dc:identifier>\n", escape(m.ISBN))
	}
	fmt.Fprintf(&sb, "    <dc:title>%s</dc:title>\n    <dc:language>%s</dc:language>\n", escape(title), escape(lang))
	for _, a := range m.Authors {
		fmt.Fprintf(&sb, "    <dc:creator>%s</dc:creator>\n", escape(a))
	}
	for _, a := range m.Contributors {
		fmt.Fprintf(&sb, "    <dc:contributor>%s</dc:contributor>\n", escape(a))
	}
	if m.Publisher != "" {
		fmt.Fprintf(&sb, "    <dc:publisher>%s</dc:publisher>\n", escape(m.Publisher))
	}
	if m.Description != "" {
		fmt.Fprintf(&sb, "    <dc:description>%s</dc:description>\n", escape(m.Description))
	}
	if d := isoDate(m.Published); d != "" {
		fmt.Fprintf(&sb, "    <dc:date>%s</dc:date>\n", d)
	}
	for _, s := range m.Subjects {
		fmt.Fprintf(&sb, "    <dc:subject>%s</dc:subject>\n", escape(s))
	}
	if c.cover != nil {
		sb.WriteString(`    <meta name="cover" content="cover-image"/>` + "\n")
	}
	fmt.Fprintf(&sb, "    <meta property=\"dcterms:modified\">%s</meta>\n  </metadata>\n  <manifest>\n", modified.UTC().Format("2006-01-02T15:04:05Z"))
	sb.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	sb.WriteString(`    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n")
	for i := range sections {
		fmt.Fprintf(&sb, `    <item id="s%d" href="%s" media-type="application/xhtml+xml"/>`+"\n", i+1, name(i))
	}
	for _, im := range images {
		id, props := fmt.Sprintf("img-%04d", im.record), ""
		if im == c.cover {
			id, props = "cover-image", ` properties="cover-image"`
		}
		fmt.Fprintf(&sb, `    <item id="%s" href="images/%s" media-type="%s"%s/>`+"\n", id, im.name, im.media, props)
	}
	sb.WriteString("  </manifest>\n  <spine toc=\"ncx\">\n")
	for i := range sections {
		fmt.Fprintf(&sb, `    <itemref idref="s%d"/>`+"\n", i+1)
	}
	sb.WriteString("  </spine>\n</package>\n")
	return []byte(sb.String())
}

var dateStart = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?`)

// isoDate is the part of a date as written that Dublin Core can carry.
func isoDate(s string) string { return dateStart.FindString(strings.TrimSpace(s)) }
