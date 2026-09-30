// Package epub reads EPUB 2 and EPUB 3 files: metadata, cover and the clean
// text of every chapter. It writes nothing and trusts nothing: an EPUB is a zip
// archive from anywhere, so every entry is read under a size limit and every
// path is kept inside the archive.
package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

var (
	// ErrNotEPUB is returned for a file that is not a zip archive or has no
	// package document to read.
	ErrNotEPUB = errors.New("not an EPUB")
	// ErrTooLarge is returned when a file exceeds a limit. It is how a zip
	// bomb ends.
	ErrTooLarge = errors.New("EPUB exceeds a size limit")
)

// Limits bound what reading one file may cost.
type Limits struct {
	// MaxEntries is the number of zip entries accepted.
	MaxEntries int
	// MaxEntryBytes is the uncompressed size of one document or image.
	MaxEntryBytes int64
	// MaxTextBytes is the extracted text of the whole book.
	MaxTextBytes int64
}

// DefaultLimits are far above any real book: the text of the longest novels is
// a few megabytes.
var DefaultLimits = Limits{
	MaxEntries:    20000,
	MaxEntryBytes: 32 << 20,
	MaxTextBytes:  128 << 20,
}

// Book is what one EPUB file holds.
type Book struct {
	Metadata Metadata
	// Chapters are the documents of the spine in reading order. Empty when
	// the content is encrypted.
	Chapters []Chapter
	// Cover is nil when the file names none.
	Cover *Cover
	// DRM reports that the content is encrypted. Metadata and cover are
	// still read; the text cannot be.
	DRM bool
}

// Chapter is one document of the spine.
type Chapter struct {
	// Href is the document's path inside the archive.
	Href string
	// Title comes from the table of contents, or else from the document's
	// first heading. It may be empty.
	Title string
	// Text is the document's text without markup. Paragraphs are separated by
	// a blank line.
	Text string
	// Offset is where Text starts in Book.Text, counted in characters.
	Offset int
}

// Cover is the cover image as stored in the file.
type Cover struct {
	MediaType string
	Data      []byte
}

// chapterSeparator stands between chapters in Book.Text.
const chapterSeparator = "\n\n"

// Text is the whole book: every chapter's text in reading order.
func (b *Book) Text() string {
	var sb strings.Builder
	for i, ch := range b.Chapters {
		if i > 0 {
			sb.WriteString(chapterSeparator)
		}
		sb.WriteString(ch.Text)
	}
	return sb.String()
}

// Parse reads an EPUB under DefaultLimits.
func Parse(r io.ReaderAt, size int64) (*Book, error) {
	return ParseWithLimits(r, size, DefaultLimits)
}

// ParseWithLimits reads an EPUB.
func ParseWithLimits(r io.ReaderAt, size int64, limits Limits) (*Book, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	if len(zr.File) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: %d entries", ErrTooLarge, len(zr.File))
	}
	a := newArchive(zr, limits)

	opfPath, err := a.packagePath()
	if err != nil {
		return nil, err
	}
	data, err := a.read(opfPath)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	pkg, err := parsePackage(data)
	if err != nil {
		return nil, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}

	book := &Book{Metadata: pkg.metadata()}
	book.DRM = a.encrypted()
	book.Cover = a.cover(opfPath, pkg)
	if book.DRM {
		return book, nil
	}

	titles := a.contents(opfPath, pkg)
	var total int64
	offset := 0
	for _, item := range pkg.spineItems() {
		if !item.isDocument() || item.hasProperty("nav") {
			continue
		}
		href, err := resolve(opfPath, item.Href)
		if err != nil {
			continue
		}
		// A spine entry that points nowhere, or at something unreadable, costs
		// the book that chapter and nothing more.
		doc, err := a.read(href)
		if err != nil {
			if errors.Is(err, ErrTooLarge) {
				return nil, err
			}
			continue
		}
		text, heading := extractText(doc)
		total += int64(len(text))
		if total > limits.MaxTextBytes {
			return nil, fmt.Errorf("%w: more than %d bytes of text", ErrTooLarge, limits.MaxTextBytes)
		}
		title := titles[href]
		if title == "" {
			title = heading
		}
		if len(book.Chapters) > 0 {
			offset += utf8.RuneCountInString(chapterSeparator)
		}
		book.Chapters = append(book.Chapters, Chapter{Href: href, Title: title, Text: text, Offset: offset})
		offset += utf8.RuneCountInString(text)
	}
	return book, nil
}

// resolve turns a reference inside the document at base into an archive path.
// A reference that leaves the archive is refused.
func resolve(base, href string) (string, error) {
	href, _, _ = strings.Cut(href, "#")
	if unescaped, err := pathUnescape(href); err == nil {
		href = unescaped
	}
	if href == "" {
		return "", errors.New("empty reference")
	}
	if strings.Contains(href, "://") || strings.HasPrefix(href, "/") {
		return "", fmt.Errorf("reference %q is not relative", href)
	}
	joined := path.Join(path.Dir(base), href)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", fmt.Errorf("reference %q leaves the archive", href)
	}
	return joined, nil
}
