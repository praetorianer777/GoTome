package epub

import (
	"bytes"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// tocFile is a table of contents: the NCX of EPUB 2 or an EPUB 3
// navigation document, by its archive path.
type tocFile struct {
	name string
	ncx  bool
}

// tocFiles are the package's tables of contents. Readers use one or the
// other, so both are checked where a book has both.
func tocFiles(opfPath string, pkg *opfPackage) []tocFile {
	var out []tocFile
	seen := map[string]bool{}
	add := func(it *opfItem, ncx bool) {
		if it == nil {
			return
		}
		name, err := resolve(opfPath, it.Href)
		if err != nil || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, tocFile{name: name, ncx: ncx})
	}
	for i := range pkg.Manifest {
		if pkg.Manifest[i].hasProperty("nav") {
			add(&pkg.Manifest[i], false)
		}
	}
	add(pkg.item(pkg.Spine.Toc), true)
	for i := range pkg.Manifest {
		if pkg.Manifest[i].MediaType == "application/x-dtbncx+xml" {
			add(&pkg.Manifest[i], true)
		}
	}
	return out
}

// linkState is where a reference of a table of contents leads.
type linkState int

const (
	leadsThere linkState = iota
	// noDocument names a document the archive does not hold.
	noDocument
	// noFragment names a fragment no element of its document has.
	noFragment
	// notChecked is a reference to elsewhere, or empty.
	notChecked
)

// links follows references inside an archive. find says what an entry is
// called in the archive, if it holds one; read gives its content.
type links struct {
	find func(name string) (string, bool)
	read func(name string) ([]byte, error)
	ids  map[string]map[string]bool
}

// follow says where a reference from the document at base leads, and the
// entry it names.
func (l *links) follow(base, href string) (linkState, string) {
	href = strings.TrimSpace(href)
	if href == "" {
		return notChecked, ""
	}
	if u, err := url.Parse(href); err != nil || u.Scheme != "" || u.Host != "" {
		return notChecked, ""
	}
	target, fragment, _ := strings.Cut(href, "#")
	name := base
	if target != "" {
		var err error
		if name, err = resolve(base, target); err != nil {
			return noDocument, target
		}
	}
	found, ok := l.find(name)
	if !ok {
		return noDocument, name
	}
	if fragment == "" || !isMarkupName(found) {
		return leadsThere, found
	}
	if unescaped, err := url.PathUnescape(fragment); err == nil {
		fragment = unescaped
	}
	if !l.idsOf(found)[fragment] {
		return noFragment, found + "#" + fragment
	}
	return leadsThere, found
}

func isMarkupName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".xhtml", ".html", ".htm":
		return true
	}
	return false
}

// idsOf are the ids of a document's elements and the names of its
// anchors, read as HTML so that a document that is not well formed has
// them too.
func (l *links) idsOf(name string) map[string]bool {
	if ids, ok := l.ids[name]; ok {
		return ids
	}
	ids := map[string]bool{}
	l.ids[name] = ids
	data, err := l.read(name)
	if err != nil {
		return ids
	}
	z := html.NewTokenizer(bytes.NewReader(data))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tag, hasAttr := z.TagName()
		isAnchor := string(tag) == "a"
		for hasAttr {
			var key, val []byte
			key, val, hasAttr = z.TagAttr()
			if k := string(key); k == "id" || k == "name" && isAnchor {
				ids[string(val)] = true
			}
		}
	}
	return ids
}

// tocHrefs are the references of a table of contents: the targets of an
// NCX's navMap, or the links of a navigation document's nav elements. It
// reads them leniently, as readers do.
func tocHrefs(data []byte, ncx bool) []string {
	var out []string
	if ncx {
		var doc struct {
			Points []navPoint `xml:"navMap>navPoint"`
		}
		if decodeXML(data, &doc) != nil {
			return nil
		}
		var walk func(points []navPoint, depth int)
		walk = func(points []navPoint, depth int) {
			if depth > 32 {
				return
			}
			for _, p := range points {
				out = append(out, p.Content.Src)
				walk(p.Points, depth+1)
			}
		}
		walk(doc.Points, 0)
		return out
	}
	root, err := html.Parse(bytes.NewReader(bytes.ToValidUTF8(data, replacement)))
	if err != nil {
		return nil
	}
	var walk func(n *html.Node, inNav bool)
	walk = func(n *html.Node, inNav bool) {
		if n.Type == html.ElementNode {
			switch {
			case n.DataAtom == atom.Nav:
				inNav = true
			case inNav && n.DataAtom == atom.A:
				for _, a := range n.Attr {
					if a.Key == "href" && a.Namespace == "" {
						out = append(out, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inNav)
		}
	}
	walk(root, false)
	return out
}

// brokenEntries are the targets of a table of contents that lead nowhere,
// each once.
func (l *links) brokenEntries(toc tocFile, data []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, href := range tocHrefs(data, toc.ncx) {
		state, target := l.follow(toc.name, href)
		if (state == noDocument || state == noFragment) && !seen[target] {
			seen[target] = true
			out = append(out, target)
		}
	}
	return out
}
