// Package cleanup finds what is wrong with how books are described and
// suggests what is right: one author under several spellings, a value a tool
// left where an author belongs, an author that is the name of a series, and
// what a shop added to a title. A person applies or dismisses each; nothing
// changes by itself. Suggestions are worked out from the catalogue each time
// they are asked for, from the books the asker sees.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// The kinds of suggestion.
const (
	// KindAuthors is one person under several spellings; the subject is
	// their catalog.PersonKey.
	KindAuthors = "authors"
	// KindPlaceholders is a value a tool left in place of an author's name;
	// the subject is its key.
	KindPlaceholders = "placeholders"
	// KindClashes is an author whose books are in a series of the same name;
	// the subject is its key.
	KindClashes = "clashes"
	// KindTitles is a title with a shop's addition; the subject is the
	// book's ID.
	KindTitles = "titles"
)

// Kinds lists every kind, in the order the page shows them.
var Kinds = []string{KindAuthors, KindPlaceholders, KindClashes, KindTitles}

// What applying a suggestion does.
const (
	FixMerge        = "merge"
	FixRemoveAuthor = "removeAuthor"
	FixClearSeries  = "clearSeries"
	FixRetitle      = "retitle"
)

// Fixes lists every fix.
var Fixes = []string{FixMerge, FixRemoveAuthor, FixClearSeries, FixRetitle}

// placeholderAuthors are the keys of what tools write when they have no
// author: "authors_sort" is a template value left unfilled. "Anonymous" is
// a real attribution and not among them.
var placeholderAuthors = []string{
	"authors sort", "author sort", "sort author", "unknown", "unknown author",
	"unbekannt", "unbekannter autor", "author", "autor", "various", "verschiedene",
	"n a", "none", "null",
}

// ErrKind is a kind of suggestion there is none of.
var ErrKind = errors.New("no such kind of suggestion")

// ErrNotFound is a suggestion the viewer has not got: dealt with since, or
// about books they do not see.
var ErrNotFound = errors.New("no such suggestion")

// Name is a name that was found, with the books the viewer sees that carry
// it.
type Name struct {
	Name  string
	Books int
}

// Suggestion is one thing found and what to do about it.
type Suggestion struct {
	Kind    string
	Subject string
	// Found are the names found: the spellings of an author, most used
	// first, or the one author or title.
	Found []Name
	Fix   string
	// Suggested is the author's spelling to keep, or the title without its
	// addition.
	Suggested string
	// Books is how many books it is about: those of every spelling of an
	// author, of the author or series that clash, or the one of a title.
	Books int
	// BookID is the book of a title.
	BookID *uuid.UUID
	value  string
}

// Cursor is where a page of suggestions ends: they are ordered by the books
// they touch, most first, then by subject.
type Cursor struct {
	Books   int
	Subject string
}

// Page is some of the suggestions of a kind.
type Page struct {
	Suggestions []Suggestion
	// Total is how many there are of the kind.
	Total int
	Next  *Cursor
}

// Starter starts a bulk change of each book its own way: bulk.Service.
type Starter interface {
	StartEach(ctx context.Context, scope library.Scope, changes []bulk.BookChange) (uuid.UUID, error)
}

// Service finds and applies suggestions.
type Service struct {
	pool *pgxpool.Pool
	bulk Starter
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool, bulk Starter) *Service {
	return &Service{pool: pool, bulk: bulk}
}

// source is the SQL function that finds a kind, with its arguments after
// the viewer's two.
func source(kind string) (string, []any, error) {
	switch kind {
	case KindAuthors:
		return "cleanup_author_spellings($1, $2)", nil, nil
	case KindPlaceholders:
		return "cleanup_placeholder_authors($1, $2, $3)", []any{placeholderAuthors}, nil
	case KindClashes:
		return "cleanup_name_clashes($1, $2)", nil, nil
	case KindTitles:
		return "cleanup_titles($1, $2)", nil, nil
	}
	return "", nil, ErrKind
}

// List returns a page of the suggestions of a kind that nobody dismissed,
// after the cursor when one is given.
func (s *Service) List(ctx context.Context, scope library.Scope, kind string, after *Cursor, limit int) (Page, error) {
	from, extra, err := source(kind)
	if err != nil {
		return Page{}, err
	}
	args := append([]any{scope.Viewer, scope.SeesAll}, extra...)
	n := len(args)
	args = append(args, kind, limit+1)
	keyset := ""
	if after != nil {
		args = append(args, after.Books, after.Subject)
		keyset = fmt.Sprintf("WHERE s.books < $%d OR (s.books = $%d AND s.subject > $%d)", n+3, n+3, n+4)
	}
	// The suggestions are worked out once for the page and its count.
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
WITH found AS MATERIALIZED (
    SELECT s.* FROM %s s
    WHERE NOT EXISTS (SELECT 1 FROM cleanup_dismissals d
                      WHERE d.kind = $%d AND d.subject = s.subject AND d.value = s.value)
)
SELECT s.subject, s.value, s.books, s.names, s.counts, (SELECT count(*) FROM found)::int
FROM found s
%s
ORDER BY s.books DESC, s.subject
LIMIT $%d`, from, n+1, keyset, n+2), args...)
	if err != nil {
		return Page{}, err
	}
	var page Page
	for rows.Next() {
		var sg Suggestion
		var names []string
		var counts []int32
		if err := rows.Scan(&sg.Subject, &sg.value, &sg.Books, &names, &counts, &page.Total); err != nil {
			rows.Close()
			return Page{}, err
		}
		for i, name := range names {
			sg.Found = append(sg.Found, Name{Name: name, Books: int(counts[i])})
		}
		sg.Kind = kind
		page.Suggestions = append(page.Suggestions, suggest(sg))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Suggestions) > limit {
		page.Suggestions = page.Suggestions[:limit]
		last := page.Suggestions[limit-1]
		page.Next = &Cursor{Books: last.Books, Subject: last.Subject}
	}
	return page, nil
}

// suggest fills in what to do about what was found.
func suggest(sg Suggestion) Suggestion {
	switch sg.Kind {
	case KindAuthors:
		sg.Fix = FixMerge
		sg.Suggested = sg.Found[0].Name
	case KindPlaceholders:
		sg.Fix = FixRemoveAuthor
	case KindClashes:
		sg.Fix = sg.value
	case KindTitles:
		sg.Fix = FixRetitle
		sg.Suggested = CleanTitle(sg.Found[0].Name)
		if id, err := uuid.Parse(sg.Subject); err == nil {
			sg.BookID = &id
		}
	}
	return sg
}

// Counts returns how many suggestions of each kind nobody dismissed.
func (s *Service) Counts(ctx context.Context, scope library.Scope) (map[string]int, error) {
	out := map[string]int{}
	for _, kind := range Kinds {
		from, extra, err := source(kind)
		if err != nil {
			return nil, err
		}
		args := append([]any{scope.Viewer, scope.SeesAll}, extra...)
		args = append(args, kind)
		var n int
		err = s.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT count(*) FROM %s s
WHERE NOT EXISTS (SELECT 1 FROM cleanup_dismissals d
                  WHERE d.kind = $%d AND d.subject = s.subject AND d.value = s.value)`, from, len(args)), args...).Scan(&n)
		if err != nil {
			return nil, err
		}
		out[kind] = n
	}
	return out, nil
}

// all returns every suggestion of a kind nobody dismissed, by subject.
func (s *Service) all(ctx context.Context, scope library.Scope, kind string) (map[string]Suggestion, error) {
	page, err := s.List(ctx, scope, kind, nil, 1<<30)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Suggestion, len(page.Suggestions))
	for _, sg := range page.Suggestions {
		out[sg.Subject] = sg
	}
	return out, nil
}

// Dismiss keeps a suggestion off the page while what was found stays as it
// is.
func (s *Service) Dismiss(ctx context.Context, scope library.Scope, kind, subject string) error {
	found, err := s.all(ctx, scope, kind)
	if err != nil {
		return err
	}
	sg, ok := found[subject]
	if !ok {
		return ErrNotFound
	}
	return sqlc.New(s.pool).DismissCleanup(ctx, sqlc.DismissCleanupParams{
		Kind: kind, Subject: subject, Value: sg.value, DismissedBy: &scope.Viewer,
	})
}

// Pick is a suggestion to apply, and for an author the spelling to keep
// when it is not the one suggested: any spelling of the same person.
type Pick struct {
	Subject string
	To      string
}

// Applied is the bulk change that applies suggestions, and how many books
// it changes.
type Applied struct {
	BulkChangeID uuid.UUID
	Books        int
}

// Apply starts a bulk change that applies the picked suggestions of a kind,
// or every one of it nobody dismissed when picks is nil. It changes only
// the books the viewer sees, as a person's edit, and leaves locked fields
// as they are.
func (s *Service) Apply(ctx context.Context, scope library.Scope, kind string, picks []Pick) (Applied, error) {
	found, err := s.all(ctx, scope, kind)
	if err != nil {
		return Applied{}, err
	}
	var chosen []Suggestion
	if picks == nil {
		for _, sg := range found {
			chosen = append(chosen, sg)
		}
	}
	for _, p := range picks {
		sg, ok := found[p.Subject]
		if !ok {
			continue
		}
		if p.To != "" {
			if kind != KindAuthors {
				return Applied{}, catalog.EditError{"to": "Only an author's spelling can be chosen."}
			}
			to := strings.Join(strings.Fields(p.To), " ")
			if catalog.PersonKey(to) != sg.Subject {
				return Applied{}, catalog.EditError{"to": "Choose a spelling of the same name."}
			}
			sg.Suggested = to
		}
		chosen = append(chosen, sg)
	}
	if len(chosen) == 0 {
		return Applied{}, ErrNotFound
	}
	slices.SortFunc(chosen, func(a, b Suggestion) int { return strings.Compare(a.Subject, b.Subject) })

	changes, err := s.changes(ctx, scope, kind, chosen)
	if err != nil {
		return Applied{}, err
	}
	if len(changes) == 0 {
		return Applied{}, ErrNotFound
	}
	id, err := s.bulk.StartEach(ctx, scope, changes)
	return Applied{BulkChangeID: id, Books: len(changes)}, err
}

// changes turns suggestions into the change of each book they touch, one
// per book however many suggestions touch it.
func (s *Service) changes(ctx context.Context, scope library.Scope, kind string, chosen []Suggestion) ([]bulk.BookChange, error) {
	var order []uuid.UUID
	byBook := map[uuid.UUID]*catalog.Change{}
	change := func(id uuid.UUID) *catalog.Change {
		c, ok := byBook[id]
		if !ok {
			c = &catalog.Change{}
			byBook[id] = c
			order = append(order, id)
		}
		return c
	}

	if kind == KindTitles {
		for _, sg := range chosen {
			if sg.BookID == nil || sg.Suggested == "" {
				continue
			}
			title := sg.Suggested
			change(*sg.BookID).Title = &title
		}
	} else {
		var people, names []string
		bySubject := map[string]Suggestion{}
		for _, sg := range chosen {
			bySubject[sg.Subject] = sg
			if kind == KindAuthors {
				people = append(people, sg.Subject)
			} else {
				names = append(names, sg.Subject)
			}
		}
		credits, err := sqlc.New(s.pool).ListCleanupCredits(ctx, sqlc.ListCleanupCreditsParams{
			PersonKeys: append([]string{}, people...), NameKeys: append([]string{}, names...),
			Viewer: scope.Viewer, SeesAll: scope.SeesAll,
		})
		if err != nil {
			return nil, err
		}
		for _, cr := range credits {
			switch kind {
			case KindAuthors:
				person := ""
				if cr.PersonKey != nil {
					person = *cr.PersonKey
				}
				sg, ok := bySubject[person]
				if !ok || cr.NameKey == catalog.Key(sg.Suggested) {
					continue
				}
				c := change(cr.BookID)
				c.RenameAuthors = append(c.RenameAuthors, catalog.Rename{From: cr.Name, To: sg.Suggested})
			case KindPlaceholders:
				if _, ok := bySubject[cr.NameKey]; ok {
					c := change(cr.BookID)
					c.RemoveAuthors = append(c.RemoveAuthors, cr.Name)
				}
			case KindClashes:
				sg, ok := bySubject[cr.NameKey]
				if !ok || cr.SeriesKey != cr.NameKey {
					continue
				}
				c := change(cr.BookID)
				if sg.Fix == FixRemoveAuthor {
					c.RemoveAuthors = append(c.RemoveAuthors, cr.Name)
				} else {
					none := ""
					c.Series = &none
				}
			}
		}
	}

	out := make([]bulk.BookChange, 0, len(order))
	for _, id := range order {
		c := byBook[id]
		if err := c.Check(); err != nil {
			return nil, err
		}
		out = append(out, bulk.BookChange{BookID: id, Change: *c})
	}
	return out, nil
}
