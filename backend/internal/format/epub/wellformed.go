package epub

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// errCharset is a document in an encoding the checks cannot read, of which
// they cannot tell whether it is well formed.
var errCharset = errors.New("an encoding that is not read")

var (
	utf8BOM = []byte("\xef\xbb\xbf")
	// xhtmlDTD is a document type of XHTML 1.0 or 1.1 by its public or its
	// system identifier, a DTD that declares HTML's named entities.
	xhtmlDTD = regexp.MustCompile(`-//W3C//DTD XHTML|/DTD/xhtml[^"']*\.dtd`)
	// entityDecl is a general entity the internal subset declares.
	entityDecl = regexp.MustCompile(`<!ENTITY\s+([^\s%"']+)\s+(?:"([^"]*)"|'([^']*)')\s*>`)
)

// newDocumentDecoder reads a document in UTF-8 or in one of the
// single-byte encodings old files declare; others are errCharset.
func newDocumentDecoder(data []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
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
		return nil, errCharset
	}
	return d
}

// declareEntities adds the entities a document type declaration declares:
// HTML's under an XHTML DTD, and those of its internal subset.
func declareEntities(into map[string]string, directive []byte) {
	if !bytes.HasPrefix(directive, []byte("DOCTYPE")) {
		return
	}
	if xhtmlDTD.Match(directive) {
		maps.Copy(into, xml.HTMLEntity)
	}
	for _, m := range entityDecl.FindAllSubmatch(directive, -1) {
		into[string(m[1])] = string(m[2]) + string(m[3])
	}
}

// checkWellFormed reports why a content document is not well-formed XML:
// one root element, every element closed in order, attributes quoted,
// and no entity but XML's own and those its document type declares. A
// document whose encoding is not read is errCharset.
func checkWellFormed(data []byte) error {
	if bytes.HasPrefix(data, []byte("\xfe\xff")) || bytes.HasPrefix(data, []byte("\xff\xfe")) {
		return errCharset
	}
	d := newDocumentDecoder(data)
	entities := map[string]string{}
	// The decoder looks entities up as it meets them, so the declarations
	// of the document type, which comes first, are in the map by then.
	d.Entity = entities
	depth, roots := 0, 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errCharset) {
				return errCharset
			}
			return err
		}
		switch t := tok.(type) {
		case xml.Directive:
			declareEntities(entities, t)
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) > 0 {
				return errors.New("text outside the root element")
			}
		}
	}
	if roots != 1 {
		return errors.New("not one root element")
	}
	return nil
}

// malformed reports whether a content document is known not to be well
// formed.
func malformed(data []byte) bool {
	err := checkWellFormed(data)
	return err != nil && !errors.Is(err, errCharset)
}

// textAndIDs is what a document says, as a reader shows it: its text
// without white space or what scripts and style sheets hold, and its ids
// in order. A repair must leave both.
type textAndIDs struct {
	text string
	ids  []string
}

func (a textAndIDs) equal(b textAndIDs) bool {
	return a.text == b.text && slices.Equal(a.ids, b.ids)
}

func appendWords(into *strings.Builder, text string) {
	for _, w := range strings.Fields(text) {
		into.WriteString(w)
	}
}

// unshown are the elements whose content is not text a reader shows.
var unshown = map[string]bool{"script": true, "style": true}

// tokenTextAndIDs reads a document's text and ids from its tokens, in the
// order they are written, as an HTML tokenizer reads them; false for a
// document with a CDATA section among its text, which HTML reads as a
// comment.
func tokenTextAndIDs(data []byte) (textAndIDs, bool) {
	var out textAndIDs
	var text strings.Builder
	var skip string
	z := html.NewTokenizer(bytes.NewReader(data))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			out.text = text.String()
			return out, errors.Is(z.Err(), io.EOF)
		case html.TextToken:
			if skip == "" {
				appendWords(&text, string(z.Text()))
			}
		case html.CommentToken:
			if skip == "" && bytes.HasPrefix(z.Raw(), []byte("<![CDATA[")) {
				return out, false
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == skip {
				skip = ""
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			for hasAttr {
				var key, val []byte
				key, val, hasAttr = z.TagAttr()
				if k := string(key); k == "id" || k == "name" && tag == "a" {
					out.ids = append(out.ids, string(val))
				}
			}
			if tt == html.StartTagToken && unshown[tag] {
				skip = tag
			}
		}
	}
}

// xmlTextAndIDs reads a well-formed document's text and ids.
func xmlTextAndIDs(data []byte) (textAndIDs, error) {
	var out textAndIDs
	var text strings.Builder
	d := newDocumentDecoder(data)
	skip := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			out.text = text.String()
			return out, nil
		}
		if err != nil {
			return out, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			for _, at := range t.Attr {
				if at.Name.Space == "" && (at.Name.Local == "id" || at.Name.Local == "name" && t.Name.Local == "a") {
					out.ids = append(out.ids, at.Value)
				}
			}
			if skip > 0 || unshown[t.Name.Local] {
				skip++
			}
		case xml.EndElement:
			if skip > 0 {
				skip--
			}
		case xml.CharData:
			if skip == 0 {
				appendWords(&text, string(t))
			}
		}
	}
}
