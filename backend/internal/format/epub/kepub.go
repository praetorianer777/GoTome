package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// KepubVersion is the version of what Kepubify makes of an EPUB. Copies
// made for Kobo readers are kept by it: raise it with any change to the
// conversion and every copy is made again.
const KepubVersion = 1

// kepubStyle is the one rule kepubify adds: Kobo's reader lays the book out
// inside the two divisions below, and their margins are its own.
const kepubStyle = `<style type="text/css" id="kobostylehacks">div#book-inner { margin-top: 0; margin-bottom: 0; }</style>`

// Kepubified says what Kepubify did.
type Kepubified struct {
	// Documents are the content documents given spans, Spans the spans.
	Documents int `json:"documents"`
	Spans     int `json:"spans"`
	// Skipped are the content documents copied as stored, as they are not
	// well formed or the result could not be shown to keep their text.
	Skipped int `json:"skipped"`
}

// Kepubify copies the EPUB from r to w as a KEPUB, the format Kobo readers
// keep reading statistics and place in: each sentence of the text of a
// content document is wrapped in a span of class koboSpan, numbered by
// paragraph and sentence ("kobo.3.2"), and the body's content in the
// divisions book-columns and book-inner. A document that is not well-formed
// XML, whose markup uses prefixes, or whose text would not be the same
// afterwards is copied as stored, like every other entry; the package
// document is not changed.
func Kepubify(r io.ReaderAt, size int64, w io.Writer, modified time.Time) (Kepubified, error) {
	zr, err := openZip(r, size, DefaultLimits)
	if err != nil {
		return Kepubified{}, err
	}
	a := newArchive(zr, DefaultLimits)
	opfPath, err := a.packagePath()
	if err != nil {
		return Kepubified{}, err
	}
	if a.encrypted() {
		return Kepubified{}, fmt.Errorf("%w: its content is encrypted", ErrNotWritable)
	}
	opf, err := a.read(opfPath)
	if err != nil {
		return Kepubified{}, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	if opf, err = toUTF8(opf); err != nil {
		return Kepubified{}, err
	}
	pkg, err := parsePackage(opf)
	if err != nil {
		return Kepubified{}, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}

	var out Kepubified
	changed := map[string][]byte{}
	for _, it := range pkg.spineItems() {
		if !it.isXHTML() || it.hasProperty("nav") {
			continue
		}
		name, err := resolve(opfPath, it.Href)
		if err != nil || a.find(name) == nil {
			continue
		}
		file := a.find(name)
		data, err := a.read(name)
		if err != nil {
			return Kepubified{}, err
		}
		converted, spans, ok := kepubDocument(data)
		if !ok {
			out.Skipped++
			continue
		}
		changed[file.Name] = converted
		out.Documents++
		out.Spans += spans
	}

	zw := zip.NewWriter(w)
	mt, err := zw.CreateHeader(&zip.FileHeader{Name: mimetypeName, Method: zip.Store})
	if err != nil {
		return out, err
	}
	if _, err := io.WriteString(mt, mimetype); err != nil {
		return out, err
	}
	for _, f := range zr.File {
		switch {
		case f.Name == mimetypeName, strings.HasSuffix(f.Name, "/"):
		case changed[f.Name] != nil:
			o, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: modified})
			if err != nil {
				return out, err
			}
			if _, err := o.Write(changed[f.Name]); err != nil {
				return out, err
			}
		default:
			if err := zw.Copy(f); err != nil {
				return out, err
			}
		}
	}
	return out, zw.Close()
}

// notTextual are the elements whose text is not a reader's to read, and
// those whose content spans must not break.
var notTextual = map[string]bool{
	"head": true, "script": true, "style": true, "title": true, "svg": true, "math": true,
	"pre": true, "textarea": true, "noscript": true, "rt": true, "rp": true,
}

// holdNoText are the elements that hold elements only; the white space
// between them is theirs, and a span there is invalid.
var holdNoText = map[string]bool{
	"html": true, "table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true,
	"colgroup": true, "ul": true, "ol": true, "dl": true, "select": true, "optgroup": true,
}

// paragraphs are the elements around which a new paragraph number starts.
var paragraphs = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "td": true, "th": true, "tr": true, "blockquote": true, "dt": true, "dd": true,
	"figcaption": true, "figure": true, "caption": true, "section": true, "article": true,
	"aside": true, "header": true, "footer": true, "nav": true, "address": true, "hr": true,
	"ul": true, "ol": true, "dl": true, "table": true, "body": true, "br": true, "pre": true,
}

// kepubDocument gives a content document its spans; false for one that is
// to stay as it is.
func kepubDocument(data []byte) ([]byte, int, bool) {
	if bytes.HasPrefix(data, []byte("\xfe\xff")) || bytes.HasPrefix(data, []byte("\xff\xfe")) {
		return nil, 0, false
	}
	if checkWellFormed(data) != nil {
		return nil, 0, false
	}
	before, ok := tokenTextAndIDs(data)
	if !ok {
		return nil, 0, false
	}
	var out bytes.Buffer
	out.Grow(len(data) + len(data)/4)
	z := html.NewTokenizer(bytes.NewReader(data))
	var stack []string
	var inBody, wrapped bool
	para, sent, spans := 0, 0, 0
	newPara := true
	skipDepth := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return nil, 0, false
			}
			break
		}
		raw := z.Raw()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)
			if len(stack) == 0 && name != "html" || strings.Contains(name, ":") {
				return nil, 0, false
			}
			out.Write(raw)
			if tt == html.StartTagToken && !voidElements[name] {
				stack = append(stack, name)
				if skipDepth > 0 || notTextual[name] {
					skipDepth++
				}
			}
			if paragraphs[name] {
				newPara = true
			}
			if name == "body" && tt == html.StartTagToken && !inBody {
				inBody = true
				out.WriteString(`<div id="book-columns"><div id="book-inner">`)
				wrapped = true
			}
		case html.EndTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)
			switch {
			case name == "body" && wrapped:
				out.WriteString(`</div></div>`)
				wrapped = false
			case name == "head":
				out.WriteString(kepubStyle)
			}
			out.Write(raw)
			if len(stack) > 0 && stack[len(stack)-1] == name {
				stack = stack[:len(stack)-1]
				if skipDepth > 0 {
					skipDepth--
				}
			}
			if paragraphs[name] {
				newPara = true
			}
		case html.TextToken:
			if !inBody || skipDepth > 0 || len(stack) == 0 || holdNoText[stack[len(stack)-1]] || len(bytes.TrimSpace(raw)) == 0 {
				out.Write(raw)
				continue
			}
			for _, seg := range sentences(string(raw)) {
				out.WriteString(seg.lead)
				if seg.text != "" {
					if newPara {
						para++
						sent = 0
						newPara = false
					}
					sent++
					spans++
					out.WriteString(`<span class="koboSpan" id="kobo.` + strconv.Itoa(para) + "." + strconv.Itoa(sent) + `">`)
					out.WriteString(seg.text)
					out.WriteString("</span>")
				}
				out.WriteString(seg.trail)
			}
		default:
			out.Write(raw)
		}
	}
	if checkWellFormed(out.Bytes()) != nil {
		return nil, 0, false
	}
	after, ok := tokenTextAndIDs(out.Bytes())
	if !ok || after.text != before.text {
		return nil, 0, false
	}
	after.ids = slices.DeleteFunc(after.ids, func(id string) bool {
		return strings.HasPrefix(id, "kobo.") || id == "book-columns" || id == "book-inner" || id == "kobostylehacks"
	})
	if !slices.Equal(after.ids, before.ids) {
		return nil, 0, false
	}
	return out.Bytes(), spans, true
}

// segment is a sentence of a text with the white space around it.
type segment struct{ lead, text, trail string }

const (
	terminators = ".!?…"
	closers     = "\"'”’»›)]"
	cjkEnds     = "。！？"
)

// closingEntities are the entities that close a quotation after a full
// stop, as a text keeps them escaped.
var closingEntities = []string{"&quot;", "&apos;", "&#39;", "&#34;", "&rdquo;", "&rsquo;", "&raquo;", "&#8221;", "&#8217;", "&#x201d;", "&#x201D;", "&#x2019;"}

// sentences cuts a text at the end of a sentence: a full stop, question or
// exclamation mark, or an ellipsis, with the closing quotes and brackets
// after it, and then white space. Everything is kept, so the pieces
// together are the text. The sentences of a script that has no spaces
// between them end at the mark.
func sentences(s string) []segment {
	var out []segment
	i := 0
	for i < len(s) {
		start := i
		for start < len(s) && isSpace(s[start]) {
			start++
		}
		end := start
		for end < len(s) {
			r, n := utf8.DecodeRuneInString(s[end:])
			end += n
			if !strings.ContainsRune(terminators, r) && !strings.ContainsRune(cjkEnds, r) {
				continue
			}
			cjk := strings.ContainsRune(cjkEnds, r)
			for end < len(s) {
				if rest := s[end:]; strings.HasPrefix(rest, "&") {
					if e := entityAt(rest); e > 0 {
						end += e
						continue
					}
					break
				}
				r2, n2 := utf8.DecodeRuneInString(s[end:])
				if strings.ContainsRune(terminators, r2) || strings.ContainsRune(closers, r2) {
					end += n2
					continue
				}
				break
			}
			if cjk || end == len(s) || isSpace(s[end]) {
				break
			}
		}
		trail := end
		for trail < len(s) && isSpace(s[trail]) {
			trail++
		}
		out = append(out, segment{lead: s[i:start], text: s[start:end], trail: s[end:trail]})
		i = trail
	}
	return out
}

func entityAt(s string) int {
	for _, e := range closingEntities {
		if strings.HasPrefix(s, e) {
			return len(e)
		}
	}
	return 0
}

func isSpace(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }

// KepubName is the name a KEPUB is saved under on a Kobo reader, which
// knows the format by the name.
func KepubName(name string) string {
	base := strings.TrimSuffix(name, path.Ext(name))
	return strings.TrimSuffix(base, ".kepub") + ".kepub.epub"
}
