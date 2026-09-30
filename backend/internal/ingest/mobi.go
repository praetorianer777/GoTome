package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/mobi"
)

func extractMOBI(_ context.Context, path string) (Extracted, error) {
	f, size, err := openFile(path)
	if err != nil {
		return Extracted{}, err
	}
	defer f.Close()
	book, err := mobi.Parse(f, size)
	if errors.Is(err, mobi.ErrNotMOBI) || errors.Is(err, mobi.ErrTooLarge) {
		return Extracted{}, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if err != nil {
		return Extracted{}, err
	}

	m := book.Metadata
	out := Extracted{
		Metadata: catalog.FileMetadata{
			Title: m.Title, Description: m.Description, Language: m.Language,
			Published: m.Published, Publisher: m.Publisher, Tags: m.Subjects,
		},
		ContentSHA256: book.ContentHash,
		DRM:           book.DRM,
	}
	// The other credits of a Kindle file name the software that made it
	// more often than a person.
	for _, name := range m.Authors {
		out.Metadata.Contributors = append(out.Metadata.Contributors, credited(name))
	}
	for scheme, value := range map[string]string{"isbn": m.ISBN, "asin": m.ASIN} {
		if id, ok := catalog.NormalizeIdentifier(scheme, value); ok {
			out.Metadata.Identifiers = append(out.Metadata.Identifiers, id)
		}
	}
	if book.Cover != nil {
		out.Cover = book.Cover.Data
	}
	if chars := utf8.RuneCountInString(book.Text); chars > 0 {
		pages := int32((chars + charsPerPage - 1) / charsPerPage)
		out.HasText, out.Pages, out.PagesEstimated = true, &pages, true
		out.Metadata.PageCount = &pages
		out.Sections = []Section{{Text: book.Text}}
	}
	return out, nil
}

// credited turns an author as Kindle files often write one, "Austen, Jane",
// into the name as it is credited, keeping the written form for sorting.
func credited(name string) catalog.NewContributor {
	c := catalog.NewContributor{Name: name, Role: catalog.RoleAuthor}
	family, given, ok := strings.Cut(name, ",")
	if ok && !strings.Contains(given, ",") && strings.TrimSpace(given) != "" && strings.TrimSpace(family) != "" {
		c.Name = strings.TrimSpace(given) + " " + strings.TrimSpace(family)
		c.SortName = name
	}
	return c
}
