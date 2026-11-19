package epub

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/image/draw"
)

// repair takes manifest items whose file the archive lacks out of the
// manifest and the spine. Nothing else is touched: a reference to such a
// file from a document already led nowhere.
func (fx *fixer) repair() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	gone := map[string]bool{}
	for _, it := range l.items {
		if it.entry != "" && !fx.exists(it.entry) {
			gone[it.attrs["id"]] = true
		}
	}
	if len(gone) == 0 {
		return nil
	}
	fx.dropItems(l, gone)
	fx.fixed.ItemsRemoved += len(gone)
	return nil
}

var (
	fontFace = regexp.MustCompile(`(?is)@font-face\s*\{[^}]*\}\s*`)
	// encryptedData is one entry of encryption.xml, and cipherURI the file
	// it names.
	encryptedData = regexp.MustCompile(`(?s)\s*<(?:[\w.-]+:)?EncryptedData\b.*?</(?:[\w.-]+:)?EncryptedData\s*>`)
	cipherURI     = regexp.MustCompile(`URI\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

// removeFonts takes the embedded fonts out: their entries, their manifest
// items, the @font-face rules of the style sheets and documents, and their
// entries in encryption.xml. Text falls back to the reader's own fonts.
func (fx *fixer) removeFonts() error { return fx.removeFontsWhere(nil) }

// removeFontsWhere takes out the fonts for which only is true, every font
// when it is nil. With a choice, only the @font-face rules that name those
// fonts go.
func (fx *fixer) removeFontsWhere(only func(entry string) bool) error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	fonts, ids, sheets := fx.collectFonts(l)
	if only != nil {
		for name := range fonts {
			if !only(name) {
				delete(fonts, name)
			}
		}
		for _, it := range l.items {
			if !fonts[it.entry] {
				delete(ids, it.attrs["id"])
			}
		}
	}
	if len(fonts) == 0 {
		return nil
	}
	for _, name := range sheets {
		data, err := fx.content(name)
		if err != nil || !bytes.Contains(bytes.ToLower(data), []byte("@font-face")) {
			continue
		}
		if only == nil {
			fx.set(name, fontFace.ReplaceAll(data, nil))
			continue
		}
		fx.set(name, fontFace.ReplaceAllFunc(data, func(rule []byte) []byte {
			for _, face := range parseFontFaces(string(rule), name) {
				if slices.ContainsFunc(face.urls, func(u string) bool { return fonts[u] }) {
					return nil
				}
			}
			return rule
		}))
	}
	if enc := fx.a.find(encryptionPath); enc != nil {
		data, err := fx.a.read(enc.Name)
		if err != nil {
			return err
		}
		kept := encryptedData.ReplaceAllFunc(data, func(entry []byte) []byte {
			m := cipherURI.FindSubmatch(entry)
			if m == nil {
				return entry
			}
			uri := string(m[1]) + string(m[2])
			if name, err := resolve("", uri); err == nil && fonts[name] {
				return nil
			}
			return entry
		})
		if encryptedData.Match(kept) {
			fx.set(enc.Name, kept)
		} else {
			fx.remove(enc.Name)
		}
	}
	for name := range fonts {
		fx.remove(name)
	}
	if len(ids) > 0 {
		fx.dropItems(l, ids)
	}
	fx.fixed.FontsRemoved += len(fonts)
	return nil
}

// collectFonts finds the embedded fonts the fixes kept, listed in the
// manifest or not, the manifest ids of those that are, and the style
// sheets and documents that may declare them.
func (fx *fixer) collectFonts(l *packageLayout) (fonts, ids map[string]bool, sheets []string) {
	fonts, ids = map[string]bool{}, map[string]bool{}
	for _, it := range l.items {
		switch {
		case it.entry == "" || !fx.exists(it.entry):
		case isFont(it.mediaType, it.entry):
			fonts[it.entry] = true
			ids[it.attrs["id"]] = true
		case it.mediaType == "text/css" || strings.HasSuffix(strings.ToLower(it.entry), ".css") ||
			(&opfItem{MediaType: it.mediaType, Href: it.entry}).isDocument():
			sheets = append(sheets, it.entry)
		}
	}
	for _, f := range fx.zr.File {
		if !strings.HasSuffix(f.Name, "/") && isFont("", f.Name) && !fx.removed[f.Name] {
			fonts[f.Name] = true
		}
	}
	return fonts, ids, sheets
}

const (
	// MaxImageSide is the longest side an image is scaled down to: more
	// than an e-reader's screen shows.
	MaxImageSide = 1600
	// maxDecodePixels bounds what an image may unpack to; one larger is
	// left as it is.
	maxDecodePixels = 64_000_000
	jpegFixQuality  = 85
)

// scaleImages scales JPEG and PNG images larger than MaxImageSide down to
// it, and recompresses those over MaxImageBytes where that makes them
// smaller. Each keeps its name and format; GIF, SVG and WebP stay.
func (fx *fixer) scaleImages() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	for _, it := range l.items {
		if it.entry == "" || !fx.exists(it.entry) {
			continue
		}
		ext := strings.ToLower(path.Ext(it.entry))
		isJPEG := it.mediaType == "image/jpeg" || ext == ".jpg" || ext == ".jpeg"
		isPNG := it.mediaType == "image/png" || ext == ".png"
		if !isJPEG && !isPNG {
			continue
		}
		data, err := fx.content(it.entry)
		if err != nil {
			continue
		}
		smaller, ok := shrinkImage(data, isJPEG)
		if ok {
			fx.set(it.entry, smaller)
			fx.fixed.ImagesScaled++
		}
	}
	return nil
}

// jpegOrientation is the EXIF orientation of a JPEG, 1 where it names none.
// Re-encoding drops it, so an image that is turned for display is left as
// it is, whatever its size.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for at := 2; at+4 <= len(data) && data[at] == 0xFF; {
		marker := data[at+1]
		if marker == 0xD9 || marker == 0xDA {
			break
		}
		size := int(data[at+2])<<8 | int(data[at+3])
		if size < 2 || at+2+size > len(data) {
			break
		}
		seg := data[at+4 : at+2+size]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		at += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		u32 = func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	default:
		return 1
	}
	ifd := u32(t[4:])
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	n := u16(t[ifd:])
	for i := 0; i < n && ifd+2+12*(i+1) <= len(t); i++ {
		e := t[ifd+2+12*i:]
		if u16(e) == 0x0112 {
			return u16(e[8:])
		}
	}
	return 1
}

// shrinkImage returns the image scaled down to MaxImageSide, or for one
// that fits but is large, recompressed; false when neither helps or the
// image does not decode. A result that is not smaller is dropped, unless
// the image is past MaxImagePixels, where fewer pixels is the point.
func shrinkImage(data []byte, isJPEG bool) ([]byte, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxDecodePixels {
		return nil, false
	}
	if isJPEG && jpegOrientation(data) > 1 {
		return nil, false
	}
	tooLarge := max(cfg.Width, cfg.Height) > MaxImageSide
	if !tooLarge && len(data) <= MaxImageBytes {
		return nil, false
	}
	var src image.Image
	if isJPEG {
		src, err = jpeg.Decode(bytes.NewReader(data))
	} else {
		src, err = png.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, false
	}
	img := src
	if tooLarge {
		w, h := cfg.Width, cfg.Height
		if w >= h {
			w, h = MaxImageSide, max(1, h*MaxImageSide/w)
		} else {
			w, h = max(1, w*MaxImageSide/h), MaxImageSide
		}
		rect := image.Rect(0, 0, w, h)
		var dst draw.Image
		switch src.(type) {
		case *image.Gray, *image.Gray16:
			dst = image.NewGray(rect)
		default:
			if isJPEG {
				dst = image.NewRGBA(rect)
			} else {
				dst = image.NewNRGBA(rect)
			}
		}
		draw.CatmullRom.Scale(dst, rect, src, src.Bounds(), draw.Src, nil)
		img = dst
	}
	var out bytes.Buffer
	if isJPEG {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegFixQuality})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
	}
	if err != nil || (out.Len() >= len(data) && cfg.Width*cfg.Height <= MaxImagePixels) {
		return nil, false
	}
	return out.Bytes(), true
}
