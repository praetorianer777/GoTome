//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// applyCleanup applies suggestions and works the bulk change through,
// returning its status.
func (a *app) applyCleanup(c *http.Client, req map[string]any) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/cleanup/apply", req)
	if status != http.StatusAccepted {
		a.t.Fatalf("apply %v: %d %v\n%s", req, status, body, a.logs.String())
	}
	id := uuid.MustParse(body["id"].(string))
	for {
		done, err := a.server.Bulk.Run(context.Background(), id)
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			break
		}
	}
	status, out, _ := a.call(c, http.MethodGet, "/bulk/"+id.String(), nil)
	if status != 200 || out["finishedAt"] == nil {
		a.t.Fatalf("status: %d %v", status, out)
	}
	return out
}

// suggestions lists the suggestions of a kind the client is offered.
func (a *app) suggestions(c *http.Client, kind string) (list []map[string]any, counts map[string]any) {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/cleanup?kind="+kind, nil)
	if status != 200 {
		a.t.Fatalf("list %s: %d %v", kind, status, body)
	}
	for _, s := range body["suggestions"].([]any) {
		list = append(list, s.(map[string]any))
	}
	return list, body["counts"].(map[string]any)
}

func found(s map[string]any) []string {
	var out []string
	for _, f := range s["found"].([]any) {
		n := f.(map[string]any)
		out = append(out, n["name"].(string))
	}
	return out
}

func TestTheSpellingsOfOneAuthorAreMergedIntoTheOneChosen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	open := a.library(admin, "Shelf", "shared")
	hidden := a.library(admin, "Hidden", "private")
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	for _, b := range []struct{ lib, title, author string }{
		{open, "Das letzte Evangelium", "Goldstein, Barbara"},
		{open, "Der vergessene Papst", "Goldstein, Barbara"},
		{open, "Die Kardinälin", "Barbara Goldstein"},
		{open, "Mort", "Terry Pratchett"},
		// A book of a library the editor does not see.
		{hidden, "Die Evangelistin", "Goldstein, Barbara"},
	} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: uuid.MustParse(b.lib), Title: b.title,
			Contributors: []catalog.NewContributor{{Name: b.author}, {Name: "Some Translator", Role: catalog.RoleTranslator}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[b.title] = id
	}

	if status, _, _ := a.call(reader, http.MethodGet, "/cleanup", nil); status != 403 {
		t.Errorf("a reader is offered clean-ups: %d", status)
	}
	list, counts := a.suggestions(editor, "authors")
	if len(list) != 1 || counts["authors"].(float64) != 1 {
		t.Fatalf("the editor's suggestions: %v %v", list, counts)
	}
	s := list[0]
	if got := found(s); !slices.Equal(got, []string{"Goldstein, Barbara", "Barbara Goldstein"}) {
		t.Errorf("the spellings the editor sees: %v", got)
	}
	if s["subject"] != "barbara goldstein" || s["fix"] != "merge" || s["suggested"] != "Goldstein, Barbara" || s["books"].(float64) != 3 {
		t.Errorf("the suggestion: %v", s)
	}
	if list, _ := a.suggestions(admin, "authors"); len(list) != 1 || list[0]["books"].(float64) != 4 {
		t.Errorf("the administrator, who sees every library, sees every book: %v", list)
	}

	if status, body, _ := a.call(editor, http.MethodPost, "/cleanup/apply", map[string]any{
		"kind": "authors", "picks": []map[string]string{{"subject": "barbara goldstein", "to": "Terry Pratchett"}},
	}); status != 422 {
		t.Errorf("another person's name as the spelling: %d %v", status, body)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/cleanup/apply", map[string]any{
		"kind": "authors", "picks": []map[string]string{{"subject": "terry pratchett"}},
	}); status != 404 {
		t.Errorf("a suggestion there is not: %d", status)
	}

	// The spelling the editor keeps is the less used one.
	status := a.applyCleanup(editor, map[string]any{
		"kind": "authors", "picks": []map[string]string{{"subject": "barbara goldstein", "to": "Barbara  Goldstein"}},
	})
	want := map[string]string{"Das letzte Evangelium": "changed", "Der vergessene Papst": "changed"}
	if got := outcomes(status); !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	for _, title := range []string{"Das letzte Evangelium", "Der vergessene Papst", "Die Kardinälin"} {
		book := a.book(ids[title])
		if !slices.Equal(book.Authors(), []string{"Barbara Goldstein"}) || len(book.Contributors) != 2 {
			t.Errorf("%s: %+v", title, book.Contributors)
		}
	}
	if !slices.Contains(a.book(ids["Das letzte Evangelium"]).Locked, catalog.FieldContributors) {
		t.Error("a merge is a person's edit, and locks the authors")
	}
	if got := a.book(ids["Die Evangelistin"]).Authors(); !slices.Equal(got, []string{"Goldstein, Barbara"}) {
		t.Errorf("the book the editor does not see was changed: %v", got)
	}
	if list, counts := a.suggestions(editor, "authors"); len(list) != 0 || counts["authors"].(float64) != 0 {
		t.Errorf("after the merge: %v %v", list, counts)
	}
	if list, _ := a.suggestions(admin, "authors"); len(list) != 1 || !slices.Equal(found(list[0]), []string{"Barbara Goldstein", "Goldstein, Barbara"}) {
		t.Errorf("the administrator's suggestion after the merge: %v", list)
	}
}

func TestPlaceholdersClashesAndTitlesAreSuggestedAppliedAndDismissed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	for _, b := range []catalog.NewBook{
		{Title: "Ohne Namen", Contributors: []catalog.NewContributor{{Name: "authors_sort"}, {Name: "Real Person"}}},
		{Title: "Nur ein Platzhalter", Contributors: []catalog.NewContributor{{Name: "Unknown"}}},
		{Title: "Anonyme Briefe", Contributors: []catalog.NewContributor{{Name: "Anonymous"}}},
		{Title: "Jerry Cotton 2913: Die beste Waffe", Series: "Jerry Cotton", Contributors: []catalog.NewContributor{{Name: "Jerry Cotton"}}},
		{Title: "Jerry Cotton 2914: Der Tod", Series: "Jerry Cotton", Contributors: []catalog.NewContributor{{Name: "Jerry Cotton"}}},
		{Title: "Todeskind", Series: "Karen Rose", Contributors: []catalog.NewContributor{{Name: "Karen Rose"}}},
		{Title: "Todesstoß", Contributors: []catalog.NewContributor{{Name: "Karen Rose"}}},
		{Title: "Das letzte Evangelium: Historischer Roman (German Edition)"},
		{Title: "Tod im Moor (Kindle Edition)"},
		{Title: "Sterbewohl: Kriminalroman"},
		{Title: "Eifel-Krimi"},
	} {
		b.LibraryID = lib
		id, err := books.CreateBook(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		ids[b.Title] = id
	}

	list, counts := a.suggestions(editor, "placeholders")
	if len(list) != 2 || list[0]["fix"] != "removeAuthor" {
		t.Fatalf("placeholders: %v", list)
	}
	if want := map[string]any{"authors": 0.0, "placeholders": 2.0, "clashes": 2.0, "titles": 3.0}; !sameMap(counts, want) {
		t.Errorf("counts %v, want %v", counts, want)
	}
	a.applyCleanup(editor, map[string]any{"kind": "placeholders"})
	if got := a.book(ids["Ohne Namen"]).Authors(); !slices.Equal(got, []string{"Real Person"}) {
		t.Errorf("authors_sort stayed: %v", got)
	}
	if got := a.book(ids["Nur ein Platzhalter"]).Authors(); len(got) != 0 {
		t.Errorf("Unknown stayed: %v", got)
	}
	if got := a.book(ids["Anonyme Briefe"]).Authors(); !slices.Equal(got, []string{"Anonymous"}) {
		t.Errorf("Anonymous is an author: %v", got)
	}

	list, _ = a.suggestions(editor, "clashes")
	fixes := map[string]string{}
	for _, s := range list {
		fixes[found(s)[0]] = s["fix"].(string)
	}
	if want := map[string]string{"Jerry Cotton": "removeAuthor", "Karen Rose": "clearSeries"}; !sameOutcomes(fixes, want) {
		t.Errorf("clashes %v, want %v", fixes, want)
	}
	a.applyCleanup(editor, map[string]any{"kind": "clashes"})
	if cotton := a.book(ids["Jerry Cotton 2913: Die beste Waffe"]); len(cotton.Authors()) != 0 || cotton.Series != "Jerry Cotton" {
		t.Errorf("Jerry Cotton is the series: %v %q", cotton.Authors(), cotton.Series)
	}
	if rose := a.book(ids["Todeskind"]); !slices.Equal(rose.Authors(), []string{"Karen Rose"}) || rose.Series != "" {
		t.Errorf("Karen Rose is the author: %v %q", rose.Authors(), rose.Series)
	}

	list, _ = a.suggestions(editor, "titles")
	suggested := map[string]string{}
	subjects := map[string]string{}
	for _, s := range list {
		suggested[found(s)[0]] = s["suggested"].(string)
		subjects[found(s)[0]] = s["subject"].(string)
	}
	want := map[string]string{
		"Das letzte Evangelium: Historischer Roman (German Edition)": "Das letzte Evangelium",
		"Tod im Moor (Kindle Edition)":                               "Tod im Moor",
		"Sterbewohl: Kriminalroman":                                  "Sterbewohl",
	}
	if !sameOutcomes(suggested, want) {
		t.Errorf("titles %v, want %v", suggested, want)
	}

	// Dismissed, a suggestion stays away while the title stays as it is.
	moor := subjects["Tod im Moor (Kindle Edition)"]
	if status, _, _ := a.call(editor, http.MethodPost, "/cleanup/dismiss", map[string]any{"kind": "titles", "subject": moor}); status != 204 {
		t.Fatalf("dismiss: %d", status)
	}
	if list, counts := a.suggestions(editor, "titles"); len(list) != 2 || counts["titles"].(float64) != 2 {
		t.Errorf("after dismissing: %v %v", list, counts)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/cleanup/dismiss", map[string]any{"kind": "titles", "subject": moor}); status != 404 {
		t.Errorf("a dismissed suggestion dismissed again: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/cleanup/dismiss", map[string]any{"kind": "colours", "subject": moor}); status != 422 {
		t.Errorf("a kind there is not: %d", status)
	}

	// A title locked by hand is left as it is.
	a.call(editor, http.MethodPatch, "/books/"+ids["Sterbewohl: Kriminalroman"].String(), map[string]any{"locks": map[string]bool{"title": true}})
	status := a.applyCleanup(editor, map[string]any{"kind": "titles"})
	if got, want := outcomes(status), map[string]string{
		"Das letzte Evangelium": "changed", "Sterbewohl: Kriminalroman": "locked title",
	}; !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	if got := a.book(ids["Tod im Moor (Kindle Edition)"]).Title; got != "Tod im Moor (Kindle Edition)" {
		t.Errorf("the dismissed title was changed: %q", got)
	}

	// Another title, and the suggestion is back.
	a.call(editor, http.MethodPatch, "/books/"+ids["Tod im Moor (Kindle Edition)"].String(), map[string]any{"title": "Tod im Moor (German Edition)"})
	if list, _ := a.suggestions(editor, "titles"); len(list) != 2 {
		t.Errorf("after the title changed: %v", list)
	}
}

func TestDuplicatesAndPlaceholdersMeetAcrossTheOrderOfAnAuthorsName(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	var ids []uuid.UUID
	for _, author := range []string{"Goldstein, Barbara", "Barbara Goldstein"} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: lib, Title: "Die Kardinälin", Contributors: []catalog.NewContributor{{Name: author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := a.server.Duplicates.Check(context.Background(), ids[1]); err != nil {
		t.Fatal(err)
	}
	var evidence string
	if err := a.pool.QueryRow(context.Background(), `SELECT string_agg(kind, ',') FROM duplicate_evidence`).Scan(&evidence); err != nil || evidence != "title_author" {
		t.Errorf("evidence: %q %v", evidence, err)
	}

	// A wish by one spelling is fulfilled by a file that names the other.
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET placeholder = true WHERE id = $1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	wish, err := sqlc.New(a.pool).FindPlaceholder(context.Background(), sqlc.FindPlaceholderParams{
		LibraryID: lib, Isbns: []string{}, TitleKey: catalog.Key("Die Kardinälin"), AuthorKeys: []string{catalog.Key("Barbara Goldstein")},
	})
	if err != nil || wish != ids[0] {
		t.Errorf("the wish: %v %v", wish, err)
	}

	// Go's PersonKey and SQL's person_key are one rule.
	for _, name := range []string{"Goldstein, Barbara", "Horváth, Ödön von", "J.R.R. Tolkien", "李 白", "Zoë  O'Neil-Smith", "a b A"} {
		var sql string
		if err := a.pool.QueryRow(context.Background(), `SELECT person_key($1)`, catalog.Key(name)).Scan(&sql); err != nil {
			t.Fatal(err)
		}
		if got := catalog.PersonKey(name); got != sql {
			t.Errorf("%q: Go %q, SQL %q", name, got, sql)
		}
	}
}

func TestTitleAdditionsAreOneRuleInGoAndSQL(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	for _, title := range []string{
		"Das letzte Evangelium: Historischer Roman (German Edition)", "Rauklands Sohn: Raukland Trilogie (German Edition)",
		"Der Schwarm - Roman", "Die Abrechnung -Thriller", "Sterbewohl: KRIMINALROMAN", "Das Original - Psycho-Thriller",
		"The Road: A Novel", "Tod im Moor (Kindle Edition)", "Kurz und gut – Erzählungen", "Ein Sommer – Erzählung",
		"Eifel-Krimi", "Mordsspaß-Krimi", "Der Roman", "Die Legenden der Albae: Die Vergessenen",
		"Sommerhaus, später: Erzählungen (Gesamtausgabe)", "Romane und Erzählungen", "Ödland",
	} {
		var sql bool
		if err := a.pool.QueryRow(context.Background(), `SELECT title_has_addition($1)`, title).Scan(&sql); err != nil {
			t.Fatal(err)
		}
		if goes := cleanup.CleanTitle(title) != title; goes != sql {
			t.Errorf("%q: Go takes something off: %v, SQL finds an addition: %v", title, goes, sql)
		}
	}
}

func sameMap(got, want map[string]any) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
