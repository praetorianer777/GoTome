package epub

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/praetorianer777/gotome/backend/internal/format/fontsubset"
)

var (
	fontFamily = regexp.MustCompile(`(?is)font-family\s*:\s*([^;}]+)`)
	cssURL     = regexp.MustCompile(`(?is)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`)
	cssImport  = regexp.MustCompile(`(?is)@import\s+(?:url\(\s*)?["']?([^"')\s;]+)`)
	algorithm  = regexp.MustCompile(`Algorithm\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

const (
	idpfAlgorithm  = "http://www.idpf.org/2008/embedding"
	adobeAlgorithm = "http://ns.adobe.com/pdf/enc#RC"
	maxImportDepth = 3
)

// fontFaceRule is an @font-face rule: the family it declares and the
// archive entries its src names.
type fontFaceRule struct {
	family string
	urls   []string
}

// parseFontFaces reads the @font-face rules of a style sheet that lies at
// base (an archive path), resolving their urls against it.
func parseFontFaces(css, base string) []fontFaceRule {
	var out []fontFaceRule
	for _, rule := range fontFace.FindAllString(css, -1) {
		face := fontFaceRule{}
		if m := fontFamily.FindStringSubmatch(rule); m != nil {
			face.family = strings.ToLower(strings.Trim(strings.TrimSpace(m[1]), `"'`))
		}
		for _, m := range cssURL.FindAllStringSubmatch(rule, -1) {
			ref, _, _ := strings.Cut(m[1]+m[2]+m[3], "?")
			if name, err := resolve(base, ref); err == nil {
				face.urls = append(face.urls, name)
			}
		}
		out = append(out, face)
	}
	return out
}

// documentUse is what a content document says about the fonts it uses: its
// text, the style sheets it links and the styles it holds.
type documentUse struct {
	runes  map[rune]bool
	sheets []string
	inline []string
}

func readDocumentUse(doc []byte, name string) documentUse {
	use := documentUse{runes: map[rune]bool{}}
	z := html.NewTokenizer(bytes.NewReader(bytes.ToValidUTF8(doc, replacement)))
	var skip string
	for {
		switch z.Next() {
		case html.ErrorToken:
			return use
		case html.StartTagToken, html.SelfClosingTagToken:
			tag, hasAttr := z.TagName()
			switch string(tag) {
			case "style", "script":
				skip = string(tag)
			case "link":
				var rel, href string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "rel":
						rel = strings.ToLower(string(v))
					case "href":
						href = string(v)
					}
				}
				if slices.Contains(strings.Fields(rel), "stylesheet") {
					if sheet, err := resolve(name, href); err == nil {
						use.sheets = append(use.sheets, sheet)
					}
				}
			}
		case html.EndTagToken:
			skip = ""
		case html.TextToken:
			switch skip {
			case "style":
				use.inline = append(use.inline, string(z.Text()))
			case "":
				for _, r := range string(z.Text()) {
					use.runes[r] = true
				}
			}
		}
	}
}

// safetyRunes are kept whatever the book says: what a reader's own
// typography, a footnote or a later edit may bring in.
func safetyRunes() []rune {
	var out []rune
	for r := rune(0x20); r < 0x7F; r++ {
		out = append(out, r)
	}
	out = append(out, ' ', '­', '¡', '¿', '§', '¶', '°', '·',
		'«', '»', '×', '÷', '€', '™', '©', '®', '−',
		'•', '…', '′', '″', '‹', '›', '†', '‡', ' ', ' ', '　')
	for r := rune(0x2000); r <= 0x200D; r++ {
		out = append(out, r)
	}
	for r := rune(0x2010); r <= 0x201F; r++ {
		out = append(out, r)
	}
	return out
}

// withCases adds to every letter its other cases.
func withCases(set map[rune]bool) {
	for r := range set {
		if unicode.IsLetter(r) {
			set[unicode.ToUpper(r)] = true
			set[unicode.ToLower(r)] = true
			set[unicode.ToTitle(r)] = true
		}
	}
}

// fontUsage says which characters each font (an archive path in fonts) is
// used for: those of the documents whose style sheets, linked or inline,
// declare it. A font that no document's sheets declare could be used by
// any, so it gets all their text.
func (fx *fixer) fontUsage(l *packageLayout, fonts map[string]bool) map[string]map[rune]bool {
	usage := map[string]map[rune]bool{}
	everything := map[rune]bool{}
	for _, it := range l.items {
		if it.entry == "" || !fx.exists(it.entry) || !(&opfItem{MediaType: it.mediaType, Href: it.entry}).isDocument() {
			continue
		}
		data, err := fx.content(it.entry)
		if err != nil {
			continue
		}
		use := readDocumentUse(data, it.entry)
		for r := range use.runes {
			everything[r] = true
		}
		declared := map[string]bool{}
		for _, css := range use.inline {
			fx.declaredFonts(css, it.entry, 0, declared)
		}
		for _, sheet := range use.sheets {
			if css, err := fx.content(sheet); err == nil && fx.exists(sheet) {
				fx.declaredFonts(string(css), sheet, 0, declared)
			}
		}
		for font := range declared {
			if fonts[font] {
				if usage[font] == nil {
					usage[font] = map[rune]bool{}
				}
				for r := range use.runes {
					usage[font][r] = true
				}
			}
		}
	}
	for font := range fonts {
		if usage[font] == nil {
			usage[font] = maps.Clone(everything)
		}
		for _, r := range safetyRunes() {
			usage[font][r] = true
		}
		withCases(usage[font])
	}
	return usage
}

// declaredFonts adds the fonts the @font-face rules of a style sheet name,
// and of the sheets it imports.
func (fx *fixer) declaredFonts(css, base string, depth int, into map[string]bool) {
	for _, face := range parseFontFaces(css, base) {
		for _, u := range face.urls {
			into[u] = true
		}
	}
	if depth >= maxImportDepth {
		return
	}
	for _, m := range cssImport.FindAllStringSubmatch(css, -1) {
		if name, err := resolve(base, m[1]); err == nil && fx.exists(name) {
			if imported, err := fx.content(name); err == nil {
				fx.declaredFonts(string(imported), name, depth+1, into)
			}
		}
	}
}

// obfuscations says which fonts encryption.xml lists as obfuscated, by the
// algorithm.
func (fx *fixer) obfuscations() map[string]string {
	out := map[string]string{}
	enc := fx.a.find(encryptionPath)
	if enc == nil {
		return out
	}
	data, err := fx.a.read(enc.Name)
	if err != nil {
		return out
	}
	for _, entry := range encryptedData.FindAll(data, -1) {
		uri, alg := cipherURI.FindSubmatch(entry), algorithm.FindSubmatch(entry)
		if uri == nil || alg == nil {
			continue
		}
		if name, err := resolve("", string(uri[1])+string(uri[2])); err == nil {
			out[name] = string(alg[1]) + string(alg[2])
		}
	}
	return out
}

// obfuscationKey is what a font is scrambled with, from the package's
// unique identifier.
func (fx *fixer) obfuscationKey(algorithm string) (key []byte, length int) {
	pkg, err := parsePackage(fx.opf)
	if err != nil || pkg.UniqueID == "" {
		return nil, 0
	}
	var id string
	for _, i := range pkg.Metadata.Identifiers {
		if i.ID == pkg.UniqueID {
			id = i.Value
		}
	}
	switch algorithm {
	case idpfAlgorithm:
		id = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				return -1
			}
			return r
		}, id)
		if id == "" {
			return nil, 0
		}
		sum := sha1.Sum([]byte(id))
		return sum[:], 1040
	case adobeAlgorithm:
		id = strings.NewReplacer("urn:uuid:", "", "-", "", " ", "", "\n", "", "\t", "", "\r", "").Replace(strings.ToLower(id))
		raw, err := hex.DecodeString(id)
		if err != nil || len(raw) != 16 {
			return nil, 0
		}
		return raw, 1024
	}
	return nil, 0
}

// xorPrefix scrambles or unscrambles the first length bytes of a font; the
// same step does both.
func xorPrefix(font, key []byte, length int) []byte {
	out := bytes.Clone(font)
	for i := 0; i < min(length, len(out)); i++ {
		out[i] ^= key[i%len(key)]
	}
	return out
}

// subsetFonts cuts each embedded TrueType font (plain, WOFF or obfuscated)
// down to the characters of the documents that declare it, keeping its name
// and manifest item. A font it cannot cut stays as it is; if the fonts are
// still past MaxFontBytes together, those removeFonts takes out are the ones
// that stayed, and then all of them.
func (fx *fixer) subsetFonts() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	fonts, _, _ := fx.collectFonts(l)
	if len(fonts) == 0 {
		return nil
	}
	usage := fx.fontUsage(l, fonts)
	obfuscated := fx.obfuscations()
	cut := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(fonts)) {
		data, err := fx.content(name)
		if err != nil {
			continue
		}
		plain := data
		var key []byte
		var length int
		if alg, ok := obfuscated[name]; ok {
			if key, length = fx.obfuscationKey(alg); key == nil {
				continue
			}
			plain = xorPrefix(data, key, length)
		}
		if !fontsubset.IsFont(plain) {
			continue
		}
		runes := make([]rune, 0, len(usage[name]))
		for r := range usage[name] {
			runes = append(runes, r)
		}
		slices.Sort(runes)
		sub, err := fontsubset.Subset(plain, runes)
		if err != nil || len(sub) >= len(plain) || fontsubset.Verify(plain, sub, runes) != nil {
			continue
		}
		if key != nil {
			sub = xorPrefix(sub, key, length)
		}
		fx.set(name, sub)
		fx.fixed.FontsSubset++
		cut[name] = true
	}
	total := func() int {
		n := 0
		for name := range fonts {
			if data, err := fx.content(name); err == nil && fx.exists(name) {
				n += len(data)
			}
		}
		return n
	}
	if total() > MaxFontBytes {
		if err := fx.removeFontsWhere(func(name string) bool { return !cut[name] }); err != nil {
			return err
		}
	}
	if total() > MaxFontBytes {
		return fx.removeFontsWhere(nil)
	}
	return nil
}
