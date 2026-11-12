package metadata

import (
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestSuggestTakesWhatTheBookLacksAndKeepsWhatWasSet(t *testing.T) {
	isbn := catalog.Identifier{Type: "isbn", Value: "9780141439587"}
	other := catalog.Identifier{Type: "isbn", Value: "9780199535521"}
	pages := int32(400)
	record := Record{
		Provider: "openlibrary", Title: "Emma", Subtitle: "A Novel", Description: "A comedy of manners.",
		Language: "en", Published: "1815", Publisher: "John Murray", Series: "Austen", PageCount: &pages,
		Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
		Tags:         []string{"Fiction"}, Identifiers: []catalog.Identifier{isbn}, CoverURL: "https://example.org/c.jpg",
	}
	file := catalog.FileSource("epub", [16]byte{1})
	base := catalog.Book{
		Title: "emma_final", Language: "en", Publisher: "Penguin", Description: "Old text.",
		Tags:    []string{"Classics"},
		Sources: map[string]string{catalog.FieldTitle: catalog.SourceFilename, catalog.FieldPublisher: file, catalog.FieldDescription: catalog.SourceManual, catalog.FieldTags: catalog.ProviderSource("hardcover")},
	}

	cases := []struct {
		name  string
		book  func(b *catalog.Book)
		field string
		want  Suggestion
	}{
		{"an empty field", nil, catalog.FieldSubtitle, Suggestion{true, ReasonEmpty}},
		{"an empty cover", nil, catalog.FieldCover, Suggestion{true, ReasonEmpty}},
		{"a title from the file name", nil, catalog.FieldTitle, Suggestion{true, ReasonFilename}},
		{"a file's value", nil, catalog.FieldPublisher, Suggestion{false, ReasonFile}},
		{"a person's value", nil, catalog.FieldDescription, Suggestion{false, ReasonManual}},
		{"another provider's value", nil, catalog.FieldTags, Suggestion{false, ReasonOther}},
		{"identifiers it adds", nil, catalog.FieldIdentifiers, Suggestion{true, ReasonAdds}},
		{"a locked empty field", func(b *catalog.Book) { b.Locked = []string{catalog.FieldSubtitle} }, catalog.FieldSubtitle, Suggestion{false, ReasonLocked}},
		{"a file's value of the same edition", func(b *catalog.Book) {
			b.Identifiers = []catalog.BookIdentifier{{Identifier: isbn}}
		}, catalog.FieldPublisher, Suggestion{true, ReasonSameEdition}},
		{"a person's value of the same edition", func(b *catalog.Book) {
			b.Identifiers = []catalog.BookIdentifier{{Identifier: isbn}}
		}, catalog.FieldDescription, Suggestion{false, ReasonManual}},
		{"a value of no known source", func(b *catalog.Book) { b.Sources = nil }, catalog.FieldPublisher, Suggestion{false, ReasonOther}},
		{"a text in another language", func(b *catalog.Book) {
			b.Language = "de-AT"
			b.Description = ""
		}, catalog.FieldDescription, Suggestion{false, ReasonLanguage}},
		{"a title in another language", func(b *catalog.Book) { b.Language = "de" }, catalog.FieldTitle, Suggestion{false, ReasonLanguage}},
		{"a publisher in another language", func(b *catalog.Book) { b.Language = "de" }, catalog.FieldPublisher, Suggestion{false, ReasonFile}},
		{"the same language in another form", func(b *catalog.Book) { b.Language = "EN-gb" }, catalog.FieldTitle, Suggestion{true, ReasonFilename}},
		{"locked identifiers", func(b *catalog.Book) { b.Locked = []string{catalog.FieldIdentifiers} }, catalog.FieldIdentifiers, Suggestion{false, ReasonLocked}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			book := base
			if c.book != nil {
				c.book(&book)
			}
			got, ok := Suggest(book, record)[c.field]
			if !ok || got != c.want {
				t.Errorf("%s: got %+v (%v), want %+v", c.field, got, ok, c.want)
			}
		})
	}

	t.Run("fields the record lacks, and identifiers the book has", func(t *testing.T) {
		book := base
		book.Identifiers = []catalog.BookIdentifier{{Identifier: isbn}, {Identifier: other}}
		got := Suggest(book, Record{Title: "Emma", Identifiers: []catalog.Identifier{other}})
		if len(got) != 1 || got[catalog.FieldTitle] != (Suggestion{true, ReasonFilename}) {
			t.Errorf("got %+v, want the title alone", got)
		}
	})
}
