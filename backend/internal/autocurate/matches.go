// Package autocurate applies by itself what the data decides, and only
// that: clean-up suggestions of the kinds where nothing is left to judge,
// and the fields of a provider's match that others agree on. Everything
// else stays for a person. Each run is an automatic run of internal/curation:
// every change is an action with the reason of its rule, and can be undone.
package autocurate

import (
	"slices"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/reconcile"
)

// The codes of the rules that settle a match by themselves.
const (
	RuleAccept          = "matches.accept"
	RuleDismissLanguage = "matches.language"
	RuleDismissYear     = "matches.year"
)

// MaxYearGap is how many years a record's year may lie from the file's own
// date before the record is taken for another book.
const MaxYearGap = 2

// MinTextChunks is how many chunks must have a language of their own before
// the text's language is trusted against a record's.
const MinTextChunks = 3

// Input is what a match is decided from.
type Input struct {
	Book catalog.Book
	// TextLanguage is the two-letter language the book's text is written
	// in, or "" when it is not known.
	TextLanguage string
	Match        enrich.Match
	// Others are the book's other matches that wait.
	Others []enrich.Match
	// Usage counts the books of the library per spelling of a name.
	Usage map[string]int
	// Unagreed lets a match through that no other provider and no shared
	// ISBN vouches for: a person chose the group it is in.
	Unagreed bool
}

// Outcome is what is done about a match.
type Outcome struct {
	// Dismiss says the record is not the book; Rule and Reason say why.
	Dismiss bool
	Rule    string
	Reason  string
	// Agreed says why the record is trusted: a shared ISBN, or another
	// provider's record of the same edition or work. Empty: it is not.
	Agreed string
	// Edit takes the fields the rule may take; Taken names them.
	Edit  catalog.Edit
	Taken []string
	// Conflicts are the fields whose values differ and are left to a
	// person; the match stays pending while there are any.
	Conflicts []string
}

// Settles says the match is wholly dealt with: dismissed, or taken with
// nothing left in conflict.
func (o Outcome) Settles() bool {
	return o.Dismiss || o.Agreed != "" && len(o.Conflicts) == 0
}

// Decide says what to do about one pending match of a book.
func Decide(in Input) Outcome {
	if rule, reason := contradicts(in); rule != "" {
		return Outcome{Dismiss: true, Rule: rule, Reason: reason}
	}
	out := Outcome{Rule: RuleAccept}
	out.Agreed = agreement(in)
	if out.Agreed == "" && in.Unagreed {
		out.Agreed = "a person accepted the group"
	}
	if out.Agreed == "" {
		return out
	}
	takeFields(in, &out)
	var fields []string
	fields = append(fields, out.Taken...)
	out.Reason = out.Agreed + "."
	if len(fields) > 0 {
		out.Reason += " Took " + strings.Join(fields, ", ") + " from " + in.Match.Provider + "."
	}
	if len(out.Conflicts) > 0 {
		out.Reason += " Left to a person: " + strings.Join(out.Conflicts, ", ") + "."
	}
	return out
}

func primary(lang string) string {
	lang, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(lang)), "-")
	return lang
}

func year(date string) (int, bool) {
	if len(date) < 4 {
		return 0, false
	}
	y, err := strconv.Atoi(date[:4])
	return y, err == nil
}

// contradicts finds a record that cannot be the book: written in another
// language than the text, or published years away from the file's date.
func contradicts(in Input) (rule, reason string) {
	r := in.Match.Record
	if want, have := primary(r.Language), primary(in.TextLanguage); want != "" && have != "" && want != have {
		return RuleDismissLanguage, "The record is in " + want + " and the text is in " + have + "."
	}
	if src := in.Book.Sources[catalog.FieldPublished]; strings.HasPrefix(src, "file:") {
		fileYear, ok1 := year(in.Book.Published())
		recYear, ok2 := year(r.Published)
		if ok1 && ok2 && max(fileYear-recYear, recYear-fileYear) > MaxYearGap {
			return RuleDismissYear, "The record is from " + strconv.Itoa(recYear) + " and the file says " + strconv.Itoa(fileYear) + "."
		}
	}
	return "", ""
}

func isbns(ids []catalog.Identifier) []string {
	var out []string
	for _, id := range ids {
		if id.Type == "isbn" {
			out = append(out, id.Value)
		}
	}
	return out
}

func shares(a, b []string) bool {
	return slices.ContainsFunc(a, func(x string) bool { return slices.Contains(b, x) })
}

// agreement says why the record is trusted, or "" when it is not: it shares
// an ISBN with the book, or another provider's record of the book names
// the same ISBN or the same work.
func agreement(in Input) string {
	r := in.Match.Record
	var own []catalog.Identifier
	for _, id := range in.Book.Identifiers {
		own = append(own, id.Identifier)
	}
	recISBN := isbns(r.Identifiers)
	if shares(isbns(own), recISBN) {
		return "The record has an ISBN of the book"
	}
	for _, o := range in.Others {
		if o.Provider == in.Match.Provider || o.State != enrich.StatePending {
			continue
		}
		if shares(recISBN, isbns(o.Record.Identifiers)) {
			return "The records of " + in.Match.Provider + " and " + o.Provider + " have the same ISBN"
		}
		if r.Work != "" && r.Work == o.Record.Work {
			return "The records of " + in.Match.Provider + " and " + o.Provider + " are of the same work"
		}
	}
	return ""
}

func describe(b catalog.Book) reconcile.Description {
	d := reconcile.Description{
		Title: b.Title, Subtitle: b.Subtitle, Series: b.Series, SeriesIndex: b.SeriesIndex,
		Publisher: b.Publisher, Published: b.Published(), Language: b.Language, Description: b.Description,
		Tags: b.Tags, HasCover: b.CoverKey != "", Sources: b.Sources, Locked: b.Locked,
	}
	for _, c := range b.Contributors {
		if c.Role == catalog.RoleAuthor {
			d.Authors = append(d.Authors, c.Name)
		}
	}
	for _, id := range b.Identifiers {
		d.Identifiers = append(d.Identifiers, id.Identifier)
	}
	return d
}

func describeRecord(r metadata.Record) reconcile.Description {
	d := reconcile.Description{
		Title: r.Title, Subtitle: r.Subtitle, Series: r.Series, SeriesIndex: r.SeriesIndex,
		Publisher: r.Publisher, Published: r.Published, Language: r.Language, Description: r.Description,
		Tags: r.Tags, HasCover: r.CoverURL != "", Identifiers: r.Identifiers,
	}
	for _, c := range r.Contributors {
		if c.Role == catalog.RoleAuthor || c.Role == "" {
			d.Authors = append(d.Authors, c.Name)
		}
	}
	return d
}

// takes says whether a decision puts the record's value in place of the
// book's: the book's is empty or only a guess from a file name, or is
// equal to it but untidy as it stands.
func takes(d reconcile.Decision, untidy bool) bool {
	if d.Conflict || len(d.Value) == 0 || d.Value[0] == "" {
		return false
	}
	switch d.Rule {
	case reconcile.RuleEmpty, reconcile.RuleFilename:
		return d.From == reconcile.SideB
	case reconcile.RuleEqual:
		return untidy
	}
	return false
}

// untidyOnA says that normalising changed the book's own value of a field.
func untidyOnA(d reconcile.Decision) bool {
	for _, n := range d.Notes {
		if n.Rule == reconcile.NoteTitleAddition && strings.HasPrefix(n.Reason, "side a:") {
			return true
		}
	}
	return false
}

func takeFields(in Input, out *Outcome) {
	b, r := in.Book, in.Match.Record
	res := reconcile.Reconcile(describe(b), describeRecord(r), reconcile.Options{Usage: in.Usage})
	e := &out.Edit
	e.Source = catalog.ProviderSource(in.Match.Provider)

	take := func(field, current string, set func(v string)) {
		d, _ := res.Of(field)
		guess := b.Sources[field] == catalog.SourceFilename && len(d.B) == 1 && d.B[0] != current
		switch {
		case d.Conflict:
			out.Conflicts = append(out.Conflicts, field)
		case takes(d, untidyOnA(d) || guess):
			value := d.Value[0]
			if d.Rule == reconcile.RuleEqual && len(d.B) == 1 {
				value = d.B[0]
			}
			set(value)
			out.Taken = append(out.Taken, field)
		}
	}
	take(catalog.FieldTitle, b.Title, func(v string) { e.Title = &v })
	take(catalog.FieldSubtitle, b.Subtitle, func(v string) { e.Subtitle = &v })
	take(catalog.FieldDescription, b.Description, func(v string) { e.Description = &v })
	take(catalog.FieldLanguage, b.Language, func(v string) { e.Language = &v })
	take(catalog.FieldPublished, b.Published(), func(v string) { e.Published = &v })
	take(catalog.FieldPublisher, b.Publisher, func(v string) { e.Publisher = &v })

	se, _ := res.Of(catalog.FieldSeries)
	switch {
	case se.Conflict:
		out.Conflicts = append(out.Conflicts, catalog.FieldSeries)
	case takes(se, false):
		place := catalog.SeriesPlace{Name: se.Value[0], Index: r.SeriesIndex}
		if ix, ok := res.Of(reconcile.FieldSeriesIndex); ok && !ix.Conflict && len(ix.Value) == 1 {
			if v, err := strconv.ParseFloat(ix.Value[0], 64); err == nil {
				place.Index = &v
			}
		}
		e.Series = &place
		out.Taken = append(out.Taken, catalog.FieldSeries)
	}

	if au, _ := res.Of(catalog.FieldContributors); au.Conflict {
		out.Conflicts = append(out.Conflicts, catalog.FieldContributors)
	} else if takes(au, false) && len(r.Contributors) > 0 {
		e.Contributors = &r.Contributors
		out.Taken = append(out.Taken, catalog.FieldContributors)
	}
	if len(b.Tags) == 0 && len(r.Tags) > 0 {
		e.Tags = &r.Tags
		out.Taken = append(out.Taken, catalog.FieldTags)
	}

	var own, all []catalog.Identifier
	for _, id := range b.Identifiers {
		all = append(all, id.Identifier)
		if id.FileID == nil {
			own = append(own, id.Identifier)
		}
	}
	added := false
	for _, id := range r.Identifiers {
		if !slices.Contains(all, id) {
			own, all, added = append(own, id), append(all, id), true
		}
	}
	if added {
		e.Identifiers = &own
		out.Taken = append(out.Taken, catalog.FieldIdentifiers)
	}
}
