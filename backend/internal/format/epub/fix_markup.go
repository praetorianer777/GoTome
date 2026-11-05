package epub

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// repairDocuments writes every XHTML document that is not well formed
// again from an HTML parse of it, as browsers read it, with elements XML
// writes as empty kept empty. One is kept only when the result is well
// formed and has the text and the ids of the original in the order they
// are written: where the parser moves or drops text to make a tree of
// it, the document is left as it is.
func (fx *fixer) repairDocuments() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	for _, it := range l.items {
		if it.entry == "" || !fx.exists(it.entry) || !(&opfItem{MediaType: it.mediaType, Href: it.entry}).isXHTML() {
			continue
		}
		data, err := fx.content(it.entry)
		if err != nil || !malformed(data) {
			continue
		}
		if repaired, ok := reserialize(data); ok {
			fx.set(it.entry, repaired)
			fx.fixed.DocumentsRepaired++
		}
	}
	return nil
}

var (
	xmlDecl = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)
	// internalSubset is a document type with declarations of its own,
	// which an HTML parse drops.
	internalSubset = regexp.MustCompile(`(?is)<!DOCTYPE[^>\[]*\[`)
)

// voidElements are HTML's elements without content, which an HTML parser
// takes as empty however they are written.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

const xhtmlNamespace = "http://www.w3.org/1999/xhtml"

// reserialize writes a document again as well-formed XHTML in UTF-8; false
// when that is not known to leave what it says.
func reserialize(data []byte) ([]byte, bool) {
	if internalSubset.Match(data) {
		return nil, false
	}
	utf, err := toUTF8(bytes.TrimPrefix(data, utf8BOM))
	if err != nil {
		return nil, false
	}
	expanded := expandSelfClosing(xmlDecl.ReplaceAll(utf, nil))
	before, ok := tokenTextAndIDs(expanded)
	if !ok {
		return nil, false
	}
	root, err := html.ParseWithOptions(bytes.NewReader(expanded), html.ParseOptionEnableScripting(false))
	if err != nil {
		return nil, false
	}
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type != html.ElementNode || n.DataAtom != atom.Html {
			continue
		}
		hasNamespace := false
		for _, a := range n.Attr {
			hasNamespace = hasNamespace || a.Namespace == "" && a.Key == "xmlns"
		}
		if !hasNamespace {
			n.Attr = append(n.Attr, html.Attribute{Key: "xmlns", Val: xhtmlNamespace})
		}
	}
	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	if err := html.Render(&out, root); err != nil {
		return nil, false
	}
	if checkWellFormed(out.Bytes()) != nil {
		return nil, false
	}
	after, err := xmlTextAndIDs(out.Bytes())
	if err != nil || !before.equal(after) {
		return nil, false
	}
	return out.Bytes(), true
}

// expandSelfClosing writes an element other than a void one that is
// written as empty, <a id="x"/>, with its end tag: XML reads it as empty,
// an HTML parser as the start of an element that holds what follows.
// Everything else is copied as it is.
func expandSelfClosing(data []byte) []byte {
	var out bytes.Buffer
	z := html.NewTokenizer(bytes.NewReader(data))
	at := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := z.Raw()
		at += len(raw)
		if tt != html.SelfClosingTagToken {
			out.Write(raw)
			continue
		}
		name, _ := z.TagName()
		if voidElements[string(name)] {
			out.Write(raw)
			continue
		}
		out.Write(bytes.TrimSuffix(raw, []byte("/>")))
		out.WriteString("></" + string(name) + ">")
		// The tokenizer reads what follows <title/> or <script/> as their
		// text, up to an end tag that is not there; a new one does not.
		z = html.NewTokenizer(bytes.NewReader(data[at:]))
	}
	return out.Bytes()
}

// tocEntry is an entry of a table of contents and where it lies: an NCX
// navPoint, or a navigation document's li.
type tocEntry struct {
	span
	// href is the reference of its content element or link, as decoded,
	// and hrefAt where its value lies inside the quotes; hasHref is false
	// for a heading without a link.
	href     string
	hrefAt   span
	hasHref  bool
	children []*tocEntry
	// list is the ol or ul of a nav entry that holds its children.
	list *span
}

var attrValue = map[string]*regexp.Regexp{
	"src":  regexp.MustCompile(`\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)')`),
	"href": regexp.MustCompile(`\shref\s*=\s*(?:"([^"]*)"|'([^']*)')`),
}

// valueSpan is where an attribute's value lies inside a tag.
func valueSpan(data []byte, tag span, attr string) (span, bool) {
	m := attrValue[attr].FindSubmatchIndex(data[tag.start:tag.end])
	if m == nil {
		return span{}, false
	}
	from, to := m[2], m[3]
	if from < 0 {
		from, to = m[4], m[5]
	}
	return span{tag.start + int64(from), tag.start + int64(to)}, true
}

// parseTOC reads the entries of a well-formed table of contents with where
// each lies: the navPoints of an NCX's navMap, the li of a navigation
// document's nav elements.
func parseTOC(data []byte, ncx bool) ([]*tocEntry, error) {
	check := xml.NewDecoder(bytes.NewReader(data))
	check.Entity = xml.HTMLEntity
	for {
		if _, err := check.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	type frame struct {
		name   string
		entry  *tocEntry
		listOf *tocEntry
	}
	var stack []frame
	var top []*tocEntry
	inside := func(name string) bool {
		for _, f := range stack {
			if f.name == name {
				return true
			}
		}
		return false
	}
	current := func() (*tocEntry, bool) {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].entry != nil {
				return stack[i].entry, i == len(stack)-1
			}
		}
		return nil, false
	}
	entryName, scope, link, attr := "li", "nav", "a", "href"
	if ncx {
		entryName, scope, link, attr = "navpoint", "navmap", "content", "src"
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Entity = xml.HTMLEntity
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
			f := frame{name: strings.ToLower(t.Name.Local)}
			cur, direct := current()
			switch {
			case f.name == entryName && inside(scope):
				f.entry = &tocEntry{span: span{start, end}}
				if cur != nil {
					cur.children = append(cur.children, f.entry)
				} else {
					top = append(top, f.entry)
				}
			case f.name == link && cur != nil && !cur.hasHref:
				for _, a := range t.Attr {
					if a.Name.Space == "" && a.Name.Local == attr {
						if at, ok := valueSpan(data, span{start, end}, attr); ok {
							cur.href, cur.hrefAt, cur.hasHref = a.Value, at, true
						}
					}
				}
			case !ncx && (f.name == "ol" || f.name == "ul") && cur != nil && direct && cur.list == nil:
				cur.list = &span{start, end}
				f.listOf = cur
			}
			if bytes.HasSuffix(data[start:end], []byte("/>")) {
				if _, err := d.RawToken(); err != nil {
					return nil, err
				}
				if f.entry != nil {
					f.entry.end = end
				}
				continue
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("an end tag without its start")
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.entry != nil {
				f.entry.end = end
			}
			if f.listOf != nil {
				f.listOf.list.end = end
			}
		}
	}
	return top, nil
}

// tocRepair is what repairing one table of contents comes to.
type tocRepair struct {
	data  []byte
	base  string
	links *links
}

// pass works through entries and returns the references of those it keeps,
// in order, how many it keeps at all depths, and the edits that keep them.
// An entry whose fragment no element has points at its document's start;
// one whose document is missing points at the first entry below it that
// leads somewhere, or goes with what is below it when none does. A
// heading whose entries all go goes too, as does a list left empty.
func (r *tocRepair) pass(entries []*tocEntry) (edits []edit, hrefs []string, kept int, removed int, repointed int) {
	for _, e := range entries {
		state := leadsThere
		if e.hasHref {
			state, _ = r.links.follow(r.base, e.href)
		}
		href := e.href
		if state == noFragment {
			href, _, _ = strings.Cut(href, "#")
			if href == "" {
				href = relativeHref(r.base, r.base)
			}
		}
		childEdits, childHrefs, childKept, childRemoved, childRepointed := r.pass(e.children)
		lost := state == noDocument && len(childHrefs) == 0 || !e.hasHref && len(e.children) > 0 && childKept == 0
		if lost {
			edits = append(edits, edit{span: lineBefore(r.data, e.span)})
			removed += 1 + childRemoved + childKept
			continue
		}
		if state == noDocument {
			href = childHrefs[0]
		}
		if href != e.href {
			edits = append(edits, edit{span: e.hrefAt, text: escape(href)})
			repointed++
		}
		if len(e.children) > 0 && childKept == 0 && e.list != nil {
			edits = append(edits, edit{span: lineBefore(r.data, *e.list)})
		} else {
			edits = append(edits, childEdits...)
		}
		removed += childRemoved
		repointed += childRepointed
		kept += 1 + childKept
		if e.hasHref {
			hrefs = append(hrefs, href)
		}
		hrefs = append(hrefs, childHrefs...)
	}
	return edits, hrefs, kept, removed, repointed
}

// repairTOC mends the entries of the tables of contents that lead nowhere
// (tocRepair.pass). A table is changed only when it is well formed before
// and after, the entries left lead somewhere, and every entry that led
// somewhere before still does; one that would be left without entries
// stays as it is.
func (fx *fixer) repairTOC() error {
	pkg, err := parsePackage(fx.opf)
	if err != nil {
		return nil
	}
	l := &links{
		find: func(name string) (string, bool) {
			if _, ok := fx.data[name]; ok && !fx.removed[name] {
				return name, true
			}
			if f := fx.a.find(name); f != nil && !fx.removed[f.Name] {
				return f.Name, true
			}
			return "", false
		},
		read: fx.content,
		ids:  map[string]map[string]bool{},
	}
	for _, toc := range tocFiles(fx.opfPath, pkg) {
		name, ok := l.find(toc.name)
		if !ok {
			continue
		}
		toc.name = name
		data, err := fx.content(name)
		if err != nil {
			continue
		}
		entries, err := parseTOC(data, toc.ncx)
		if err != nil {
			continue
		}
		r := &tocRepair{data: data, base: name, links: l}
		edits, _, kept, removed, repointed := r.pass(entries)
		if len(edits) == 0 || kept == 0 {
			continue
		}
		changed := applyEdits(data, edits)
		if _, err := parseTOC(changed, toc.ncx); err != nil || len(l.brokenEntries(toc, changed)) > 0 ||
			!keepsWhatLeads(l, toc, data, changed) {
			continue
		}
		fx.set(name, changed)
		fx.fixed.TocEntriesRemoved += removed
		fx.fixed.TocEntriesRepointed += repointed
	}
	return nil
}

// keepsWhatLeads reports whether every reference of a table of contents
// that led somewhere is in the changed one as often.
func keepsWhatLeads(l *links, toc tocFile, before, after []byte) bool {
	count := map[string]int{}
	for _, href := range tocHrefs(after, toc.ncx) {
		count[href]++
	}
	for _, href := range tocHrefs(before, toc.ncx) {
		if state, _ := l.follow(toc.name, href); state == leadsThere {
			if count[href] == 0 {
				return false
			}
			count[href]--
		}
	}
	return true
}
