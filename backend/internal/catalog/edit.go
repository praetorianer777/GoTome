package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// The fields of a book that have a source and may be locked, as stored in
// field_sources and locked_fields, beside FieldLanguage, FieldPublished and
// FieldSeries, which the filters share. The series covers the position in it.
const (
	FieldTitle        = "title"
	FieldSubtitle     = "subtitle"
	FieldDescription  = "description"
	FieldPublisher    = "publisher"
	FieldPageCount    = "pageCount"
	FieldContributors = "contributors"
	FieldTags         = "tags"
	FieldIdentifiers  = "identifiers"
	FieldCover        = "cover"
)

// LockableFields lists every field a lock may be put on.
var LockableFields = []string{
	FieldTitle, FieldSubtitle, FieldDescription, FieldLanguage, FieldPublished,
	FieldPublisher, FieldSeries, FieldPageCount, FieldContributors, FieldTags,
	FieldIdentifiers, FieldCover,
}

// maxDescription bounds a description in characters; a provider's longest
// are a few thousand.
const maxDescription = 50_000

// Edit is a change a person makes to how a book is described. A nil field is
// left as it is; an empty one removes the value. Every field the edit sets is
// recorded as typed by hand and locked against automatic updates, unless
// Locks says otherwise for it. Locks also locks or unlocks fields the edit
// does not set.
type Edit struct {
	Title       *string
	Subtitle    *string
	Description *string
	// Language is a BCP 47 tag such as "en" or "de-AT".
	Language *string
	// Published is "2010", "2010-08" or "2010-08-31".
	Published *string
	Publisher *string
	Series    *SeriesPlace
	// PageCount 0 removes the count.
	PageCount    *int32
	Contributors *[]NewContributor
	Tags         *[]string
	// Identifiers replace the book's own; those its files carry stay. Type is
	// a scheme NormalizeIdentifier knows, such as isbn.
	Identifiers *[]Identifier
	// Cover is a key in the cover store.
	Cover *string
	Locks map[string]bool
}

// SeriesPlace is a series and the book's position in it.
type SeriesPlace struct {
	// Name "" takes the book out of its series.
	Name  string
	Index *float64
}

// EditError says, per field, what is wrong with an edit.
type EditError map[string]string

func (e EditError) Error() string {
	return "the edit is not valid: " + strings.Join(slices.Sorted(maps.Keys(e)), ", ")
}

// check finds what is wrong with the edit, and brings what it can into the
// form it is stored in.
func (e *Edit) check() error {
	problems := EditError{}
	if e.Title != nil && clean(*e.Title) == "" {
		problems[FieldTitle] = "A book needs a title."
	}
	if e.Description != nil && len([]rune(*e.Description)) > maxDescription {
		problems[FieldDescription] = fmt.Sprintf("A description may be at most %d characters long.", maxDescription)
	}
	if e.Language != nil && strings.TrimSpace(*e.Language) != "" {
		tag, err := language.Parse(strings.TrimSpace(*e.Language))
		if err != nil {
			problems[FieldLanguage] = "Give a language code such as en, de or pt-BR."
		} else {
			canonical := tag.String()
			e.Language = &canonical
		}
	}
	if e.Published != nil && strings.TrimSpace(*e.Published) != "" {
		if _, _, ok := ParsePublished(*e.Published); !ok {
			problems[FieldPublished] = "Give the date as a year, year and month, or a whole date: 2010, 2010-08 or 2010-08-31."
		}
	}
	if e.Series != nil && e.Series.Index != nil && *e.Series.Index < 0 {
		problems[FieldSeries] = "The position in a series cannot be below zero."
	}
	if e.PageCount != nil && *e.PageCount < 0 {
		problems[FieldPageCount] = "The number of pages cannot be below zero."
	}
	if e.Contributors != nil {
		for _, c := range *e.Contributors {
			if !slices.Contains([]string{RoleAuthor, RoleNarrator, RoleTranslator, RoleEditor, RoleIllustrator}, c.Role) {
				problems[FieldContributors] = "A contributor's role is author, narrator, translator, editor or illustrator."
			}
		}
	}
	if e.Identifiers != nil {
		var normalized []Identifier
		for _, ident := range *e.Identifiers {
			id, ok := NormalizeIdentifier(ident.Type, ident.Value)
			if !ok {
				problems[FieldIdentifiers] = fmt.Sprintf("%q is not a valid %s.", ident.Value, strings.ToUpper(ident.Type))
				continue
			}
			normalized = append(normalized, id)
		}
		e.Identifiers = &normalized
	}
	for field := range e.Locks {
		if !slices.Contains(LockableFields, field) {
			problems["locks"] = fmt.Sprintf("There is no field %q to lock.", field)
		}
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}

// ChangesValues reports whether the edit sets any field, rather than only
// putting locks on or taking them off.
func (e Edit) ChangesValues() bool {
	return e.Title != nil || e.Subtitle != nil || e.Description != nil || e.Language != nil ||
		e.Published != nil || e.Publisher != nil || e.Series != nil || e.PageCount != nil ||
		e.Contributors != nil || e.Tags != nil || e.Identifiers != nil || e.Cover != nil
}

// EditTx changes how a book the scope may see is described, inside a
// transaction the caller runs, or returns ErrNotFound or an EditError.
func EditTx(ctx context.Context, tx pgx.Tx, scope library.Scope, id uuid.UUID, e Edit) error {
	if err := e.check(); err != nil {
		return err
	}
	q := sqlc.New(tx)
	book, err := q.LockVisibleBook(ctx, sqlc.LockVisibleBookParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	sources := map[string]string{}
	if err := json.Unmarshal(book.FieldSources, &sources); err != nil {
		return fmt.Errorf("field sources of book %s: %w", book.ID, err)
	}
	locked := map[string]bool{}
	for _, field := range book.LockedFields {
		locked[field] = true
	}
	typed := func(field string) {
		sources[field] = SourceManual
		locked[field] = true
	}

	update := describedAs(book)
	text := func(field string, value *string, current **string) {
		if value != nil {
			*current = optional(clean(*value))
			typed(field)
		}
	}
	text(FieldSubtitle, e.Subtitle, &update.Subtitle)
	text(FieldLanguage, e.Language, &update.Language)
	text(FieldCover, e.Cover, &update.CoverKey)
	if e.Description != nil {
		update.Description = optional(*e.Description)
		typed(FieldDescription)
	}
	if e.Title != nil {
		update.Title = clean(*e.Title)
		update.TitleKey = Key(update.Title)
		typed(FieldTitle)
	}
	update.SortTitle = SortTitle(update.Title, deref(update.Language))
	if e.Published != nil {
		update.PublishedOn, update.PublishedPrecision = nil, nil
		if date, precision, ok := ParsePublished(*e.Published); ok {
			update.PublishedOn, update.PublishedPrecision = &date, &precision
		}
		typed(FieldPublished)
	}
	if e.Publisher != nil {
		update.PublisherID = nil
		if name := clean(*e.Publisher); Key(name) != "" {
			publisher, err := q.UpsertPublisher(ctx, sqlc.UpsertPublisherParams{Name: name, NameKey: Key(name)})
			if err != nil {
				return fmt.Errorf("publisher: %w", err)
			}
			update.PublisherID = &publisher.ID
		}
		typed(FieldPublisher)
	}
	if e.Series != nil {
		update.SeriesID, update.SeriesIndex = nil, nil
		if name := clean(e.Series.Name); Key(name) != "" {
			series, err := q.UpsertSeries(ctx, sqlc.UpsertSeriesParams{Name: name, NameKey: Key(name)})
			if err != nil {
				return fmt.Errorf("series: %w", err)
			}
			update.SeriesID, update.SeriesIndex = &series.ID, e.Series.Index
		}
		typed(FieldSeries)
	}
	if e.PageCount != nil {
		update.PageCount = nil
		if *e.PageCount > 0 {
			update.PageCount = e.PageCount
		}
		typed(FieldPageCount)
	}
	if e.Contributors != nil {
		if err := q.DeleteBookContributors(ctx, book.ID); err != nil {
			return err
		}
		if err := addContributors(ctx, q, book.ID, *e.Contributors); err != nil {
			return err
		}
		if err := q.RefreshAuthorSort(ctx, book.ID); err != nil {
			return err
		}
		typed(FieldContributors)
	}
	if e.Tags != nil {
		if err := q.DeleteBookTags(ctx, book.ID); err != nil {
			return err
		}
		if err := addTags(ctx, q, book.ID, *e.Tags); err != nil {
			return err
		}
		typed(FieldTags)
	}
	if e.Identifiers != nil {
		if err := q.DeleteBookOwnIdentifiers(ctx, book.ID); err != nil {
			return err
		}
		for _, ident := range *e.Identifiers {
			err := q.AddBookIdentifier(ctx, sqlc.AddBookIdentifierParams{BookID: book.ID, Type: ident.Type, Value: ident.Value})
			if err != nil {
				return fmt.Errorf("identifier %s: %w", ident.Type, err)
			}
		}
		typed(FieldIdentifiers)
	}

	maps.Copy(locked, e.Locks)
	if update.FieldSources, err = json.Marshal(sources); err != nil {
		return err
	}
	if err := q.UpdateBookDescribed(ctx, update); err != nil {
		return err
	}
	var locks []string
	for field, on := range locked {
		if on {
			locks = append(locks, field)
		}
	}
	slices.Sort(locks)
	return q.SetBookLocks(ctx, sqlc.SetBookLocksParams{ID: book.ID, LockedFields: append([]string{}, locks...)})
}

// describedAs is the update that leaves the book as it is.
func describedAs(book sqlc.Book) sqlc.UpdateBookDescribedParams {
	return sqlc.UpdateBookDescribedParams{
		ID: book.ID, Title: book.Title, SortTitle: book.SortTitle, TitleKey: book.TitleKey,
		Subtitle: book.Subtitle, Description: book.Description, Language: book.Language,
		PublishedOn: book.PublishedOn, PublishedPrecision: book.PublishedPrecision,
		PublisherID: book.PublisherID, SeriesID: book.SeriesID, SeriesIndex: book.SeriesIndex,
		PageCount: book.PageCount, CoverKey: book.CoverKey,
	}
}

// Kinds of source, as Provenance reports them.
const (
	ProvenanceFile     = "file"
	ProvenanceFilename = "filename"
	ProvenanceManual   = "manual"
	ProvenanceProvider = "provider"
)

// Provenance reads a stored source: what kind it is and, for a file, the
// file's format, for a provider, the provider's name.
func Provenance(source string) (kind, detail string) {
	if rest, ok := strings.CutPrefix(source, "file:"); ok {
		format, _, _ := strings.Cut(rest, ":")
		return ProvenanceFile, format
	}
	if name, ok := strings.CutPrefix(source, "provider:"); ok {
		return ProvenanceProvider, name
	}
	return source, ""
}
