package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const (
	containerPath  = "META-INF/container.xml"
	encryptionPath = "META-INF/encryption.xml"
)

// archive looks entries up by name and reads them under the limits.
type archive struct {
	limits Limits
	files  map[string]*zip.File
	// folded finds an entry whose name differs in case from the reference to
	// it, which files made on case-insensitive systems get wrong.
	folded map[string]*zip.File
}

func newArchive(zr *zip.Reader, limits Limits) *archive {
	a := &archive{limits: limits, files: map[string]*zip.File{}, folded: map[string]*zip.File{}}
	for _, f := range zr.File {
		if _, dup := a.files[f.Name]; !dup {
			a.files[f.Name] = f
		}
		if lower := strings.ToLower(f.Name); a.folded[lower] == nil {
			a.folded[lower] = f
		}
	}
	return a
}

func (a *archive) find(name string) *zip.File {
	if f := a.files[name]; f != nil {
		return f
	}
	return a.folded[strings.ToLower(name)]
}

// read returns an entry's content. The size a zip entry declares is whatever
// its author wrote there, so the limit is enforced on the bytes that come out.
func (a *archive) read(name string) ([]byte, error) {
	f := a.find(name)
	if f == nil {
		return nil, fmt.Errorf("no entry %q", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("entry %q: %w", name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, a.limits.MaxEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("entry %q: %w", name, err)
	}
	if int64(len(data)) > a.limits.MaxEntryBytes {
		return nil, fmt.Errorf("%w: entry %q is larger than %d bytes", ErrTooLarge, name, a.limits.MaxEntryBytes)
	}
	return data, nil
}

// packagePath is where the container says the package document is.
func (a *archive) packagePath() (string, error) {
	data, err := a.read(containerPath)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return "", err
		}
		return "", fmt.Errorf("%w: no %s", ErrNotEPUB, containerPath)
	}
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := decodeXML(data, &container); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrNotEPUB, containerPath, err)
	}
	for _, rf := range container.Rootfiles {
		if p, err := resolve("", rf.FullPath); err == nil && a.find(p) != nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %s names no package document", ErrNotEPUB, containerPath)
}

// Font obfuscation is listed in encryption.xml too, but it only scrambles
// embedded fonts and is not rights management.
var fontObfuscation = map[string]bool{
	"http://www.idpf.org/2008/embedding": true,
	"http://ns.adobe.com/pdf/enc#RC":     true,
}

// encrypted reports whether any content is encrypted with something other
// than font obfuscation. A file that cannot be read is taken as encrypted:
// extracting garbage would be worse than extracting nothing.
func (a *archive) encrypted() bool {
	if a.find(encryptionPath) == nil {
		return false
	}
	data, err := a.read(encryptionPath)
	if err != nil {
		return true
	}
	var enc struct {
		Data []struct {
			Method struct {
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"EncryptionMethod"`
		} `xml:"EncryptedData"`
	}
	if err := decodeXML(data, &enc); err != nil {
		return true
	}
	for _, d := range enc.Data {
		if !fontObfuscation[d.Method.Algorithm] {
			return true
		}
	}
	return false
}

// cover finds the cover image: the EPUB 3 manifest property, then the EPUB 2
// meta element, then an image that is called a cover.
func (a *archive) cover(opfPath string, pkg *opfPackage) *Cover {
	var item *opfItem
	for i := range pkg.Manifest {
		if pkg.Manifest[i].hasProperty("cover-image") {
			item = &pkg.Manifest[i]
			break
		}
	}
	if item == nil {
		for _, m := range pkg.Metadata.Metas {
			if m.Name == "cover" {
				item = pkg.item(m.Content)
				break
			}
		}
	}
	if item == nil || !item.isImage() {
		item = nil
		for i := range pkg.Manifest {
			it := &pkg.Manifest[i]
			if it.isImage() && strings.Contains(strings.ToLower(it.ID+" "+it.Href), "cover") {
				item = it
				break
			}
		}
	}
	if item == nil {
		return nil
	}
	href, err := resolve(opfPath, item.Href)
	if err != nil {
		return nil
	}
	data, err := a.read(href)
	if err != nil || len(data) == 0 {
		return nil
	}
	return &Cover{MediaType: item.MediaType, Data: data}
}

// contents maps each document to its title in the table of contents: the
// EPUB 3 navigation document where there is one, the EPUB 2 NCX otherwise.
func (a *archive) contents(opfPath string, pkg *opfPackage) map[string]string {
	titles := map[string]string{}
	add := func(base, src, label string) {
		label = collapse(label)
		href, err := resolve(base, src)
		if err != nil || label == "" {
			return
		}
		// The first entry that points into a document names it; later ones
		// are sections inside it.
		if _, seen := titles[href]; !seen {
			titles[href] = label
		}
	}

	for i := range pkg.Manifest {
		item := &pkg.Manifest[i]
		if !item.hasProperty("nav") {
			continue
		}
		navPath, err := resolve(opfPath, item.Href)
		if err != nil {
			continue
		}
		data, err := a.read(navPath)
		if err != nil {
			continue
		}
		for _, link := range navLinks(data) {
			add(navPath, link.href, link.text)
		}
	}
	if len(titles) > 0 {
		return titles
	}

	ncx := pkg.item(pkg.Spine.Toc)
	if ncx == nil {
		for i := range pkg.Manifest {
			if pkg.Manifest[i].MediaType == "application/x-dtbncx+xml" {
				ncx = &pkg.Manifest[i]
				break
			}
		}
	}
	if ncx == nil {
		return titles
	}
	ncxPath, err := resolve(opfPath, ncx.Href)
	if err != nil {
		return titles
	}
	data, err := a.read(ncxPath)
	if err != nil {
		return titles
	}
	var doc struct {
		Points []navPoint `xml:"navMap>navPoint"`
	}
	if err := decodeXML(data, &doc); err != nil {
		return titles
	}
	var walk func(points []navPoint, depth int)
	walk = func(points []navPoint, depth int) {
		// Nesting this deep is not a table of contents.
		if depth > 32 {
			return
		}
		for _, p := range points {
			add(ncxPath, p.Content.Src, p.Label)
			walk(p.Points, depth+1)
		}
	}
	walk(doc.Points, 0)
	return titles
}

type navPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Points []navPoint `xml:"navPoint"`
}

// decodeXML reads XML that may declare one of the single-byte encodings old
// files were saved in. Anything else is read as UTF-8.
func decodeXML(data []byte, v any) error {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "iso-8859-1", "latin1", "windows-1252", "cp1252":
			raw, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			runes := make([]rune, len(raw))
			for i, b := range raw {
				runes[i] = rune(b)
			}
			return strings.NewReader(string(runes)), nil
		}
		return input, nil
	}
	return dec.Decode(v)
}

func pathUnescape(s string) (string, error) { return url.PathUnescape(s) }
