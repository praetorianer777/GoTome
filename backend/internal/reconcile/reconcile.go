// Package reconcile decides, field by field, what two descriptions of one
// book should say, and why. It is pure: no database, no network. The
// duplicate merge, the clean-up and accepting a match call it with what the
// catalogue holds and read the decisions back.
//
// Each side is first normalised with the rules clean-up has (authors by
// person key, placeholders and publishing platforms taken out of the
// authors, shop additions off the title, a series read out of the title).
// Then each field is decided in this order: equal after normalising; one
// side empty or only a guess from a file name; one a subset of the other; a
// locked or hand-typed value against an automatic one; a provider's record
// that shares an identifier; else a conflict, left for a person.
package reconcile

import (
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Side says where a decided value came from.
type Side string

// The origins of a decided value. Merged is a union of both sides, and
// Provider a value a provider's record chose between the two.
const (
	SideA        Side = "a"
	SideB        Side = "b"
	SideMerged   Side = "merged"
	SideProvider Side = "provider"
)

// The codes of the rules that decide a field. They are stable: the run log
// stores them.
const (
	RuleEqual    = "equal"
	RuleEmpty    = "one_empty"
	RuleFilename = "filename_guess"
	RuleSubset   = "subset"
	RuleLocked   = "locked"
	RuleProvider = "provider"
	RuleUnion    = "union"
	RuleConflict = "conflict"
)

// The codes of the normalising steps a decision may report in Notes.
const (
	NotePlaceholderAuthor = "placeholder_author_removed"
	NotePlatformAuthor    = "platform_author_to_publisher"
	NoteTitleAddition     = "title_addition_removed"
	NoteSeriesFromTitle   = "series_from_title"
	NoteSubtitleSplit     = "subtitle_split_off"
)

// FieldSeriesIndex is the position in the series. It is decided with the
// series and locked with it.
const FieldSeriesIndex = "seriesIndex"

// Description is one side: what a book says about itself, with where each
// field came from.
type Description struct {
	Title       string
	Subtitle    string
	Authors     []string
	Series      string
	SeriesIndex *float64
	Publisher   string
	// Published is "2010", "2010-08" or "2010-08-31".
	Published   string
	Language    string
	Description string
	Identifiers []catalog.Identifier
	Tags        []string
	HasCover    bool

	// Sources is the book's field_sources: per catalog field name, where the
	// value came from (catalog.SourceManual, catalog.SourceFilename,
	// "provider:<name>", ...).
	Sources map[string]string
	// Locked is the book's locked_fields.
	Locked []string
}

// Options are what the decisions may lean on besides the two sides.
type Options struct {
	// Providers are records of providers that share an identifier with the
	// book (evidence of #462). They decide between two differing values.
	Providers []metadata.Record
	// Usage counts, per spelling of a person or series, the books of the
	// library that write it so: the most used spelling is the cleanest.
	Usage map[string]int
}

// Note is a normalising step that changed a side before it was compared.
type Note struct {
	Rule   string
	Reason string
}

// Decision is the outcome for one field.
type Decision struct {
	// Field is the catalog field name (catalog.FieldTitle, ...).
	Field string
	// Conflict is set when no rule decided; Value is then empty and A and B
	// hold both values, normalised.
	Conflict bool
	// From is the side the value came from; empty for a conflict.
	From Side
	// Rule is the stable code of the rule that decided; Reason says it in
	// English for the log and the pages.
	Rule   string
	Reason string
	// Value is the decided value, a single element for a single-valued field.
	Value []string
	A, B  []string
	// Notes are the normalising steps that changed either side of this field.
	Notes []Note
}

// Result is every field's decision and the description they make.
type Result struct {
	Decisions []Decision
	// Merged holds the decided values; a field in conflict is left empty.
	// Its Sources and Locked are not set.
	Merged Description
}

// Conflicts returns the decisions nobody could make.
func (r Result) Conflicts() []Decision {
	var out []Decision
	for _, d := range r.Decisions {
		if d.Conflict {
			out = append(out, d)
		}
	}
	return out
}

// Of returns the decision for a field.
func (r Result) Of(field string) (Decision, bool) {
	for _, d := range r.Decisions {
		if d.Field == field {
			return d, true
		}
	}
	return Decision{}, false
}

type side struct {
	d    Description
	name Side
}

func (s side) source(field string) string { return s.d.Sources[field] }
func (s side) guess(field string) bool    { return s.source(field) == catalog.SourceFilename }
func (s side) strong(field string) bool {
	return slices.Contains(s.d.Locked, field) || s.source(field) == catalog.SourceManual
}

type run struct {
	a, b  side
	o     Options
	notes map[string][]Note
}

// Reconcile decides every field of the two descriptions.
func Reconcile(a, b Description, o Options) Result {
	r := &run{o: o, notes: map[string][]Note{}}
	r.a = side{d: r.normalise(a, "a"), name: SideA}
	r.b = side{d: r.normalise(b, "b"), name: SideB}
	r.alignTitles()

	var res Result
	add := func(d Decision) { d.Notes = r.notes[d.Field]; res.Decisions = append(res.Decisions, d) }

	t := r.text(catalog.FieldTitle, r.a.d.Title, r.b.d.Title, textRules{key: catalog.Key})
	add(t)
	res.Merged.Title = first(t.Value)
	st := r.text(catalog.FieldSubtitle, r.a.d.Subtitle, r.b.d.Subtitle, textRules{key: catalog.Key, subset: contains})
	add(st)
	res.Merged.Subtitle = first(st.Value)

	au := r.authors()
	add(au)
	res.Merged.Authors = au.Value

	se := r.text(catalog.FieldSeries, r.a.d.Series, r.b.d.Series, textRules{key: catalog.Key})
	add(se)
	res.Merged.Series = first(se.Value)
	ix := r.seriesIndex(se)
	add(ix)
	if len(ix.Value) == 1 {
		res.Merged.SeriesIndex = r.indexOf(ix)
	}

	pu := r.text(catalog.FieldPublisher, r.a.d.Publisher, r.b.d.Publisher, textRules{key: catalog.Key})
	add(pu)
	res.Merged.Publisher = first(pu.Value)
	pd := r.text(catalog.FieldPublished, r.a.d.Published, r.b.d.Published, textRules{key: strings.TrimSpace, subset: prefixOf, fuzzy: true})
	add(pd)
	res.Merged.Published = first(pd.Value)
	la := r.text(catalog.FieldLanguage, r.a.d.Language, r.b.d.Language, textRules{key: strings.ToLower, subset: languageSubset, fuzzy: true})
	add(la)
	res.Merged.Language = first(la.Value)
	de := r.text(catalog.FieldDescription, r.a.d.Description, r.b.d.Description, textRules{key: catalog.Key, subset: contains})
	add(de)
	res.Merged.Description = first(de.Value)

	id := r.identifiers()
	add(id)
	for _, v := range id.Value {
		typ, val, _ := strings.Cut(v, ":")
		res.Merged.Identifiers = append(res.Merged.Identifiers, catalog.Identifier{Type: typ, Value: val})
	}
	tg := r.tags()
	add(tg)
	res.Merged.Tags = tg.Value
	cv := r.cover()
	add(cv)
	res.Merged.HasCover = len(cv.Value) > 0
	return res
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (r *run) note(field, rule, who, reason string) {
	r.notes[field] = append(r.notes[field], Note{Rule: rule, Reason: "side " + who + ": " + reason})
}
