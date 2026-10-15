package epub

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"path"
	"regexp"
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
func (fx *fixer) removeFonts() error {
	l, err := fx.layout()
	if err != nil {
		return err
	}
	fonts := map[string]bool{}
	ids := map[string]bool{}
	var sheets []string
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
	if len(fonts) == 0 {
		return nil
	}
	for _, name := range sheets {
		data, err := fx.content(name)
		if err != nil || !bytes.Contains(bytes.ToLower(data), []byte("@font-face")) {
			continue
		}
		fx.set(name, fontFace.ReplaceAll(data, nil))
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

// shrinkImage returns the image scaled down to MaxImageSide, or for one
// that fits but is large, recompressed; false when neither helps or the
// image does not decode.
func shrinkImage(data []byte, isJPEG bool) ([]byte, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxDecodePixels {
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
	if err != nil || (!tooLarge && out.Len() >= len(data)) {
		return nil, false
	}
	return out.Bytes(), true
}
