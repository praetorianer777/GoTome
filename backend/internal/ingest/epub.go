package ingest

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// charsPerPage turns the length of a text into a page count for formats that
// have no pages: about 300 words, a page of a paperback.
const charsPerPage = 1800

// marcRoles maps the MARC relator codes EPUBs credit people with to the roles
// the catalogue knows. People in other roles, such as the cover designer, are
// not credited on the book.
var marcRoles = map[string]string{
	"aut": catalog.RoleAuthor,
	"nrt": catalog.RoleNarrator,
	"trl": catalog.RoleTranslator,
	"edt": catalog.RoleEditor,
	"ill": catalog.RoleIllustrator,
}

func extractEPUB(_ context.Context, path string) (Extracted, error) {
	f, size, err := openFile(path)
	if err != nil {
		return Extracted{}, err
	}
	defer f.Close()
	book, err := epub.Parse(f, size)
	if errors.Is(err, epub.ErrNotEPUB) || errors.Is(err, epub.ErrTooLarge) {
		return Extracted{}, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if err != nil {
		return Extracted{}, err
	}

	m := book.Metadata
	out := Extracted{
		Metadata: catalog.FileMetadata{
			Title: m.Title, Subtitle: m.Subtitle, Description: m.Description,
			Language: m.Language, Published: m.Published, Publisher: m.Publisher,
			Series: m.Series, SeriesIndex: m.SeriesIndex, Tags: m.Subjects,
		},
		ContentSHA256: book.ContentHash,
		DRM:           book.DRM,
	}
	for _, c := range m.Contributors {
		if role, ok := marcRoles[c.Role]; ok {
			out.Metadata.Contributors = append(out.Metadata.Contributors, catalog.NewContributor{
				Name: c.Name, SortName: c.FileAs, Role: role,
			})
		}
	}
	seen := map[catalog.Identifier]bool{}
	for _, ident := range m.Identifiers {
		if id, ok := catalog.NormalizeIdentifier(ident.Scheme, ident.Value); ok && !seen[id] {
			seen[id] = true
			out.Metadata.Identifiers = append(out.Metadata.Identifiers, id)
		}
	}
	if book.Cover != nil {
		out.Cover = book.Cover.Data
	}

	chars := 0
	for _, ch := range book.Chapters {
		chars += utf8.RuneCountInString(ch.Text)
		out.Sections = append(out.Sections, Section{Label: ch.Title, Text: ch.Text})
	}
	if chars > 0 {
		pages := int32((chars + charsPerPage - 1) / charsPerPage)
		out.HasText, out.Pages, out.PagesEstimated = true, &pages, true
		out.Metadata.PageCount = &pages
	}
	return out, nil
}
