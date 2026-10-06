package ingest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/procexec"
)

// The programs of poppler that read PDFs. Variables, so that a test can put a
// program in their place that misbehaves on purpose.
var (
	pdfinfo   = "pdfinfo"
	pdftotext = "pdftotext"
	pdftoppm  = "pdftoppm"
)

// pdfLimits bound one run of a poppler program on one file. They are far above
// what a real book needs: the text of a long one is a few megabytes, and its
// first page renders in well under a second.
var pdfLimits = procexec.Limits{
	Timeout:   2 * time.Minute,
	MaxOutput: 64 << 20,
	MaxMemory: 1 << 30,
	MaxCPU:    2 * time.Minute,
}

// A PDF has a text layer when its pages carry this many letters on average.
// A scanned book without one still yields a few: a stamp, a copyright line.
const minLettersPerPage = 20

// coverWidth is the width the first page is rendered at, which is the width
// of the largest cover size kept.
const coverWidth = "1000"

// junkTitle matches the titles PDF writers fill in on their own: the name of
// the document they were given, or nothing much.
var junkTitle = regexp.MustCompile(`(?i)^(?:untitled|document\d*|\s*|.*\.(?:docx?|odt|rtf|pdf|indd|tex|dvi|ps|qxd|pages))$`)

func extractPDF(ctx context.Context, path string) (Extracted, error) {
	info, err := runPoppler(ctx, pdfinfo, "-isodates", "-enc", "UTF-8", path)
	if err != nil {
		var exit *procexec.ExitError
		if errors.As(err, &exit) && strings.Contains(exit.Stderr, "Incorrect password") {
			// Locked with a password: catalogued, not read.
			return Extracted{DRM: true}, nil
		}
		return Extracted{}, err
	}
	fields := pdfInfoFields(info)
	out := Extracted{Metadata: pdfMetadata(fields)}
	if pages, err := strconv.Atoi(fields["Pages"]); err == nil && pages > 0 {
		n := int32(pages)
		out.Pages, out.Metadata.PageCount = &n, &n
	}

	text, err := runPoppler(ctx, pdftotext, "-enc", "UTF-8", "-eol", "unix", path, "-")
	if err != nil {
		return Extracted{}, err
	}
	letters := 0
	// pdftotext ends every page with a form feed.
	pages := strings.Split(string(text), "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}
	for i, page := range pages {
		for _, r := range page {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		out.Sections = append(out.Sections, Section{Label: strconv.Itoa(i + 1), Text: strings.TrimSpace(page)})
	}
	out.HasText = len(pages) > 0 && letters >= minLettersPerPage*len(pages)
	if id, ok := findDOI(append([]string{fields["Subject"], fields["Keywords"]}, pages[:min(len(pages), doiPages)]...)...); ok {
		out.Metadata.Identifiers = append(out.Metadata.Identifiers, id)
	}
	if !out.HasText {
		out.Sections = nil
	}

	cover, err := runPoppler(ctx, pdftoppm, "-f", "1", "-l", "1", "-singlefile", "-png", "-scale-to-x", coverWidth, "-scale-to-y", "-1", path)
	if err != nil {
		return Extracted{}, err
	}
	out.Cover = cover
	return out, nil
}

// doiPages is how many of a PDF's first pages are looked through for its
// DOI: a paper prints it on its first, a book on the back of its title page.
const doiPages = 3

// doiPattern is a DOI as it is printed: the prefix 10. and a registrant,
// then anything up to white space.
var doiPattern = regexp.MustCompile(`\b10\.\d{4,9}/[^\s"<>]+`)

// findDOI is the first DOI in the texts, without the punctuation a
// sentence puts after it.
func findDOI(texts ...string) (catalog.Identifier, bool) {
	for _, text := range texts {
		for _, m := range doiPattern.FindAllString(text, -1) {
			m = strings.TrimRight(m, ".,;:)]}'")
			if id, ok := catalog.NormalizeIdentifier(catalog.IDDOI, m); ok && strings.Contains(id.Value, "/") {
				return id, true
			}
		}
	}
	return catalog.Identifier{}, false
}

// runPoppler runs one of poppler's programs. What goes wrong with it is
// taken to be the file's doing: a PDF that makes poppler fail, crash or hang
// will do so again.
func runPoppler(ctx context.Context, program string, args ...string) ([]byte, error) {
	out, err := procexec.Run(ctx, pdfLimits, program, args...)
	var exit *procexec.ExitError
	switch {
	case err == nil:
		return out, nil
	case errors.As(err, &exit), errors.Is(err, procexec.ErrTimeout), errors.Is(err, procexec.ErrOutputTooLarge):
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return nil, err
}

// pdfInfoFields reads pdfinfo's "Name: value" lines.
func pdfInfoFields(out []byte) map[string]string {
	fields := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		name, value, ok := strings.Cut(scanner.Text(), ":")
		if ok {
			fields[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	return fields
}

func pdfMetadata(fields map[string]string) catalog.FileMetadata {
	var m catalog.FileMetadata
	title := strings.TrimSpace(strings.TrimPrefix(fields["Title"], "Microsoft Word - "))
	if !junkTitle.MatchString(title) {
		m.Title = title
	}
	for name := range strings.SplitSeq(fields["Author"], ";") {
		if name = strings.TrimSpace(name); name != "" {
			m.Contributors = append(m.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
		}
	}
	for tag := range strings.FieldsFuncSeq(fields["Keywords"], func(r rune) bool { return r == ',' || r == ';' }) {
		if tag = strings.TrimSpace(tag); tag != "" {
			m.Tags = append(m.Tags, tag)
		}
	}
	return m
}
