package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// AutoKinds are the kinds an automatic run may apply: those where the data
// decides. A clash is never one, as either side may be right.
var AutoKinds = []string{KindPlaceholders, KindTitles, KindAuthors, KindSeries}

const (
	// MinSeriesBooks is how many titles must name a series, one way, before
	// it is set on all of them by itself.
	MinSeriesBooks = 3
)

// Auto is a suggestion an automatic run may apply, and why.
type Auto struct {
	Suggestion
	Rule   string
	Reason string
}

// RuleOf is the code of the rule that applies a kind by itself.
func RuleOf(kind string) string { return "cleanup." + kind }

// Automatic returns the suggestions of the kinds that the data decides, for
// the books the scope sees. Everything else stays a suggestion.
func (s *Service) Automatic(ctx context.Context, scope library.Scope, kind string) ([]Auto, error) {
	switch kind {
	case KindPlaceholders, KindTitles, KindAuthors, KindSeries:
	default:
		return nil, ErrKind
	}
	found, err := s.find(ctx, scope, kind, nil)
	if err != nil {
		return nil, err
	}
	var out []Auto
	for _, sg := range found {
		reason, ok, err := s.decide(ctx, scope, sg)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, Auto{Suggestion: sg, Rule: RuleOf(kind), Reason: reason})
		}
	}
	return out, nil
}

func (s *Service) decide(ctx context.Context, scope library.Scope, sg Suggestion) (string, bool, error) {
	switch sg.Kind {
	case KindPlaceholders:
		return "A tool's stand-in is no author.", true, nil
	case KindTitles:
		return "A shop's addition to the title.", sg.Suggested != "" && sg.Suggested != sg.Found[0].Name, nil
	case KindAuthors:
		for _, n := range sg.Found[1:] {
			if !SpellingOnly(sg.Found[0].Name, n.Name) {
				return "", false, nil
			}
		}
		return "One person, written in different order, case, accents or initials only.", len(sg.Found) > 1, nil
	case KindSeries:
		return s.decideSeries(ctx, scope, sg)
	}
	return "", false, nil
}

// decideSeries accepts a series in titles when enough titles agree on it, no
// two take one place, and the series is known: a visible library holds it
// or a stored answer of a provider names it.
func (s *Service) decideSeries(ctx context.Context, scope library.Scope, sg Suggestion) (string, bool, error) {
	key := catalog.Key(sg.Suggested)
	if key == "" {
		return "", false, nil
	}
	places := map[float64]bool{}
	var books []uuid.UUID
	for _, n := range sg.Found {
		if n.Book == nil || n.Place == nil || catalog.Key(n.Place.Series) != key || places[n.Place.Position] {
			return "", false, nil
		}
		places[n.Place.Position] = true
		books = append(books, *n.Book)
	}
	if len(books) < MinSeriesBooks {
		return "", false, nil
	}
	var known bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM series s JOIN books b ON b.series_id = s.id
               WHERE s.name_key = $1 AND b.deleted_at IS NULL
                 AND b.library_id IN (SELECT * FROM visible_library_ids($2, $3)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit($2, $3))))`,
		key, scope.Viewer, scope.SeesAll).Scan(&known)
	if err != nil {
		return "", false, err
	}
	if known {
		return fmt.Sprintf("%d titles name the series and no two share a place; a book in a library you see is in it.", len(books)), true, nil
	}
	rows, err := sqlc.New(s.pool).ListMatchRecords(ctx, sqlc.ListMatchRecordsParams{BookIds: books, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return "", false, err
	}
	for _, m := range rows {
		var r metadata.Record
		if json.Unmarshal(m.Record, &r) == nil && catalog.Key(r.Series) == key {
			return fmt.Sprintf("%d titles name the series and no two share a place; %s names it too.", len(books), m.Provider), true, nil
		}
	}
	if s.Meta != nil {
		for _, id := range books {
			book, err := s.books.Get(ctx, scope, id)
			if err != nil {
				continue
			}
			for _, r := range s.Meta.Cached(ctx, book) {
				if catalog.Key(r.Series) == key {
					return fmt.Sprintf("%d titles name the series and no two share a place; %s names it too.", len(books), r.Provider), true, nil
				}
			}
		}
	}
	return "", false, nil
}

// AutoChange is the change an automatic run makes to one book.
type AutoChange struct {
	bulk.BookChange
	Rule   string
	Reason string
}

// AutoChanges turns the suggestions Automatic returned into the change of
// each book they touch.
func (s *Service) AutoChanges(ctx context.Context, scope library.Scope, kind string, autos []Auto) ([]AutoChange, error) {
	if len(autos) == 0 {
		return nil, nil
	}
	chosen := make([]Suggestion, len(autos))
	for i, a := range autos {
		chosen[i] = a.Suggestion
	}
	changes, err := s.changes(ctx, scope, kind, chosen)
	if err != nil {
		return nil, err
	}
	out := make([]AutoChange, len(changes))
	for i, c := range changes {
		out[i] = AutoChange{BookChange: c, Rule: autos[0].Rule, Reason: autos[0].Reason + " " + Describe(c.Change)}
	}
	return out, nil
}

// Describe says in English what a clean-up change does, for a run's log.
func Describe(c catalog.Change) string {
	var parts []string
	for _, r := range c.RenameAuthors {
		parts = append(parts, fmt.Sprintf("Author %q becomes %q.", r.From, r.To))
	}
	for _, a := range c.RemoveAuthors {
		parts = append(parts, fmt.Sprintf("Author %q is taken off.", a))
	}
	if c.Title != nil {
		parts = append(parts, fmt.Sprintf("Title becomes %q.", *c.Title))
	}
	if c.Series != nil {
		parts = append(parts, fmt.Sprintf("Series becomes %q.", *c.Series))
	}
	return strings.Join(parts, " ")
}
