package epub

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// SplitBytes is the most a part of a split content document holds, as
// calibre's flow size: well under MaxDocumentBytes.
const SplitBytes = 260 << 10

// element is an element of a content document and where it lies.
type element struct {
	name string
	// start and tagEnd bound its start tag, closeStart and end its end
	// tag; for an empty element all but start are tagEnd.
	start, tagEnd, closeStart, end int64
	children                       []*element
	// ids are the id attribute and an anchor's name.
	ids []string
}

// parseElements reads a well-formed document into its elements, with the
// body element. A document that is not well formed is not split.
func parseElements(data []byte) (*element, error) {
	check := xml.NewDecoder(bytes.NewReader(data))
	check.Entity = xml.HTMLEntity
	for {
		if _, err := check.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Entity = xml.HTMLEntity
	var stack []*element
	var body *element
	for {
		start := d.InputOffset()
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		end := d.InputOffset()
		switch t := tok.(type) {
		case xml.StartElement:
			e := &element{name: strings.ToLower(t.Name.Local), start: start, tagEnd: end}
			for _, at := range t.Attr {
				if at.Name.Local == "id" || (at.Name.Local == "name" && e.name == "a") {
					e.ids = append(e.ids, at.Value)
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, e)
			}
			if e.name == "body" && body == nil {
				body = e
			}
			stack = append(stack, e)
			if bytes.HasSuffix(data[start:end], []byte("/>")) {
				if _, err := d.RawToken(); err != nil {
					return nil, err
				}
				e.closeStart, e.end = end, end
				stack = stack[:len(stack)-1]
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("an end tag without its start")
			}
			e := stack[len(stack)-1]
			e.closeStart, e.end = start, end
			stack = stack[:len(stack)-1]
		}
	}
	if body == nil || body.end == body.tagEnd {
		return nil, errors.New("no body")
	}
	return body, nil
}

var headings = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}

// startsChapter reports whether a part may well begin with the element: a
// heading, or a block whose first element is one.
func startsChapter(e *element) bool {
	if headings[e.name] {
		return true
	}
	switch e.name {
	case "section", "div", "article", "header":
		return len(e.children) > 0 && startsChapter(e.children[0])
	}
	return false
}

// piece is one part of a split document: the elements it is wrapped in,
// from the body inwards, and the stretch of the innermost one it holds.
type piece struct {
	wrappers []*element
	from, to int64
}

// cutter cuts a document's body into pieces of at most limit bytes.
type cutter struct {
	data   []byte
	limit  int64
	pieces []piece
}

func (c *cutter) emit(wrappers []*element, from, to int64) {
	if to <= from || len(bytes.TrimSpace(c.data[from:to])) == 0 {
		return
	}
	c.pieces = append(c.pieces, piece{wrappers: wrappers, from: from, to: to})
}

// cut walks the children of n: they are gathered into pieces until one
// would grow past the limit, which is then cut before the last chapter
// that begins in its latter three quarters, or else before the child. A
// child too large on its own is cut inside, wrapped as it was.
func (c *cutter) cut(n *element, outer []*element) {
	wrappers := append(append([]*element{}, outer...), n)
	from := n.tagEnd
	lastChapter := int64(-1)
	for _, child := range n.children {
		if child.end-child.start > c.limit && len(child.children) > 0 {
			c.emit(wrappers, from, child.start)
			c.cut(child, wrappers)
			from, lastChapter = child.end, -1
			continue
		}
		if child.end-from > c.limit && child.start > from {
			at := child.start
			if lastChapter > from+c.limit/4 {
				at = lastChapter
			}
			c.emit(wrappers, from, at)
			from, lastChapter = at, -1
		}
		if startsChapter(child) && child.start > from {
			lastChapter = child.start
		}
	}
	c.emit(wrappers, from, n.closeStart)
}

// partName is the entry of the nth part of a split document, beside it.
func partName(doc string, n int, taken func(string) bool) string {
	dir, base := path.Split(doc)
	ext := path.Ext(base)
	stem := strings.Map(func(r rune) rune {
		if r < 0x80 && (r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, strings.TrimSuffix(base, ext))
	for k := n; ; k += 1000 {
		name := dir + stem + "-gotome-" + strconv.Itoa(k) + ext
		if !taken(name) {
			return name
		}
	}
}

// splitDocument cuts one content document into parts, each a whole
// document with the head and the wrappers of the original. It returns the
// parts' bytes, the first being the original's, and which part holds each
// id; nil when the document does not split.
func splitDocument(data []byte, limit int64) ([][]byte, map[string]int) {
	body, err := parseElements(data)
	if err != nil {
		return nil, nil
	}
	c := &cutter{data: data, limit: limit}
	c.cut(body, nil)
	if len(c.pieces) < 2 {
		return nil, nil
	}
	parts := make([][]byte, len(c.pieces))
	where := map[string]int{}
	for i, p := range c.pieces {
		var b bytes.Buffer
		b.Write(data[:body.start])
		for _, w := range p.wrappers {
			b.Write(data[w.start:w.tagEnd])
		}
		b.Write(data[p.from:p.to])
		for j := len(p.wrappers) - 1; j >= 0; j-- {
			w := p.wrappers[j]
			b.Write(data[w.closeStart:w.end])
		}
		b.Write(data[body.end:])
		parts[i] = b.Bytes()
		for _, w := range p.wrappers {
			for _, id := range w.ids {
				if _, seen := where[id]; !seen {
					where[id] = i
				}
			}
		}
	}
	var walk func(e *element)
	walk = func(e *element) {
		for _, id := range e.ids {
			for i, p := range c.pieces {
				if e.start >= p.from && e.start < p.to {
					where[id] = i
					break
				}
			}
		}
		for _, child := range e.children {
			walk(child)
		}
	}
	walk(body)
	return parts, where
}

// reference is an attribute that points somewhere: href, src or
// xlink:href.
var idrefAttr = regexp.MustCompile(`\s+idref\s*=\s*("[^"]*"|'[^']*')`)

var reference = regexp.MustCompile(`(\b(?:href|src|xlink:href)\s*=\s*)("[^"]*"|'[^']*')`)

// splitDocuments splits every content document of the spine larger than
// MaxDocumentBytes into parts of at most SplitBytes, at chapters where it
// can, and points what led into a part that moved there: links of the
// documents, the table of contents and the guide.
func (fx *fixer) splitDocuments() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	byID := map[string]*manifestEntry{}
	for i := range l.items {
		byID[l.items[i].attrs["id"]] = &l.items[i]
	}
	// moved maps a split document to its parts' names and which part
	// holds each id.
	type split struct {
		parts []string
		where map[string]int
	}
	moved := map[string]split{}
	var edits []edit
	for _, ref := range l.refs {
		it := byID[ref.idref]
		if it == nil || it.entry == "" || !fx.exists(it.entry) || !(&opfItem{MediaType: it.mediaType, Href: it.entry}).isDocument() {
			continue
		}
		if _, done := moved[it.entry]; done {
			continue
		}
		data, err := fx.content(it.entry)
		if err != nil || int64(len(data)) <= MaxDocumentBytes {
			continue
		}
		parts, where := splitDocument(data, SplitBytes)
		if parts == nil {
			continue
		}
		s := split{parts: []string{it.entry}, where: where}
		fx.set(it.entry, parts[0])
		var items, refs strings.Builder
		for n := 1; n < len(parts); n++ {
			name := partName(it.entry, n+1, fx.exists)
			fx.set(name, parts[n])
			s.parts = append(s.parts, name)
			id := it.attrs["id"] + "-gotome-" + strconv.Itoa(n+1)
			for k := 2; l.ids[id]; k++ {
				id = it.attrs["id"] + "-gotome-" + strconv.Itoa(n+1) + "-" + strconv.Itoa(k)
			}
			l.ids[id] = true
			fmt.Fprintf(&items, "\n    <%sitem id=\"%s\" href=\"%s\" media-type=\"%s\"/>", l.itemPrefix, escape(id),
				escape(relativeHref(fx.opfPath, name)), escape(it.attrs["media-type"]))
			tag := string(fx.opf[ref.start:ref.end])
			refs.WriteString("\n    " + idrefAttr.ReplaceAllLiteralString(tag, ` idref="`+escape(id)+`"`))
		}
		moved[it.entry] = s
		edits = append(edits, edit{span: span{it.end, it.end}, text: items.String()}, edit{span: span{ref.end, ref.end}, text: refs.String()})
		fx.fixed.DocumentsSplit++
		fx.fixed.PartsAdded += len(parts) - 1
	}
	if len(moved) == 0 {
		return nil
	}
	fx.editPackage(edits)

	// Every document that may hold links, and the table of contents, is
	// pointed at the parts; a split document's parts point at each other.
	repoint := func(doc string, data []byte, self int) []byte {
		return reference.ReplaceAllFunc(data, func(m []byte) []byte {
			sub := reference.FindSubmatch(m)
			quote := sub[2][0]
			value := string(sub[2][1 : len(sub[2])-1])
			target, fragment, hasFragment := strings.Cut(value, "#")
			if !hasFragment || strings.Contains(target, "://") {
				return m
			}
			resolved := doc
			if target != "" {
				var err error
				if resolved, err = resolve(doc, target); err != nil {
					return m
				}
				if f := fx.a.find(resolved); f != nil {
					resolved = f.Name
				}
			}
			s, ok := moved[resolved]
			if !ok {
				return m
			}
			id := html.UnescapeString(fragment)
			if unescaped, err := url.PathUnescape(id); err == nil {
				id = unescaped
			}
			part, ok := s.where[id]
			if !ok || (target == "" && part == self) {
				return m
			}
			href := relativeHref(doc, s.parts[part]) + "#" + fragment
			return append(append(append([]byte{}, sub[1]...), quote), append([]byte(href), quote)...)
		})
	}
	for _, it := range l.items {
		isDoc := (&opfItem{MediaType: it.mediaType, Href: it.entry}).isDocument()
		if it.entry == "" || !fx.exists(it.entry) || !isDoc && it.mediaType != "application/x-dtbncx+xml" {
			continue
		}
		if s, ok := moved[it.entry]; ok {
			for i, name := range s.parts {
				fx.data[name] = repoint(it.entry, fx.data[name], i)
			}
			continue
		}
		data, err := fx.content(it.entry)
		if err != nil {
			continue
		}
		if changed := repoint(it.entry, data, -1); !bytes.Equal(changed, data) {
			fx.set(it.entry, changed)
		}
	}
	fx.opf = repoint(fx.opfPath, fx.opf, -1)
	return nil
}
