package epub

import (
	"fmt"
	"io"
)

// OutlineEntry is a titled document of the spine and how far into the book
// it starts.
type OutlineEntry struct {
	Title string
	// Start is the share of the book that lies before the document, 0 to 1.
	Start float64
}

// Outline lists the spine's documents the table of contents titles, with
// where each starts. Shares are counted as foliate-js counts the fraction
// it reports: by the size the archive declares for each document, leaving
// out those the spine marks as not linear. It reads the archive's
// directory, the package document and the tables of contents, not the
// documents.
func Outline(r io.ReaderAt, size int64) ([]OutlineEntry, error) {
	zr, err := openZip(r, size, DefaultLimits)
	if err != nil {
		return nil, err
	}
	a := newArchive(zr, DefaultLimits)
	opfPath, err := a.packagePath()
	if err != nil {
		return nil, err
	}
	data, err := a.read(opfPath)
	if err != nil {
		return nil, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	pkg, err := parsePackage(data)
	if err != nil {
		return nil, fmt.Errorf("%w: package document: %v", ErrNotEPUB, err)
	}
	titles := a.contents(opfPath, pkg)

	type document struct {
		title  string
		before int64
	}
	var docs []document
	var total int64
	seen := map[string]bool{}
	for _, ref := range pkg.Spine.Items {
		item := pkg.item(ref.IDRef)
		if item == nil || seen[item.ID] || ref.Linear == "no" {
			continue
		}
		seen[item.ID] = true
		name, err := resolve(opfPath, item.Href)
		if err != nil {
			continue
		}
		f := a.find(name)
		if f == nil || f.UncompressedSize64 == 0 {
			continue
		}
		if titles[name] != "" {
			docs = append(docs, document{title: titles[name], before: total})
		}
		total += int64(f.UncompressedSize64)
	}
	out := make([]OutlineEntry, 0, len(docs))
	for _, d := range docs {
		out = append(out, OutlineEntry{Title: d.title, Start: float64(d.before) / float64(total)})
	}
	return out, nil
}
