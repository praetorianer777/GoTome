// Package autodupe resolves duplicate pairs by itself where the evidence is
// certain (#466): books of the same text are made one, books one of which
// holds the other are kept and linked. It works in runs of internal/curation,
// so that every pair is an action that can be undone, and a dry run says
// what it would do.
package autodupe

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/reconcile"
)

// Facts are what decides which book of a pair survives.
type Facts struct {
	ID uuid.UUID
	// HasState is set when anyone reads, rates or shelves the book.
	HasState bool
	// HasManual is set when a field is locked or typed by hand.
	HasManual bool
	// Formats are the formats of its live files.
	Formats []string
	// Filled counts the details it has.
	Filled int
}

func (f Facts) bestFormat() int {
	best := 0
	for _, format := range f.Formats {
		best = max(best, catalog.FormatRank(format))
	}
	return best
}

// Survivor chooses which of two books survives a merge: the one anyone has a
// reading state with, then one with a locked or hand-typed field, then the
// one with the better format, then the one with more details, then the older
// (the smaller ID). It says whether the first wins and why.
func Survivor(a, b Facts) (firstWins bool, why string) {
	switch {
	case a.HasState != b.HasState:
		return a.HasState, "someone has a reading state with it"
	case a.HasManual != b.HasManual:
		return a.HasManual, "it has a field set by hand"
	case a.bestFormat() != b.bestFormat():
		return a.bestFormat() > b.bestFormat(), "its format is better"
	case a.Filled != b.Filled:
		return a.Filled > b.Filled, "it has more details"
	}
	return a.ID.String() < b.ID.String(), "it is older"
}

// File is a live file of one of the pair's books.
type File struct {
	ID       uuid.UUID
	Book     uuid.UUID
	Kind     string
	Format   string
	Modified time.Time
	// Findings counts what makes an EPUB troublesome on an e-reader.
	Findings int
}

func better(a, b File) bool {
	switch {
	case a.Findings != b.Findings:
		return a.Findings < b.Findings
	case !a.Modified.Equal(b.Modified):
		return a.Modified.After(b.Modified)
	}
	return a.ID.String() > b.ID.String()
}

// Copies are the files to trash: where each book has exactly one file of an
// ebook format, the worse of the two, which is the better one's copy. The
// parts of an audiobook and formats only one book has are kept.
func Copies(files []File) []uuid.UUID {
	byFormat := map[string][]File{}
	for _, f := range files {
		if f.Kind == "ebook" {
			byFormat[f.Format] = append(byFormat[f.Format], f)
		}
	}
	var out []uuid.UUID
	for _, format := range slices.Sorted(mapKeys(byFormat)) {
		fs := byFormat[format]
		if len(fs) != 2 || fs[0].Book == fs[1].Book {
			continue
		}
		if better(fs[0], fs[1]) {
			out = append(out, fs[1].ID)
		} else {
			out = append(out, fs[0].ID)
		}
	}
	return out
}

func mapKeys[K comparable, V any](m map[K]V) func(yield func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// DescriptionOf is what reconcile compares of a book.
func DescriptionOf(b catalog.Book) reconcile.Description {
	d := reconcile.Description{
		Title: b.Title, Subtitle: b.Subtitle, Series: b.Series, SeriesIndex: b.SeriesIndex, Publisher: b.Publisher,
		Language: b.Language, Description: b.Description, Tags: b.Tags, HasCover: b.CoverKey != "",
		Sources: b.Sources, Locked: b.Locked,
	}
	for _, c := range b.Contributors {
		if c.Role == catalog.RoleAuthor {
			d.Authors = append(d.Authors, c.Name)
		}
	}
	if b.PublishedOn != nil {
		layout := "2006-01-02"
		switch b.PublishedPrecision {
		case "year":
			layout = "2006"
		case "month":
			layout = "2006-01"
		}
		d.Published = b.PublishedOn.Format(layout)
	}
	for _, id := range b.Identifiers {
		d.Identifiers = append(d.Identifiers, id.Identifier)
	}
	return d
}

// Filled counts the details of a book that are set.
func Filled(b catalog.Book) int {
	n := 0
	for _, set := range []bool{
		b.Subtitle != "", b.Description != "", b.Language != "", b.PublishedOn != nil, b.Publisher != "",
		b.Series != "", b.PageCount != nil, len(b.Contributors) > 0, len(b.Tags) > 0, b.CoverKey != "",
		len(b.Identifiers) > 0,
	} {
		if set {
			n++
		}
	}
	return n
}

// Fields is what a merge of the pair does with the fields.
type Fields struct {
	// Take are fields the surviving book takes whole from the other.
	Take []string
	// Union are fields it gains the other's values of.
	Union []string
	// Conflicts are fields no rule decided.
	Conflicts []string
}

// fieldOf maps a decision's field to the field a merge takes it by.
func fieldOf(field string) string {
	if field == reconcile.FieldSeriesIndex {
		return catalog.FieldSeries
	}
	return field
}

// Merging turns the decisions for the survivor (side a) and the other (side
// b) into what the merge takes. Identifiers are kept by every merge.
func Merging(res reconcile.Result) Fields {
	var f Fields
	add := func(list *[]string, field string) {
		if !slices.Contains(*list, field) {
			*list = append(*list, field)
		}
	}
	for _, d := range res.Decisions {
		field := fieldOf(d.Field)
		switch {
		case field == catalog.FieldIdentifiers:
		case d.Conflict:
			add(&f.Conflicts, d.Field)
		case d.From == reconcile.SideB:
			add(&f.Take, field)
		case d.From == reconcile.SideMerged &&
			(field == catalog.FieldTags || field == catalog.FieldContributors):
			add(&f.Union, field)
		}
	}
	slices.Sort(f.Take)
	return f
}

// fieldNames say fields in a reason, never their values.
func fieldNames(fields []string) string { return strings.Join(fields, ", ") }

// Reason says what a merge did, or would do, without naming the books or
// the values: the evidence, why the survivor was chosen, the fields.
func Reason(evidence, why string, f Fields, trashed int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s; kept one book because %s", evidence, why)
	if len(f.Take) > 0 {
		fmt.Fprintf(&b, "; took %s from the other", fieldNames(f.Take))
	}
	if len(f.Union) > 0 {
		fmt.Fprintf(&b, "; added the other's %s", fieldNames(f.Union))
	}
	if len(f.Conflicts) > 0 {
		fmt.Fprintf(&b, "; kept its own %s, which differ", fieldNames(f.Conflicts))
	}
	if trashed > 0 {
		fmt.Fprintf(&b, "; trashed %d file(s) that copy another of the same format", trashed)
	}
	return b.String()
}

// Evidence says in words what class a pair is of.
func Evidence(class string, o *dedupe.Overlap) string {
	switch class {
	case "same_text":
		return "same bytes or same text"
	}
	if o == nil {
		return class
	}
	return fmt.Sprintf("shared text, jaccard %.2f, containing %.2f and %.2f", o.Jaccard, o.AInB, o.BInA)
}
