package cleanup

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// Whether a provider's answer backs a suggestion up.
const (
	VerdictAgrees  = "agrees"
	VerdictDiffers = "differs"
)

// Where a provider's answer was kept: a match found for the book, or an
// answer cached under one of its identifiers.
const (
	SourceMatch  = "match"
	SourceCached = "cached"
)

// Options say which suggestion's fix a preview shows and how much of it.
type Options struct {
	// To is that of a Pick.
	To string
	// Offset and Limit page the books.
	Offset int
	Limit  int
}

// Field is one field of a book as the fix leaves it. Locked says the fix
// leaves it as it is.
type Field struct {
	Name   string
	Before string
	After  string
	Locked bool
}

// Evidence is what a provider said about a book, in the field the
// suggestion is about, and whether that backs the suggestion up. It is only
// ever shown: nothing is applied from it.
type Evidence struct {
	Provider string
	Source   string
	// Field is series, title or authors, and Value what the provider says.
	Field   string
	Value   string
	Verdict string
}

// BookView is one book a fix changes.
type BookView struct {
	ID       uuid.UUID
	Title    string
	CoverKey string
	Fields   []Field
	Evidence []Evidence
}

// Preview is what a fix does to the books of one suggestion, as Apply would
// do it.
type Preview struct {
	Suggestion Suggestion
	// Total is how many books the fix changes; Books are those from Offset
	// on.
	Total int
	Books []BookView
}

// Preview shows what applying a suggestion does to each of its books, from
// the same changes Apply makes, with what the providers are known to say
// about them. It asks no provider.
func (s *Service) Preview(ctx context.Context, scope library.Scope, kind, subject string, o Options) (Preview, error) {
	found, err := s.find(ctx, scope, kind, []string{subject})
	if err != nil {
		return Preview{}, err
	}
	sg, ok := found[subject]
	if !ok {
		return Preview{}, ErrNotFound
	}
	if sg, err = pickFix(kind, sg, Pick{Subject: subject, To: o.To}); err != nil {
		return Preview{}, err
	}
	changes, err := s.changes(ctx, scope, kind, []Suggestion{sg})
	if err != nil {
		return Preview{}, err
	}
	out := Preview{Suggestion: sg, Total: len(changes)}
	from := min(max(o.Offset, 0), len(changes))
	to := len(changes)
	if o.Limit > 0 {
		to = min(to, from+o.Limit)
	}
	changes = changes[from:to]

	ids := make([]uuid.UUID, len(changes))
	for i, c := range changes {
		ids[i] = c.BookID
	}
	matches, err := sqlc.New(s.pool).ListMatchRecords(ctx, sqlc.ListMatchRecordsParams{
		BookIds: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return Preview{}, err
	}
	byBook := map[uuid.UUID][]metadata.Record{}
	for _, m := range matches {
		var r metadata.Record
		if json.Unmarshal(m.Record, &r) == nil {
			r.Provider = m.Provider
			byBook[m.BookID] = append(byBook[m.BookID], r)
		}
	}

	for _, c := range changes {
		book, err := s.books.Get(ctx, scope, c.BookID)
		if err != nil {
			continue
		}
		view := BookView{ID: book.ID, Title: book.Title, CoverKey: book.CoverKey}
		for _, f := range catalog.PreviewBook(book, c.Change) {
			view.Fields = append(view.Fields, Field{Name: f.Field, Before: f.Before, After: f.After, Locked: f.Locked})
		}
		for _, r := range byBook[c.BookID] {
			if e, ok := evidenceOf(sg, book.ID, r, SourceMatch); ok {
				view.Evidence = append(view.Evidence, e)
			}
		}
		if s.Meta != nil {
			for _, r := range s.Meta.Cached(ctx, book) {
				if slices.ContainsFunc(byBook[c.BookID], func(m metadata.Record) bool { return m.Provider == r.Provider && m.ID == r.ID }) {
					continue
				}
				if e, ok := evidenceOf(sg, book.ID, r, SourceCached); ok {
					view.Evidence = append(view.Evidence, e)
				}
			}
		}
		out.Books = append(out.Books, view)
	}
	return out, nil
}

// evidenceOf reads a provider's record as far as it bears on the
// suggestion, or says it does not.
func evidenceOf(sg Suggestion, book uuid.UUID, r metadata.Record, source string) (Evidence, bool) {
	e := Evidence{Provider: r.Provider, Source: source}
	authors := make([]string, 0, len(r.Contributors))
	for _, c := range r.Contributors {
		if c.Role == "" || c.Role == catalog.RoleAuthor {
			authors = append(authors, c.Name)
		}
	}
	switch sg.Kind {
	case KindAuthors:
		var named bool
		for _, a := range authors {
			if catalog.PersonKey(a) == sg.Subject {
				named = true
				if e.Verdict != VerdictDiffers {
					e.Verdict = VerdictAgrees
					if catalog.Key(a) != catalog.Key(sg.Suggested) {
						e.Verdict = VerdictDiffers
					}
				}
			}
		}
		if !named {
			return e, false
		}
		e.Field, e.Value = "authors", strings.Join(authors, "; ")
	case KindPlaceholders:
		if len(authors) == 0 {
			return e, false
		}
		e.Field, e.Value, e.Verdict = "authors", strings.Join(authors, "; "), VerdictAgrees
		if slices.ContainsFunc(authors, func(a string) bool { return catalog.Key(a) == sg.Subject }) {
			e.Verdict = VerdictDiffers
		}
	case KindClashes:
		named := slices.ContainsFunc(authors, func(a string) bool { return catalog.Key(a) == sg.Subject })
		isSeries := catalog.Key(r.Series) == sg.Subject
		switch {
		case r.Series != "":
			e.Field, e.Value = "series", catalog.SeriesText(r.Series, r.SeriesIndex)
		case named:
			e.Field, e.Value = "authors", strings.Join(authors, "; ")
		default:
			return e, false
		}
		nameIsSeries := isSeries
		nameIsAuthor := named && !isSeries
		switch {
		case !nameIsSeries && !nameIsAuthor:
		case sg.Fix == FixClearSeries:
			e.Verdict = verdictOf(nameIsAuthor)
		default:
			e.Verdict = verdictOf(nameIsSeries)
		}
	case KindTitles:
		if r.Title == "" {
			return e, false
		}
		e.Field, e.Value = "title", r.Title
		switch catalog.Key(r.Title) {
		case catalog.Key(sg.Suggested):
			e.Verdict = VerdictAgrees
		case catalog.Key(sg.Found[0].Name):
			e.Verdict = VerdictDiffers
		}
	case KindSeries:
		if r.Series == "" {
			return e, false
		}
		e.Field, e.Value = "series", catalog.SeriesText(r.Series, r.SeriesIndex)
		e.Verdict = verdictOf(catalog.Key(r.Series) == catalog.Key(sg.Suggested))
		for _, n := range sg.Found {
			if e.Verdict == VerdictAgrees && n.Book != nil && *n.Book == book && n.Place != nil &&
				r.SeriesIndex != nil && *r.SeriesIndex != n.Place.Position {
				e.Verdict = VerdictDiffers
			}
		}
	default:
		return e, false
	}
	return e, true
}

func verdictOf(agrees bool) string {
	if agrees {
		return VerdictAgrees
	}
	return VerdictDiffers
}
