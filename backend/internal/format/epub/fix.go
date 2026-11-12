package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// FixVersion is the version of what Fix makes of an EPUB. Copies made for
// devices are kept by it: raise it with any change to Fix, to the limits
// and to what the findings mean, and every copy is made again.
const FixVersion = 1

// Fixed says what Fix changed.
type Fixed struct {
	// DocumentsSplit are the content documents cut into parts, PartsAdded
	// the parts that came of them besides the first.
	DocumentsSplit int `json:"documentsSplit"`
	PartsAdded     int `json:"partsAdded"`
	// ImagesScaled are the images scaled down or recompressed.
	ImagesScaled int `json:"imagesScaled"`
	// FontsSubset are the embedded fonts cut down to the characters the
	// book uses, FontsRemoved those taken out.
	FontsSubset  int `json:"fontsSubset"`
	FontsRemoved int `json:"fontsRemoved"`
	// ItemsRemoved are the manifest items without a file taken out.
	ItemsRemoved int `json:"itemsRemoved"`
	// DocumentsRepaired are the content documents written again as
	// well-formed XHTML.
	DocumentsRepaired int `json:"documentsRepaired"`
	// TocEntriesRemoved are the entries of the tables of contents taken
	// out as they led nowhere, TocEntriesRepointed those pointed at the
	// start of their document or at an entry below them instead.
	TocEntriesRemoved   int `json:"tocEntriesRemoved"`
	TocEntriesRepointed int `json:"tocEntriesRepointed"`
}

// Any reports whether anything was changed.
func (f Fixed) Any() bool {
	return f.DocumentsSplit+f.ImagesScaled+f.FontsSubset+f.FontsRemoved+f.ItemsRemoved+
		f.DocumentsRepaired+f.TocEntriesRemoved+f.TocEntriesRepointed > 0
}

// fixer holds an EPUB while fixes change it: the package document as text,
// and the entries that are replaced, removed or added. Every other entry is
// copied as stored.
type fixer struct {
	zr      *zip.Reader
	a       *archive
	opfPath string
	opf     []byte
	data    map[string][]byte
	removed map[string]bool
	// added are new entries, in the order they are written.
	added []string
	fixed Fixed
}

// Fix copies the EPUB from r to w with what makes it slow or troublesome
// on an e-reader fixed, each finding of epub.Findings by its own step:
// LargeDocument splits content documents, LargeImage scales images down,
// LargeFonts removes embedded fonts and SubsetFonts, another fix of the same
// finding, cuts them down to the characters the book uses (and removes only
// those that cannot be cut or stay over the limit), MissingResources takes manifest items
// whose file is missing out of the manifest and spine, MalformedDocument
// writes documents that are not well formed again as XHTML, and BrokenTOC
// takes entries of the tables of contents out that lead nowhere. A step
// changes a document only where it can check the result. What a step does
// not touch is copied as stored. A step that finds nothing to do changes
// nothing, and Fixed says so; w is then written all the same.
func Fix(r io.ReaderAt, size int64, w io.Writer, findings []string, modified time.Time) (Fixed, error) {
	for _, f := range findings {
		if !slices.Contains(Fixes, f) {
			return Fixed{}, fmt.Errorf("no fix for %q", f)
		}
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return Fixed{}, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	if len(zr.File) > DefaultLimits.MaxEntries {
		return Fixed{}, fmt.Errorf("%w: %d entries", ErrTooLarge, len(zr.File))
	}
	a := newArchive(zr, DefaultLimits)
	opfPath, err := a.packagePath()
	if err != nil {
		return Fixed{}, err
	}
	if a.encrypted() {
		return Fixed{}, fmt.Errorf("%w: its content is encrypted", ErrNotWritable)
	}
	opf, err := a.read(opfPath)
	if err != nil {
		return Fixed{}, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	if opf, err = toUTF8(opf); err != nil {
		return Fixed{}, err
	}
	fx := &fixer{zr: zr, a: a, opfPath: a.find(opfPath).Name, opf: opf, data: map[string][]byte{}, removed: map[string]bool{}}
	// Missing files first, so that no other step trips over them; documents
	// before the tables of contents, which are edited only where they are
	// well formed, and before splitting, which splits only those; fonts
	// before splitting, so that the parts carry no @font-face either.
	steps := []struct {
		finding string
		run     func() error
	}{
		{MissingResources, fx.repair},
		{MalformedDocument, fx.repairDocuments},
		{BrokenTOC, fx.repairTOC},
		{LargeFonts, fx.removeFonts},
		{SubsetFonts, fx.subsetFonts},
		{LargeImage, fx.scaleImages},
		{LargeDocument, fx.splitDocuments},
	}
	for _, s := range steps {
		if s.finding == LargeFonts && slices.Contains(findings, SubsetFonts) {
			continue
		}
		if slices.Contains(findings, s.finding) {
			if err := s.run(); err != nil {
				return Fixed{}, err
			}
		}
	}
	return fx.fixed, fx.write(w, modified)
}

// content is an entry's bytes as the fixes left them.
func (fx *fixer) content(name string) ([]byte, error) {
	if d, ok := fx.data[name]; ok {
		return d, nil
	}
	return fx.a.read(name)
}

func (fx *fixer) set(name string, data []byte) {
	if _, known := fx.data[name]; !known && fx.a.files[name] == nil {
		fx.added = append(fx.added, name)
	}
	fx.data[name] = data
}

func (fx *fixer) remove(name string) {
	fx.removed[name] = true
	delete(fx.data, name)
}

// write puts the EPUB together: the media type first, then the entries in
// their order, each as the fixes left it, then the new ones.
func (fx *fixer) write(w io.Writer, modified time.Time) error {
	zw := zip.NewWriter(w)
	mt, err := zw.CreateHeader(&zip.FileHeader{Name: mimetypeName, Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(mt, mimetype); err != nil {
		return err
	}
	create := func(name string, data []byte) error {
		out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	}
	written := map[string]bool{mimetypeName: true}
	for _, f := range fx.zr.File {
		switch {
		case written[f.Name], fx.removed[f.Name], strings.HasSuffix(f.Name, "/"):
		case f.Name == fx.opfPath:
			if err := create(f.Name, fx.opf); err != nil {
				return err
			}
		default:
			if data, ok := fx.data[f.Name]; ok {
				if err := create(f.Name, data); err != nil {
					return err
				}
			} else if err := zw.Copy(f); err != nil {
				return err
			}
		}
		written[f.Name] = true
	}
	for _, name := range fx.added {
		if !written[name] && !fx.removed[name] {
			if err := create(name, fx.data[name]); err != nil {
				return err
			}
			written[name] = true
		}
	}
	return zw.Close()
}

// manifestEntry is an item of the manifest with where it lies in the
// package document and the archive entry it names, if there is one.
type manifestEntry struct {
	span
	tagEnd    int64
	attrs     map[string]string
	entry     string
	mediaType string
}

// spineRef is an itemref of the spine.
type spineRef struct {
	span
	idref string
}

// packageLayout is where the manifest's items and the spine's itemrefs lie
// in the package document, to be edited in place.
type packageLayout struct {
	items []manifestEntry
	refs  []spineRef
	// manifestClose is where the manifest's end tag starts.
	manifestClose int64
	// spineTag is the spine's start tag, and spineAttrs its attributes.
	spineTag   span
	spineAttrs map[string]string
	ids        map[string]bool
	// itemPrefix is the prefix the manifest's items are written with, with
	// its colon; empty when OPF is the default namespace.
	itemPrefix string
}

func (fx *fixer) layout() (*packageLayout, error) {
	notWritable := func(err error) (*packageLayout, error) {
		return nil, fmt.Errorf("%w: package document: %v", ErrNotWritable, err)
	}
	check := xml.NewDecoder(bytes.NewReader(fx.opf))
	check.Entity = xml.HTMLEntity
	for {
		if _, err := check.Token(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return notWritable(err)
		}
	}
	l := &packageLayout{manifestClose: -1, ids: map[string]bool{}}
	d := xml.NewDecoder(bytes.NewReader(fx.opf))
	d.Entity = xml.HTMLEntity
	depth := 0
	var inManifest, inSpine bool
	var open *manifestEntry
	var openRef *spineRef
	for {
		start := d.InputOffset()
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return notWritable(err)
		}
		end := d.InputOffset()
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			attrs := map[string]string{}
			for _, at := range t.Attr {
				attrs[at.Name.Local] = at.Value
				if at.Name.Local == "id" {
					l.ids[at.Value] = true
				}
			}
			selfClosing := bytes.HasSuffix(fx.opf[start:end], []byte("/>"))
			switch {
			case depth == 2 && t.Name.Local == "manifest":
				inManifest = !selfClosing
			case depth == 2 && t.Name.Local == "spine":
				inSpine = !selfClosing
				l.spineTag, l.spineAttrs = span{start, end}, attrs
			case depth == 3 && inManifest && t.Name.Local == "item":
				if t.Name.Space != "" {
					l.itemPrefix = t.Name.Space + ":"
				}
				it := manifestEntry{span: span{start, end}, tagEnd: end, attrs: attrs, mediaType: strings.ToLower(attrs["media-type"])}
				if !strings.Contains(attrs["href"], "://") {
					if name, err := resolve(fx.opfPath, attrs["href"]); err == nil {
						it.entry = name
						if f := fx.a.find(name); f != nil {
							it.entry = f.Name
						}
					}
				}
				l.items = append(l.items, it)
				if !selfClosing {
					open = &l.items[len(l.items)-1]
				}
			case depth == 3 && inSpine && t.Name.Local == "itemref":
				l.refs = append(l.refs, spineRef{span: span{start, end}, idref: attrs["idref"]})
				if !selfClosing {
					openRef = &l.refs[len(l.refs)-1]
				}
			}
			if selfClosing {
				// RawToken reports the end of an empty element as its own
				// token, at the same offset.
				if _, err := d.RawToken(); err != nil {
					return notWritable(err)
				}
				depth--
			}
		case xml.EndElement:
			switch {
			case depth == 3 && open != nil:
				open.end = end
				open = nil
			case depth == 3 && openRef != nil:
				openRef.end = end
				openRef = nil
			case depth == 2 && inManifest:
				inManifest, l.manifestClose = false, start
			case depth == 2 && inSpine:
				inSpine = false
			}
			depth--
		}
	}
	if l.manifestClose < 0 {
		return notWritable(errors.New("it has no manifest"))
	}
	return l, nil
}

// exists reports whether the archive holds an entry the fixes kept.
func (fx *fixer) exists(name string) bool {
	if fx.removed[name] {
		return false
	}
	if _, ok := fx.data[name]; ok {
		return true
	}
	f := fx.a.find(name)
	return f != nil && !fx.removed[f.Name]
}

// editPackage applies edits to the package document.
func (fx *fixer) editPackage(edits []edit) {
	fx.opf = applyEdits(fx.opf, edits)
}

// applyEdits replaces the spans of data the edits name; an edit that
// starts inside one before it is passed over.
func applyEdits(data []byte, edits []edit) []byte {
	slices.SortStableFunc(edits, func(a, b edit) int { return int(a.start - b.start) })
	var out bytes.Buffer
	var at int64
	for _, e := range edits {
		if e.start < at {
			continue
		}
		out.Write(data[at:e.start])
		out.WriteString(e.text)
		at = e.end
	}
	out.Write(data[at:])
	return out.Bytes()
}

func (fx *fixer) lineBefore(s span) span { return lineBefore(fx.opf, s) }

// lineBefore extends a span to the whitespace before it on its line, so that
// taking an element out leaves no empty line.
func lineBefore(data []byte, s span) span {
	start := s.start
	for start > 0 && (data[start-1] == ' ' || data[start-1] == '\t') {
		start--
	}
	if start > 0 && data[start-1] == '\n' {
		start--
	}
	return span{start, s.end}
}

var spineTocAttr = regexp.MustCompile(`\s+toc\s*=\s*("[^"]*"|'[^']*')`)

// dropItems takes the manifest items out whose ids are given, with the
// spine's references to them.
func (fx *fixer) dropItems(l *packageLayout, ids map[string]bool) {
	var edits []edit
	for _, it := range l.items {
		if ids[it.attrs["id"]] {
			edits = append(edits, edit{span: fx.lineBefore(it.span)})
		}
	}
	for _, ref := range l.refs {
		if ids[ref.idref] {
			edits = append(edits, edit{span: fx.lineBefore(ref.span)})
		}
	}
	if ids[l.spineAttrs["toc"]] {
		tag := string(fx.opf[l.spineTag.start:l.spineTag.end])
		edits = append(edits, edit{span: l.spineTag, text: spineTocAttr.ReplaceAllString(tag, "")})
	}
	fx.editPackage(edits)
}

// relativeHref is the reference from the document at from to the entry
// at to, both archive paths.
func relativeHref(from, to string) string {
	fromDir := strings.Split(path.Dir(from), "/")
	toParts := strings.Split(to, "/")
	if path.Dir(from) == "." {
		fromDir = nil
	}
	common := 0
	for common < len(fromDir) && common < len(toParts)-1 && fromDir[common] == toParts[common] {
		common++
	}
	var b strings.Builder
	for range fromDir[common:] {
		b.WriteString("../")
	}
	b.WriteString(strings.Join(toParts[common:], "/"))
	return b.String()
}
