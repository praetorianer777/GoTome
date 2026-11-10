package curation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Run is a run as the list shows it. Its counts are of the actions in
// libraries the viewer sees.
type Run struct {
	ID            uuid.UUID
	Kind          string
	Rule          string
	StartedBy     *uuid.UUID
	StartedByName string
	DryRun        bool
	Automatic     bool
	State         string
	CreatedAt     time.Time
	FinishedAt    *time.Time
	ConfirmedAt   *time.Time
	UndoneAt      *time.Time
	Actions       int
	Undone        int
	// Skipped counts the pairs a duplicates run left for a person.
	Skipped int
	// Undoing is set while the run's job undoes it.
	Undoing bool
}

// FieldChange is one detail of a book as an action left it.
type FieldChange struct {
	Field  string
	Before string
	After  string
}

// BookChange is what an action changed about one of its books.
type BookChange struct {
	ID     uuid.UUID
	Title  string
	Fields []FieldChange
}

// Action is an action as the run's page shows it.
type Action struct {
	ID           uuid.UUID
	RunID        uuid.UUID
	Kind         string
	Rule         string
	Reason       string
	Books        []BookChange
	TrashedFiles int
	State        string
	Note         string
	CreatedAt    time.Time
	UndoneAt     *time.Time
}

const cursorTime = time.RFC3339Nano

// Page bounds.
const (
	DefaultPage = 50
	MaxPage     = 200
)

// ErrCursor is a cursor that this list did not hand out.
var ErrCursor = errors.New("not a cursor")

func parseCursor(c string) (*time.Time, *uuid.UUID, error) {
	if c == "" {
		return nil, nil, nil
	}
	at, id, ok := strings.Cut(c, "_")
	if !ok {
		return nil, nil, ErrCursor
	}
	t, err := time.Parse(cursorTime, at)
	if err != nil {
		return nil, nil, ErrCursor
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, nil, ErrCursor
	}
	return &t, &u, nil
}

func makeCursor(t time.Time, id uuid.UUID) string {
	return t.UTC().Format(cursorTime) + "_" + id.String()
}

// RunFilter narrows the list of runs.
type RunFilter struct {
	Kind  string
	State string
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Runs lists the runs the scope sees, the newest first, with the cursor of
// the page after, empty at the end.
func (s *Service) Runs(ctx context.Context, scope library.Scope, f RunFilter, limit int, cursor string) ([]Run, string, error) {
	at, id, err := parseCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	limit = min(max(limit, 1), MaxPage)
	rows, err := sqlc.New(s.pool).ListCurationRuns(ctx, sqlc.ListCurationRunsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Kind: optional(f.Kind), State: optional(f.State),
		AfterAt: at, AfterID: id, MaxRows: int32(limit + 1),
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = makeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	out := make([]Run, len(rows))
	for i, r := range rows {
		out[i] = Run{
			ID: r.ID, Kind: r.Kind, Rule: deref(r.Rule), StartedBy: r.StartedBy, StartedByName: deref(r.StartedByName),
			DryRun: r.DryRun, Automatic: r.Automatic, State: r.State, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
			ConfirmedAt: r.ConfirmedAt, UndoneAt: r.UndoneAt, Actions: int(r.Actions), Undone: int(r.Undone), Skipped: int(r.Skipped), Undoing: r.Undoing,
		}
	}
	return out, next, nil
}

// GetRun returns a run the scope sees, with a page of its actions the scope
// sees and the cursor of the next, empty at the end.
func (s *Service) GetRun(ctx context.Context, scope library.Scope, id uuid.UUID, limit int, cursor string) (Run, []Action, string, error) {
	at, aid, err := parseCursor(cursor)
	if err != nil {
		return Run{}, nil, "", err
	}
	q := sqlc.New(s.pool)
	r, err := q.GetCurationRun(ctx, sqlc.GetCurationRunParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, nil, "", ErrNotFound
	}
	if err != nil {
		return Run{}, nil, "", err
	}
	run := Run{
		ID: r.ID, Kind: r.Kind, Rule: deref(r.Rule), StartedBy: r.StartedBy, StartedByName: deref(r.StartedByName),
		DryRun: r.DryRun, Automatic: r.Automatic, State: r.State, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		ConfirmedAt: r.ConfirmedAt, UndoneAt: r.UndoneAt, Actions: int(r.Actions), Undone: int(r.Undone), Skipped: int(r.Skipped), Undoing: r.Undoing,
	}
	limit = min(max(limit, 1), MaxPage)
	rows, err := q.ListCurationActions(ctx, sqlc.ListCurationActionsParams{
		RunID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll, AfterAt: at, AfterID: aid, MaxRows: int32(limit + 1),
	})
	if err != nil {
		return Run{}, nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = makeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	actions, err := s.actionsOf(ctx, rows)
	return run, actions, next, err
}

// actionsOf works out what each action changed, with the books' titles.
func (s *Service) actionsOf(ctx context.Context, rows []sqlc.ListCurationActionsRow) ([]Action, error) {
	q := sqlc.New(s.pool)
	var books, named []uuid.UUID
	type pair struct{ before, after Snapshot }
	snaps := make([]pair, len(rows))
	for i, r := range rows {
		books = append(books, r.Books...)
		if err := jsonUnmarshal(r.Before, &snaps[i].before); err != nil {
			return nil, err
		}
		if err := jsonUnmarshal(r.After, &snaps[i].after); err != nil {
			return nil, err
		}
		for _, snap := range []Snapshot{snaps[i].before, snaps[i].after} {
			ids, err := namedIDs(snap)
			if err != nil {
				return nil, err
			}
			named = append(named, ids...)
		}
	}
	titles := map[uuid.UUID]string{}
	ts, err := q.ListCurationTitles(ctx, books)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		titles[t.ID] = t.Title
	}
	names := map[uuid.UUID]string{}
	ns, err := q.ListCurationNames(ctx, named)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		names[n.ID] = n.Name
	}
	out := make([]Action, len(rows))
	for i, r := range rows {
		a := Action{
			ID: r.ID, RunID: r.RunID, Kind: r.Kind, Rule: deref(r.Rule), Reason: r.Reason, TrashedFiles: len(r.TrashedFiles),
			State: r.State, Note: r.Note, CreatedAt: r.CreatedAt, UndoneAt: r.UndoneAt,
		}
		for _, b := range r.Books {
			bc := BookChange{ID: b, Title: titles[b]}
			bc.Fields, err = Changes(snaps[i].before, snaps[i].after, b, names, titles)
			if err != nil {
				return nil, err
			}
			a.Books = append(a.Books, bc)
		}
		out[i] = a
	}
	return out, nil
}

// namedIDs are the IDs in a snapshot that stand for a name.
func namedIDs(s Snapshot) ([]uuid.UUID, error) {
	var out []uuid.UUID
	add := func(v any) {
		if str, ok := v.(string); ok {
			if id, err := uuid.Parse(str); err == nil {
				out = append(out, id)
			}
		}
	}
	for table, cols := range map[string][]string{
		"books": {"publisher_id", "series_id"}, "book_contributors": {"author_id"}, "book_tags": {"tag_id"},
	} {
		if _, ok := s[table]; !ok {
			continue
		}
		rows, err := s.rows(table)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			for _, c := range cols {
				add(r[c])
			}
		}
	}
	return out, nil
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fmt.Sprint(x)
	case bool:
		if x {
			return "yes"
		}
		return ""
	}
	return fmt.Sprint(v)
}

// Changes lists the details of the book that differ between two snapshots:
// the fields a person sees, the credits, the tags and the number of files.
func Changes(before, after Snapshot, book uuid.UUID, names, titles map[uuid.UUID]string) ([]FieldChange, error) {
	b, err := detailsOf(before, book, names, titles)
	if err != nil {
		return nil, err
	}
	a, err := detailsOf(after, book, names, titles)
	if err != nil {
		return nil, err
	}
	var out []FieldChange
	for _, f := range detailFields {
		if b[f] != a[f] {
			out = append(out, FieldChange{Field: f, Before: b[f], After: a[f]})
		}
	}
	return out, nil
}

var detailFields = []string{
	"title", "subtitle", "description", "language", "published", "publisher", "series", "seriesIndex", "pageCount",
	"cover", "contributors", "tags", "files", "mergedInto", "deleted",
}

func detailsOf(s Snapshot, book uuid.UUID, names, titles map[uuid.UUID]string) (map[string]string, error) {
	out := map[string]string{}
	id := book.String()
	nameOf := func(v any) string {
		u, err := uuid.Parse(text(v))
		if err != nil {
			return ""
		}
		return names[u]
	}
	if _, ok := s["books"]; ok {
		rows, err := s.rows("books")
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r["id"] != id {
				continue
			}
			published := text(r["published_on"])
			if published != "" && text(r["published_precision"]) != "" {
				published += " (" + text(r["published_precision"]) + ")"
			}
			merged := ""
			if u, err := uuid.Parse(text(r["merged_into_id"])); err == nil {
				merged = titles[u]
			}
			deleted := ""
			if r["deleted_at"] != nil {
				deleted = "yes"
			}
			for k, v := range map[string]string{
				"title": text(r["title"]), "subtitle": text(r["subtitle"]), "description": text(r["description"]),
				"language": text(r["language"]), "published": published, "publisher": nameOf(r["publisher_id"]),
				"series": nameOf(r["series_id"]), "seriesIndex": text(r["series_index"]), "pageCount": text(r["page_count"]),
				"cover": text(r["cover_key"]), "mergedInto": merged, "deleted": deleted,
			} {
				out[k] = v
			}
		}
	}
	if _, ok := s["book_contributors"]; ok {
		rows, err := s.rows("book_contributors")
		if err != nil {
			return nil, err
		}
		type credit struct {
			position float64
			text     string
		}
		var credits []credit
		for _, r := range rows {
			if r["book_id"] != id {
				continue
			}
			position, _ := r["position"].(float64)
			label := nameOf(r["author_id"])
			if role := text(r["role"]); role != "author" {
				label += " (" + role + ")"
			}
			credits = append(credits, credit{position, label})
		}
		slices.SortStableFunc(credits, func(x, y credit) int {
			if x.position != y.position {
				if x.position < y.position {
					return -1
				}
				return 1
			}
			return strings.Compare(x.text, y.text)
		})
		labels := make([]string, len(credits))
		for i, c := range credits {
			labels[i] = c.text
		}
		out["contributors"] = strings.Join(labels, ", ")
	}
	if _, ok := s["book_tags"]; ok {
		rows, err := s.rows("book_tags")
		if err != nil {
			return nil, err
		}
		var tags []string
		for _, r := range rows {
			if r["book_id"] == id {
				tags = append(tags, nameOf(r["tag_id"]))
			}
		}
		slices.Sort(tags)
		out["tags"] = strings.Join(tags, ", ")
	}
	if _, ok := s["book_files"]; ok {
		rows, err := s.rows("book_files")
		if err != nil {
			return nil, err
		}
		n := 0
		for _, r := range rows {
			if r["book_id"] == id && r["trashed_at"] == nil {
				n++
			}
		}
		out["files"] = fmt.Sprint(n)
	}
	return out, nil
}
