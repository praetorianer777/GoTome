package epub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrNotWritable is returned for an EPUB that Rewrite will not change: one
// whose content is encrypted, or whose package document it cannot rewrite
// without changing what it does not understand.
var ErrNotWritable = errors.New("EPUB cannot be rewritten")

// Update is what Rewrite writes into an EPUB.
type Update struct {
	// Metadata replaces what the package document says about the book.
	// Version is not written. Contributors' roles are MARC relator codes.
	// An empty Language leaves the file's, which a package must have. EPUB 2
	// has no subtitle; it is written to EPUB 3 only.
	Metadata Metadata
	// Cover, when set, becomes the cover image.
	Cover *Cover
	// RemoveCover takes away what marks an image as the cover. The image
	// stays: a page of the book may show it.
	RemoveCover bool
	// Modified is written as dcterms:modified, which EPUB 3 requires.
	Modified time.Time
}

const (
	mimetypeName = "mimetype"
	mimetype     = "application/epub+zip"
	nsDC         = "http://purl.org/dc/elements/1.1/"
	nsOPF        = "http://www.idpf.org/2007/opf"
	// ownPrefix starts the ids and names of what Rewrite adds, so that the
	// next rewrite knows them for its own.
	ownPrefix = "gotome-"
)

// Rewrite copies the EPUB from r to w with its package document describing
// the book as u says. Every other entry is copied as it is stored, so the
// content, and with it Book.ContentHash, stays the same. In the package
// document only the metadata, and for a new cover the manifest, change;
// identifiers other than the package's own unique identifier are replaced,
// and the unique one stays, as obfuscated fonts are keyed by it.
func Rewrite(r io.ReaderAt, size int64, w io.Writer, u Update) error {
	if strings.TrimSpace(u.Metadata.Title) == "" {
		return errors.New("a book needs a title")
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	if len(zr.File) > DefaultLimits.MaxEntries {
		return fmt.Errorf("%w: %d entries", ErrTooLarge, len(zr.File))
	}
	a := newArchive(zr, DefaultLimits)
	opfPath, err := a.packagePath()
	if err != nil {
		return err
	}
	if a.encrypted() {
		return fmt.Errorf("%w: its content is encrypted", ErrNotWritable)
	}
	opfEntry := a.find(opfPath)
	data, err := a.read(opfPath)
	if err != nil {
		return fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}

	var coverName string
	if u.Cover != nil {
		sum := sha256.Sum256(u.Cover.Data)
		coverName = ownPrefix + "cover-" + hex.EncodeToString(sum[:6]) + coverExtension(u.Cover.MediaType)
	}
	rewritten, err := rewritePackage(data, u, coverName)
	if err != nil {
		return err
	}
	dropped := map[string]bool{}
	for _, href := range rewritten.droppedHrefs {
		if p, err := resolve(opfPath, href); err == nil {
			dropped[p] = true
		}
	}

	zw := zip.NewWriter(w)
	// The container format wants the media type first, uncompressed and
	// without extra fields, which a modification time would add.
	mt, err := zw.CreateHeader(&zip.FileHeader{Name: mimetypeName, Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(mt, mimetype); err != nil {
		return err
	}
	for _, f := range zr.File {
		switch {
		case f.Name == mimetypeName:
		case f == opfEntry:
			out, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: u.Modified})
			if err != nil {
				return err
			}
			if _, err := out.Write(rewritten.opf); err != nil {
				return err
			}
		case dropped[f.Name]:
		default:
			// As stored, compressed or not: nothing is read in full.
			if err := zw.Copy(f); err != nil {
				return err
			}
		}
	}
	if u.Cover != nil {
		out, err := zw.CreateHeader(&zip.FileHeader{
			Name: path.Join(path.Dir(opfPath), coverName), Method: zip.Store, Modified: u.Modified,
		})
		if err != nil {
			return err
		}
		if _, err := out.Write(u.Cover.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func coverExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}

type rewrittenPackage struct {
	opf []byte
	// droppedHrefs are entries of the manifest that were taken out, relative
	// to the package document.
	droppedHrefs []string
}

// span is a stretch of the package document, in bytes.
type span struct{ start, end int64 }

// edit replaces a span of the package document.
type edit struct {
	span
	text string
}

// part is one child of the metadata element: an element, or a comment or
// other markup that is kept as it is.
type part struct {
	span
	element bool
	space   string
	local   string
	attrs   map[string]string
	drop    bool
}

// item is one item of the manifest.
type item struct {
	span
	// tagEnd is where its start tag ends.
	tagEnd      int64
	raw         xml.StartElement
	selfClosing bool
	attrs       map[string]string
}

var xmlDeclEncoding = regexp.MustCompile(`^(<\?xml[^>]*?encoding\s*=\s*["'])([^"']*)(["'])`)

// rewritePackage changes the package document's metadata and, for the
// cover, its manifest. What it keeps it copies byte for byte.
func rewritePackage(data []byte, u Update, coverName string) (rewrittenPackage, error) {
	data, err := toUTF8(data)
	if err != nil {
		return rewrittenPackage{}, err
	}
	notWritable := func(why string, args ...any) (rewrittenPackage, error) {
		return rewrittenPackage{}, fmt.Errorf("%w: package document: %s", ErrNotWritable, fmt.Sprintf(why, args...))
	}

	// RawToken below keeps the prefixes as written but does not match end
	// tags to start tags; a document that is not well formed is left alone.
	check := xml.NewDecoder(bytes.NewReader(data))
	check.Entity = xml.HTMLEntity
	for {
		if _, err := check.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return notWritable("%v", err)
		}
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Entity = xml.HTMLEntity
	var (
		depth                   int
		bindings                = map[string]string{}
		version, uniqueID       string
		inMetadata, inManifest  bool
		metadataOpen, metaClose int64 = -1, -1
		manifestClose           int64 = -1
		parts                   []part
		items                   []item
		taken                   = map[string]bool{}
		current                 *part
		currentItem             *item
	)
	for {
		start := d.InputOffset()
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return notWritable("%v", err)
		}
		end := d.InputOffset()
		selfClosing := bytes.HasSuffix(data[start:end], []byte("/>"))
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			attrs := map[string]string{}
			for _, at := range t.Attr {
				attrs[at.Name.Local] = at.Value
				if at.Name.Local == "id" {
					taken[at.Value] = true
				}
			}
			switch {
			case depth == 1:
				addBindings(bindings, t)
				version, uniqueID = attrs["version"], attrs["unique-identifier"]
			case depth == 2 && t.Name.Local == "metadata":
				addBindings(bindings, t)
				if selfClosing {
					return notWritable("its metadata is empty")
				}
				inMetadata, metadataOpen = true, end
			case depth == 2 && t.Name.Local == "manifest":
				inManifest = true
			case depth == 3 && inMetadata:
				own := map[string]string{}
				addBindings(own, t)
				space := own[t.Name.Space]
				if space == "" {
					space = bindings[t.Name.Space]
				}
				parts = append(parts, part{span: span{start: start}, element: true, space: space, local: t.Name.Local, attrs: attrs})
				current = &parts[len(parts)-1]
			case depth == 3 && inManifest && t.Name.Local == "item":
				items = append(items, item{span: span{start, end}, tagEnd: end, raw: t.Copy(), selfClosing: selfClosing, attrs: attrs})
				currentItem = &items[len(items)-1]
			}
		case xml.EndElement:
			switch {
			case depth == 3 && current != nil:
				current.end = end
				current = nil
			case depth == 3 && currentItem != nil:
				if !currentItem.selfClosing {
					currentItem.end = end
				}
				currentItem = nil
			case depth == 2 && inMetadata:
				inMetadata, metaClose = false, start
			case depth == 2 && inManifest:
				inManifest, manifestClose = false, start
			}
			depth--
		default:
			// Comments and the like between the metadata's children stay
			// where they are; the whitespace between them is written anew.
			if depth == 2 && inMetadata {
				if text, ok := tok.(xml.CharData); ok && len(bytes.TrimSpace(text)) == 0 {
					continue
				}
				parts = append(parts, part{span: span{start, end}})
			}
		}
	}
	if metadataOpen < 0 || metaClose < 0 || manifestClose < 0 {
		return notWritable("it has no metadata or no manifest")
	}
	epub3 := strings.HasPrefix(strings.TrimSpace(version), "3")
	coverChanges := u.Cover != nil || u.RemoveCover

	droppedIDs := map[string]bool{}
	for i := range parts {
		p := &parts[i]
		if !p.element {
			continue
		}
		if p.space == nsDC {
			switch p.local {
			case "title", "creator", "contributor", "publisher", "date", "description", "subject":
				p.drop = true
			case "language":
				p.drop = u.Metadata.Language != ""
			case "identifier":
				p.drop = p.attrs["id"] != uniqueID || uniqueID == ""
			}
		} else if p.local == "meta" {
			switch {
			case p.attrs["property"] == "belongs-to-collection", p.attrs["property"] == "dcterms:modified":
				p.drop = true
			case p.attrs["name"] == "calibre:series", p.attrs["name"] == "calibre:series_index":
				p.drop = true
			case p.attrs["name"] == "cover" && coverChanges:
				p.drop = true
			}
		}
		if p.drop && p.attrs["id"] != "" {
			droppedIDs[p.attrs["id"]] = true
		}
	}
	// What refines something dropped goes with it, and so on down.
	for changed := true; changed; {
		changed = false
		for i := range parts {
			p := &parts[i]
			if p.element && !p.drop && droppedIDs[strings.TrimPrefix(p.attrs["refines"], "#")] {
				p.drop, changed = true, true
				if id := p.attrs["id"]; id != "" {
					droppedIDs[id] = true
				}
			}
		}
	}
	// What is dropped frees its id for what is written instead, so ids do not
	// grow a number with every rewrite.
	for id := range droppedIDs {
		delete(taken, id)
	}
	if coverChanges {
		for _, it := range items {
			if id := it.attrs["id"]; strings.HasPrefix(id, ownPrefix+"cover") {
				delete(taken, id)
			}
		}
	}
	var uniqueValue string
	for _, p := range parts {
		if p.element && !p.drop && p.space == nsDC && p.local == "identifier" {
			uniqueValue = identifierKey(string(data[p.start:p.end]))
		}
	}

	g := newGenerator(bindings, taken, epub3)
	for _, p := range parts {
		if !p.drop {
			g.raw(string(data[p.start:p.end]))
		}
	}
	g.metadata(u, uniqueValue)

	var edits []edit
	var result rewrittenPackage
	if coverChanges {
		for _, it := range items {
			id := it.attrs["id"]
			props := strings.Fields(it.attrs["properties"])
			switch {
			case strings.HasPrefix(id, ownPrefix+"cover"):
				edits = append(edits, edit{span: it.span})
				result.droppedHrefs = append(result.droppedHrefs, it.attrs["href"])
			case slices.Contains(props, "cover-image"):
				edits = append(edits, edit{span: span{it.start, it.tagEnd}, text: startTag(it.raw, it.selfClosing, slices.DeleteFunc(props, func(p string) bool { return p == "cover-image" }))})
			}
		}
	}
	if u.Cover != nil {
		id := g.id("cover")
		props := ""
		if epub3 {
			props = ` properties="cover-image"`
		}
		edits = append(edits, edit{span: span{manifestClose, manifestClose}, text: fmt.Sprintf(`  <%s id="%s" href="%s" media-type="%s"%s/>`+"\n  ",
			g.opfName("item"), id, escape(coverName), escape(u.Cover.MediaType), props)})
		g.meta(attr("name", "cover") + attr("content", id))
	}
	if epub3 {
		g.metaText(attr("property", "dcterms:modified"), u.Modified.UTC().Format("2006-01-02T15:04:05Z"))
	}
	edits = append(edits, edit{span: span{metadataOpen, metaClose}, text: g.String() + "\n  "})

	slices.SortFunc(edits, func(a, b edit) int { return int(a.start - b.start) })
	var out bytes.Buffer
	var at int64
	for _, e := range edits {
		out.Write(data[at:e.start])
		out.WriteString(e.text)
		at = e.end
	}
	out.Write(data[at:])
	result.opf = out.Bytes()
	return result, nil
}

// toUTF8 brings a package document saved in Latin-1 into UTF-8, which is all
// Rewrite writes, and says so in its declaration.
func toUTF8(data []byte) ([]byte, error) {
	m := xmlDeclEncoding.FindSubmatchIndex(data)
	if m == nil {
		return data, nil
	}
	switch strings.ToLower(string(data[m[4]:m[5]])) {
	case "utf-8", "utf8":
		return data, nil
	case "iso-8859-1", "latin1", "windows-1252", "cp1252":
	default:
		return nil, fmt.Errorf("%w: package document in %s", ErrNotWritable, data[m[4]:m[5]])
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	converted := []byte(string(runes))
	m = xmlDeclEncoding.FindSubmatchIndex(converted)
	return slices.Concat(converted[:m[4]], []byte("UTF-8"), converted[m[5]:]), nil
}

func addBindings(into map[string]string, t xml.StartElement) {
	for _, at := range t.Attr {
		switch {
		case at.Name.Space == "xmlns":
			into[at.Name.Local] = at.Value
		case at.Name.Space == "" && at.Name.Local == "xmlns":
			into[""] = at.Value
		}
	}
}

// identifierKey is an identifier element's value as it is compared with
// another: what is left of it without a scheme, hyphens or spaces.
func identifierKey(element string) string {
	var v struct {
		Scheme string `xml:"scheme,attr"`
		Value  string `xml:",chardata"`
	}
	if err := decodeXML([]byte(element), &v); err != nil {
		return ""
	}
	id, _ := identifier(v.Scheme, v.Value)
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(id.Value))
}

// startTag writes a start tag again with other properties.
func startTag(t xml.StartElement, selfClosing bool, properties []string) string {
	var b strings.Builder
	b.WriteString("<" + qualified(t.Name))
	for _, at := range t.Attr {
		if at.Name.Space == "" && at.Name.Local == "properties" {
			if len(properties) == 0 {
				continue
			}
			at.Value = strings.Join(properties, " ")
		}
		b.WriteString(" " + qualified(at.Name) + `="` + escape(at.Value) + `"`)
	}
	if selfClosing {
		b.WriteString("/>")
	} else {
		b.WriteString(">")
	}
	return b.String()
}

func qualified(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func attr(name, value string) string { return " " + name + `="` + escape(value) + `"` }

// generator writes metadata elements with the prefixes the document has
// bound, declaring the namespace on the element where it has none.
type generator struct {
	strings.Builder
	epub3 bool
	taken map[string]bool
	// dc is the prefix of Dublin Core and dcDecl its declaration, if the
	// document makes none.
	dc, dcDecl string
	// opf is the prefix of OPF attributes in EPUB 2, and opfDecl likewise.
	opf, opfDecl string
	// opfElem is the prefix OPF elements are written with; empty when OPF
	// is the default namespace. opfElemDecl declares it otherwise.
	opfElem, opfElemDecl string
}

func newGenerator(bindings map[string]string, taken map[string]bool, epub3 bool) *generator {
	g := &generator{epub3: epub3, taken: taken}
	find := func(uri string) (string, bool) {
		for _, prefix := range slices.Sorted(maps.Keys(bindings)) {
			if prefix != "" && bindings[prefix] == uri {
				return prefix, true
			}
		}
		return "", false
	}
	var ok bool
	if g.dc, ok = find(nsDC); !ok {
		g.dc, g.dcDecl = "dc", attr("xmlns:dc", nsDC)
	}
	if g.opf, ok = find(nsOPF); !ok {
		g.opf, g.opfDecl = "opf", attr("xmlns:opf", nsOPF)
	}
	if bindings[""] != nsOPF {
		if p, ok := find(nsOPF); ok {
			g.opfElem = p
		} else {
			g.opfElemDecl = attr("xmlns", nsOPF)
		}
	}
	return g
}

// id returns an id no element of the document has.
func (g *generator) id(base string) string {
	id := ownPrefix + base
	for n := 2; g.taken[id]; n++ {
		id = ownPrefix + base + "-" + strconv.Itoa(n)
	}
	g.taken[id] = true
	return id
}

func (g *generator) raw(s string) { g.WriteString("\n    " + s) }

func (g *generator) opfName(local string) string {
	if g.opfElem != "" {
		return g.opfElem + ":" + local
	}
	return local
}

func (g *generator) dcElement(local, attrs, text string) {
	g.raw("<" + g.dc + ":" + local + g.dcDecl + attrs + ">" + escape(text) + "</" + g.dc + ":" + local + ">")
}

func (g *generator) meta(attrs string) {
	g.raw("<" + g.opfName("meta") + g.opfElemDecl + attrs + "/>")
}

func (g *generator) metaText(attrs, text string) {
	g.raw("<" + g.opfName("meta") + g.opfElemDecl + attrs + ">" + escape(text) + "</" + g.opfName("meta") + ">")
}

// refined writes an element with an id and, in EPUB 3, meta elements that
// refine it; EPUB 2 gets them as OPF attributes where it has such.
func (g *generator) refined(local, text, base string, refinements [][2]string, epub2Attrs string) {
	if !g.epub3 {
		decl := ""
		if epub2Attrs != "" {
			decl = g.opfDecl
		}
		g.dcElement(local, decl+epub2Attrs, text)
		return
	}
	if len(refinements) == 0 {
		g.dcElement(local, "", text)
		return
	}
	id := g.id(base)
	g.dcElement(local, attr("id", id), text)
	for _, r := range refinements {
		extra := ""
		if r[0] == "role" {
			extra = attr("scheme", "marc:relators")
		}
		g.metaText(attr("refines", "#"+id)+attr("property", r[0])+extra, r[1])
	}
}

func (g *generator) metadata(u Update, uniqueValue string) {
	m := u.Metadata
	if m.Subtitle != "" && g.epub3 {
		g.refined("title", collapse(m.Title), "title", [][2]string{{"title-type", "main"}}, "")
		g.refined("title", collapse(m.Subtitle), "subtitle", [][2]string{{"title-type", "subtitle"}}, "")
	} else {
		g.dcElement("title", "", collapse(m.Title))
	}
	for _, c := range m.Contributors {
		role := c.Role
		if role == "" {
			role = "aut"
		}
		local := "contributor"
		if role == "aut" {
			local = "creator"
		}
		refinements := [][2]string{{"role", role}}
		epub2 := attr(g.opf+":role", role)
		if c.FileAs != "" {
			refinements = append(refinements, [2]string{"file-as", c.FileAs})
			epub2 += attr(g.opf+":file-as", c.FileAs)
		}
		g.refined(local, c.Name, "creator", refinements, epub2)
	}
	if m.Language != "" {
		g.dcElement("language", "", m.Language)
	}
	for _, ident := range m.Identifiers {
		if ident.Value == "" || identifierKeyOf(ident) == uniqueValue {
			continue
		}
		if g.epub3 {
			value := ident.Value
			if ident.Scheme != "" {
				value = "urn:" + ident.Scheme + ":" + ident.Value
			}
			g.dcElement("identifier", "", value)
		} else {
			scheme := ""
			if ident.Scheme != "" {
				scheme = g.opfDecl + attr(g.opf+":scheme", strings.ToUpper(ident.Scheme))
			}
			g.dcElement("identifier", scheme, ident.Value)
		}
	}
	if m.Publisher != "" {
		g.dcElement("publisher", "", m.Publisher)
	}
	if m.Published != "" {
		g.dcElement("date", "", m.Published)
	}
	if m.Description != "" {
		g.dcElement("description", "", m.Description)
	}
	for _, s := range m.Subjects {
		g.dcElement("subject", "", s)
	}
	if m.Series != "" {
		index := ""
		if m.SeriesIndex != nil {
			index = strconv.FormatFloat(*m.SeriesIndex, 'f', -1, 64)
		}
		if g.epub3 {
			id := g.id("series")
			g.metaText(attr("property", "belongs-to-collection")+attr("id", id), m.Series)
			g.metaText(attr("refines", "#"+id)+attr("property", "collection-type"), "series")
			if index != "" {
				g.metaText(attr("refines", "#"+id)+attr("property", "group-position"), index)
			}
		}
		// Many readers know a series only from calibre's meta elements.
		g.meta(attr("name", "calibre:series") + attr("content", m.Series))
		if index != "" {
			g.meta(attr("name", "calibre:series_index") + attr("content", index))
		}
	}
}

func identifierKeyOf(id Identifier) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(id.Value))
}
