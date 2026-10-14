package epub

import (
	"archive/zip"
	"fmt"
	"image"
	_ "image/gif" // registered for image.DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path"
	"slices"
	"strings"

	_ "golang.org/x/image/webp"
)

// Measures are what makes an EPUB slow on an e-reader. Reading engines such
// as Adobe's on the Tolino lay out one content document at a time with every
// font it uses, and decode each image at full size, so these are the
// largest of each, not totals of the book.
type Measures struct {
	// LargestDocument is the uncompressed size of the largest content
	// document, and LargestDocumentName its path in the archive.
	LargestDocument     int64  `json:"largestDocument"`
	LargestDocumentName string `json:"largestDocumentName,omitempty"`
	// Fonts is how many embedded fonts there are, FontBytes their size
	// together and LargestFont the largest's.
	Fonts       int   `json:"fonts"`
	FontBytes   int64 `json:"fontBytes"`
	LargestFont int64 `json:"largestFont"`
	// LargestImage is the size of the largest raster image, and
	// LargestImageName its path.
	LargestImage     int64  `json:"largestImage"`
	LargestImageName string `json:"largestImageName,omitempty"`
	// ImageWidth and ImageHeight are those of the image of the most
	// pixels, read from its header; ImagePixelsName is its path.
	ImageWidth      int    `json:"imageWidth"`
	ImageHeight     int    `json:"imageHeight"`
	ImagePixelsName string `json:"imagePixelsName,omitempty"`
	// MissingResources are manifest items whose file the archive does not
	// hold.
	MissingResources []string `json:"missingResources,omitempty"`
}

// The findings a file can have, in the order they are listed.
const (
	// LargeDocument is a content document past MaxDocumentBytes.
	LargeDocument = "large-document"
	// LargeFonts is embedded fonts past MaxFontBytes together.
	LargeFonts = "large-fonts"
	// LargeImage is an image past MaxImagePixels or MaxImageBytes.
	LargeImage = "large-image"
	// MissingResources is a manifest that lists files the archive does not
	// hold, which some readers refuse to open.
	MissingResources = "missing-resources"
)

// Findings are every finding, in order.
var Findings = []string{LargeDocument, LargeFonts, LargeImage, MissingResources}

// The limits past which a file is slow on an e-reader. They are set from
// a sample of a real library (#233) and are meant to be tuned with books
// a reader is known to be slow on.
const (
	MaxDocumentBytes = 300 << 10
	MaxFontBytes     = 1 << 20
	MaxImagePixels   = 4_000_000
	MaxImageBytes    = 1 << 20
)

// Findings names what about the file makes it slow or troublesome on an
// e-reader, in the order of Findings; none for a file that is fine.
func (m Measures) Findings() []string {
	out := []string{}
	if m.LargestDocument > MaxDocumentBytes {
		out = append(out, LargeDocument)
	}
	if m.FontBytes > MaxFontBytes {
		out = append(out, LargeFonts)
	}
	if int64(m.ImageWidth)*int64(m.ImageHeight) > MaxImagePixels || m.LargestImage > MaxImageBytes {
		out = append(out, LargeImage)
	}
	if len(m.MissingResources) > 0 {
		out = append(out, MissingResources)
	}
	return out
}

// imageHeaderBytes is as much of an image as is read for its size. A
// JPEG's dimensions follow whatever comes before them, such as a large
// EXIF block; one that puts them further in is not measured.
const imageHeaderBytes = 256 << 10

// fontTypes are the media types embedded fonts are declared with.
var fontTypes = []string{
	"application/vnd.ms-opentype", "application/x-font-ttf", "application/x-font-otf",
	"application/x-font-truetype", "application/x-font-opentype", "application/font-sfnt",
	"application/font-woff", "application/x-font-woff",
}

var fontExtensions = []string{".ttf", ".otf", ".woff", ".woff2"}

// isFont takes a manifest item, or with an empty media type an archive
// entry, for a font by its media type or its extension.
func isFont(mediaType, name string) bool {
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "font/") || slices.Contains(fontTypes, mediaType) {
		return true
	}
	return slices.Contains(fontExtensions, strings.ToLower(path.Ext(name)))
}

// Measure reads what makes an EPUB slow on an e-reader. It reads the
// archive's directory, the package document and the start of each image,
// nothing else; sizes are those the archive declares.
func Measure(r io.ReaderAt, size int64) (Measures, error) {
	var m Measures
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return m, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	if len(zr.File) > DefaultLimits.MaxEntries {
		return m, fmt.Errorf("%w: %d entries", ErrTooLarge, len(zr.File))
	}
	a := newArchive(zr, DefaultLimits)
	opfPath, err := a.packagePath()
	if err != nil {
		return m, err
	}
	data, err := a.read(opfPath)
	if err != nil {
		return m, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	pkg, err := parsePackage(data)
	if err != nil {
		return m, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}

	fonts := map[*zip.File]bool{}
	for i := range pkg.Manifest {
		it := &pkg.Manifest[i]
		if strings.Contains(it.Href, "://") {
			continue
		}
		name, err := resolve(opfPath, it.Href)
		if err != nil {
			continue
		}
		f := a.find(name)
		if f == nil {
			m.MissingResources = append(m.MissingResources, name)
			continue
		}
		size := int64(f.UncompressedSize64)
		switch {
		case it.isDocument():
			if size > m.LargestDocument {
				m.LargestDocument, m.LargestDocumentName = size, f.Name
			}
		case isFont(it.MediaType, f.Name):
			fonts[f] = true
		case it.isImage() && it.MediaType != "image/svg+xml":
			if size > m.LargestImage {
				m.LargestImage, m.LargestImageName = size, f.Name
			}
			if w, h, ok := imageSize(f); ok && w*h > m.ImageWidth*m.ImageHeight {
				m.ImageWidth, m.ImageHeight, m.ImagePixelsName = w, h, f.Name
			}
		}
	}
	// Fonts the manifest does not list are still in the file a reader is
	// given, and some readers load them.
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/") && isFont("", f.Name) {
			fonts[f] = true
		}
	}
	for f := range fonts {
		size := int64(f.UncompressedSize64)
		m.Fonts++
		m.FontBytes += size
		m.LargestFont = max(m.LargestFont, size)
	}
	return m, nil
}

// imageSize reads an image's dimensions from its header.
func imageSize(f *zip.File) (int, int, bool) {
	rc, err := f.Open()
	if err != nil {
		return 0, 0, false
	}
	defer rc.Close()
	cfg, _, err := image.DecodeConfig(io.LimitReader(rc, imageHeaderBytes))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
