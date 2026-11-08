package metadata

import (
	"slices"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// Why a field of a record is or is not suggested for taking.
const (
	// ReasonEmpty is a field the book has no value in.
	ReasonEmpty = "empty"
	// ReasonFilename is a title guessed from a file's name.
	ReasonFilename = "filename"
	// ReasonSameEdition is a record that shares an identifier with the
	// book, whose field no person set.
	ReasonSameEdition = "sameEdition"
	// ReasonAdds is identifiers the book does not have yet; they are added,
	// never put in place of its own.
	ReasonAdds = "adds"
	// ReasonLocked is a field no provider may change.
	ReasonLocked = "locked"
	// ReasonManual is a value a person typed.
	ReasonManual = "manual"
	// ReasonLanguage is a text in another language than the book's.
	ReasonLanguage = "language"
	// ReasonFile is a value one of the book's files gave.
	ReasonFile = "file"
	// ReasonOther is a value from elsewhere, another provider's or of no
	// known source, that a record of another edition may not replace.
	ReasonOther = "other"
)

// Reasons lists every reason, for the API's description.
var Reasons = []string{
	ReasonEmpty, ReasonFilename, ReasonSameEdition, ReasonAdds, ReasonLocked,
	ReasonManual, ReasonLanguage, ReasonFile, ReasonOther,
}

// Suggestion says whether taking a field of a record is worth it, and why.
type Suggestion struct {
	Suggested bool
	Reason    string
}

// languageFields are written in the book's language, so another
// language's are not the book's.
var languageFields = []string{catalog.FieldTitle, catalog.FieldSubtitle, catalog.FieldDescription}

// Suggest says per field the record has a value in whether taking it is
// worth it: where the book has none, has only a guess from a file name, or
// the record is of the same edition and no person set the field; not where
// it would replace what a person or a file set, or is of another language.
// Identifiers are suggested when the record adds any. Fields the record
// says nothing about are left out.
func Suggest(book catalog.Book, r Record) map[string]Suggestion {
	sameEdition := false
	added := false
	for _, theirs := range r.Identifiers {
		have := slices.ContainsFunc(book.Identifiers, func(mine catalog.BookIdentifier) bool { return mine.Identifier == theirs })
		sameEdition = sameEdition || have
		added = added || !have
	}
	otherLanguage := book.Language != "" && r.Language != "" && baseLanguage(book.Language) != baseLanguage(r.Language)

	out := map[string]Suggestion{}
	field := func(name string, theirs, empty bool) {
		if !theirs {
			return
		}
		source, _ := catalog.Provenance(book.Sources[name])
		s := Suggestion{}
		switch {
		case slices.Contains(book.Locked, name):
			s.Reason = ReasonLocked
		case otherLanguage && slices.Contains(languageFields, name):
			s.Reason = ReasonLanguage
		case empty:
			s = Suggestion{Suggested: true, Reason: ReasonEmpty}
		case source == catalog.ProvenanceFilename:
			s = Suggestion{Suggested: true, Reason: ReasonFilename}
		case source == catalog.ProvenanceManual:
			s.Reason = ReasonManual
		case sameEdition:
			s = Suggestion{Suggested: true, Reason: ReasonSameEdition}
		case source == catalog.ProvenanceFile:
			s.Reason = ReasonFile
		default:
			s.Reason = ReasonOther
		}
		out[name] = s
	}
	field(catalog.FieldTitle, r.Title != "", book.Title == "")
	field(catalog.FieldSubtitle, r.Subtitle != "", book.Subtitle == "")
	field(catalog.FieldContributors, len(r.Contributors) > 0, len(book.Contributors) == 0)
	field(catalog.FieldSeries, r.Series != "", book.Series == "")
	field(catalog.FieldPublisher, r.Publisher != "", book.Publisher == "")
	field(catalog.FieldPublished, r.Published != "", book.Published() == "")
	field(catalog.FieldLanguage, r.Language != "", book.Language == "")
	field(catalog.FieldPageCount, r.PageCount != nil, book.PageCount == nil)
	field(catalog.FieldTags, len(r.Tags) > 0, len(book.Tags) == 0)
	field(catalog.FieldDescription, r.Description != "", book.Description == "")
	field(catalog.FieldCover, r.CoverURL != "", book.CoverKey == "")
	if added {
		s := Suggestion{Suggested: true, Reason: ReasonAdds}
		if slices.Contains(book.Locked, catalog.FieldIdentifiers) {
			s = Suggestion{Reason: ReasonLocked}
		}
		out[catalog.FieldIdentifiers] = s
	}
	return out
}
