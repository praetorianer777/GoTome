package catalog

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MaxSelection is the most books one bulk change takes.
const MaxSelection = 5000

// ErrTooMany is a selection of more than MaxSelection books.
var ErrTooMany = fmt.Errorf("a selection holds at most %d books", MaxSelection)

// Select returns the IDs of the books the scope may see that are in the
// library, match the filter and, when ids is not empty, are among ids; in
// title order, or ErrTooMany.
func (s *Service) Select(ctx context.Context, scope library.Scope, libraryID *uuid.UUID, tree filter.Node, ids []uuid.UUID) ([]uuid.UUID, error) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where, err := visibleBooks(scope, libraryID, tree, false, arg)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		where = append(where, "b.id = ANY("+arg(ids)+"::uuid[])")
	}
	query := `SELECT b.id FROM books b WHERE ` + strings.Join(where, " AND ") + `
ORDER BY b.sort_title, b.id LIMIT ` + arg(MaxSelection+1)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	selected, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	if len(selected) > MaxSelection {
		return nil, ErrTooMany
	}
	return selected, nil
}

// Change is what a person changes on many books at once: values set on
// each, and authors and tags added to or taken from what each book has.
// Like an Edit, it records every field it changes as typed by hand and
// locks it; unlike one, it leaves a field that was locked before as it is,
// unless IncludeLocked.
type Change struct {
	Language  *string `json:"language,omitempty"`
	Published *string `json:"published,omitempty"`
	Publisher *string `json:"publisher,omitempty"`
	// Series names the series; each book keeps its position. "" takes the
	// books out of their series.
	Series *string `json:"series,omitempty"`
	// Authors replace the authors; narrators and the other contributors
	// stay.
	Authors       *[]string `json:"authors,omitempty"`
	AddAuthors    []string  `json:"addAuthors,omitempty"`
	RemoveAuthors []string  `json:"removeAuthors,omitempty"`
	Tags          *[]string `json:"tags,omitempty"`
	AddTags       []string  `json:"addTags,omitempty"`
	RemoveTags    []string  `json:"removeTags,omitempty"`
	// Locks puts the lock on a field or takes it off, as in an Edit.
	Locks         map[string]bool `json:"locks,omitempty"`
	IncludeLocked bool            `json:"includeLocked,omitempty"`
}

// fields are the fields the change sets values of.
func (c Change) fields() []string {
	var out []string
	for field, set := range map[string]bool{
		FieldLanguage:     c.Language != nil,
		FieldPublished:    c.Published != nil,
		FieldPublisher:    c.Publisher != nil,
		FieldSeries:       c.Series != nil,
		FieldContributors: c.Authors != nil || len(c.AddAuthors) > 0 || len(c.RemoveAuthors) > 0,
		FieldTags:         c.Tags != nil || len(c.AddTags) > 0 || len(c.RemoveTags) > 0,
	} {
		if set {
			out = append(out, field)
		}
	}
	slices.Sort(out)
	return out
}

// Check finds what is wrong with the change, as Edit does, and brings the
// language into the form it is stored in.
func (c *Change) Check() error {
	if len(c.fields()) == 0 && len(c.Locks) == 0 {
		return EditError{"change": "Say what to change."}
	}
	probe := Edit{Language: c.Language, Published: c.Published, Locks: c.Locks}
	if err := probe.check(); err != nil {
		return err
	}
	c.Language = probe.Language
	return nil
}

// Changed is what a change did to one book.
type Changed struct {
	// Values is set when the change set any value; a book that already was
	// as the change would leave it is not touched.
	Values bool
	// Locks is set when only locks were put on or taken off.
	Locks bool
	// Skipped are the fields the change left alone because they are locked.
	Skipped []string
}

// ChangeTx applies a change to one book the scope may see, inside a
// transaction the caller runs, or returns ErrNotFound. The change must have
// passed Check.
func ChangeTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, c Change) (Changed, error) {
	q := sqlc.New(tx)
	if _, err := q.LockVisibleBook(ctx, sqlc.LockVisibleBookParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Changed{}, ErrNotFound
		}
		return Changed{}, err
	}
	book, err := get(ctx, q, scope, id)
	if err != nil {
		return Changed{}, err
	}
	var out Changed
	for _, field := range c.fields() {
		if slices.Contains(book.Locked, field) && !c.IncludeLocked {
			out.Skipped = append(out.Skipped, field)
		}
	}
	use := func(field string) bool { return !slices.Contains(out.Skipped, field) }

	var e Edit
	if c.Language != nil && use(FieldLanguage) && *c.Language != book.Language {
		e.Language = c.Language
	}
	if c.Published != nil && use(FieldPublished) && strings.TrimSpace(*c.Published) != book.Published() {
		e.Published = c.Published
	}
	// The catalogue finds a publisher or series by its key and keeps the
	// spelling it knows, so another spelling of the same name changes
	// nothing.
	if c.Publisher != nil && use(FieldPublisher) && Key(*c.Publisher) != Key(book.Publisher) {
		e.Publisher = c.Publisher
	}
	if c.Series != nil && use(FieldSeries) && Key(*c.Series) != Key(book.Series) {
		e.Series = &SeriesPlace{Name: *c.Series, Index: book.SeriesIndex}
	}
	if use(FieldContributors) {
		var authors, others []NewContributor
		for _, p := range book.Contributors {
			credit := NewContributor{Name: p.Name, SortName: p.SortName, Role: p.Role}
			if p.Role == RoleAuthor {
				authors = append(authors, credit)
			} else {
				others = append(others, credit)
			}
		}
		names := make([]string, len(authors))
		for i, a := range authors {
			names[i] = a.Name
		}
		if after := revise(names, c.Authors, c.AddAuthors, c.RemoveAuthors); !slices.Equal(keys(after), keys(names)) {
			var credits []NewContributor
			for _, name := range after {
				credit := NewContributor{Name: name, Role: RoleAuthor}
				if i := slices.IndexFunc(authors, func(a NewContributor) bool { return Key(a.Name) == Key(name) }); i >= 0 {
					credit = authors[i]
				}
				credits = append(credits, credit)
			}
			credits = append(credits, others...)
			e.Contributors = &credits
		}
	}
	if use(FieldTags) {
		after := revise(book.Tags, c.Tags, c.AddTags, c.RemoveTags)
		if !slices.Equal(slices.Sorted(slices.Values(keys(after))), slices.Sorted(slices.Values(keys(book.Tags)))) {
			e.Tags = &after
		}
	}
	out.Values = e.ChangesValues()
	for field, on := range c.Locks {
		if slices.Contains(book.Locked, field) != on {
			e.Locks = maps.Clone(c.Locks)
			out.Locks = !out.Values
			break
		}
	}
	if !out.Values && !out.Locks {
		return out, nil
	}
	return out, EditTx(ctx, tx, scope, id, e)
}

// revise is a list of names replaced by set when it is given, then with
// remove taken out and add put in, each name once by its key.
func revise(names []string, set *[]string, add, remove []string) []string {
	if set != nil {
		names = *set
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range append(slices.Clone(names), add...) {
		k := Key(name)
		if k == "" || seen[k] || slices.ContainsFunc(remove, func(r string) bool { return Key(r) == k }) {
			continue
		}
		seen[k] = true
		out = append(out, clean(name))
	}
	return out
}

func keys(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = Key(name)
	}
	return out
}
