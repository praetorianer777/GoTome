//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
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
		{Title: "Kommissar Falkner 2913: Die beste Waffe", Series: "Kommissar Falkner", Contributors: []catalog.NewContributor{{Name: "Kommissar Falkner"}}},
		{Title: "Kommissar Falkner 2914: Der Tod", Series: "Kommissar Falkner", Contributors: []catalog.NewContributor{{Name: "Kommissar Falkner"}}},
		{Title: "Todeskind", Series: "Ulrike Weidner", Contributors: []catalog.NewContributor{{Name: "Ulrike Weidner"}}},
		{Title: "Todesstoß", Contributors: []catalog.NewContributor{{Name: "Ulrike Weidner"}}},
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
	if want := map[string]any{"authors": 0.0, "placeholders": 2.0, "clashes": 2.0, "titles": 3.0, "series": 0.0}; !sameMap(counts, want) {
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
	if want := map[string]string{"Kommissar Falkner": "removeAuthor", "Ulrike Weidner": "choose"}; !sameOutcomes(fixes, want) {
		t.Errorf("clashes %v, want %v", fixes, want)
	}
	// Either fix of a clash takes something away: "apply all" leaves the
	// one that is to choose.
	a.applyCleanup(editor, map[string]any{"kind": "clashes"})
	if rose := a.book(ids["Todeskind"]); !slices.Equal(rose.Authors(), []string{"Ulrike Weidner"}) || rose.Series != "Ulrike Weidner" {
		t.Errorf("Ulrike Weidner was changed without a choice: %v %q", rose.Authors(), rose.Series)
	}
	a.applyCleanup(editor, map[string]any{
		"kind": "clashes", "picks": []map[string]string{{"subject": "ulrike weidner", "fix": "clearSeries"}},
	})
	if falkner := a.book(ids["Kommissar Falkner 2913: Die beste Waffe"]); len(falkner.Authors()) != 0 || falkner.Series != "Kommissar Falkner" {
		t.Errorf("Kommissar Falkner is the series: %v %q", falkner.Authors(), falkner.Series)
	}
	if rose := a.book(ids["Todeskind"]); !slices.Equal(rose.Authors(), []string{"Ulrike Weidner"}) || rose.Series != "" {
		t.Errorf("Ulrike Weidner is the author: %v %q", rose.Authors(), rose.Series)
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

func TestASeriesInTitlesIsSuggestedAppliedAndDismissed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	open := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	hidden := uuid.MustParse(a.library(admin, "Hidden", "private"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	add := func(lib uuid.UUID, title, series string) {
		t.Helper()
		id, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: title, Series: series})
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	add(open, "Rain Wild Chronicles 02 - Drachenkämpfer", "")
	add(open, "Rain Wild Chronicles 01 - Drachenhüter", "")
	add(open, "Drachenseele (Rain Wild Chronicles #3)", "")
	// Another book of the series, alone in a library the editor does not
	// see: alone, it is not taken for one.
	add(hidden, "Rain Wild Chronicles 04 - Drachenblut", "")
	// One title alone, but the series is known.
	add(open, "Die Zwerge", "Die Zwerge")
	add(open, "Die Zwerge, Band 2: Der Krieg der Zwerge", "")
	// Known only where the editor does not look.
	add(hidden, "Geheime Reihe", "Geheime Reihe")
	add(open, "Geheime Reihe 1 - Das Erbe", "")
	// Alone, and no series of the name: titles, not series.
	add(open, "Fahrenheit 451 - Ein Klassiker", "")
	add(open, "Es - Teil 1 - Der Club der Verlierer", "")
	add(open, "1984 - Roman", "")
	// In a series already.
	add(open, "Die Zwerge 3 - Die Rache der Zwerge", "Die Zwerge")

	list, counts := a.suggestions(editor, "series")
	if counts["series"].(float64) != 2 || len(list) != 2 {
		t.Fatalf("the editor's suggestions: %v %v", list, counts)
	}
	rain, zwerge := list[0], list[1]
	if rain["subject"] != "rain wild chronicles" || rain["fix"] != "setSeries" || rain["suggested"] != "Rain Wild Chronicles" || rain["books"].(float64) != 3 {
		t.Errorf("the series: %v", rain)
	}
	if got, want := found(rain), []string{
		"Rain Wild Chronicles 01 - Drachenhüter", "Rain Wild Chronicles 02 - Drachenkämpfer", "Drachenseele (Rain Wild Chronicles #3)",
	}; !slices.Equal(got, want) {
		t.Errorf("the titles in their order: %v, want %v", got, want)
	}
	first := rain["found"].([]any)[0].(map[string]any)
	if first["book"] != ids["Rain Wild Chronicles 01 - Drachenhüter"].String() || first["position"] != 1.0 || first["title"] != "Drachenhüter" {
		t.Errorf("what a title says: %v", first)
	}
	if zwerge["subject"] != "die zwerge" || !slices.Equal(found(zwerge), []string{"Die Zwerge, Band 2: Der Krieg der Zwerge"}) {
		t.Errorf("the known series: %v", zwerge)
	}
	if list, _ := a.suggestions(admin, "series"); len(list) != 3 || list[0]["books"].(float64) != 3 || list[2]["subject"] != "geheime reihe" {
		t.Errorf("the administrator, who sees the series of the hidden library: %v", list)
	}

	// Dismissed, a suggestion stays away until another book holds the series.
	if status, _, _ := a.call(editor, http.MethodPost, "/cleanup/dismiss", map[string]any{"kind": "series", "subject": "die zwerge"}); status != 204 {
		t.Fatalf("dismiss: %d", status)
	}
	if list, _ := a.suggestions(editor, "series"); len(list) != 1 {
		t.Errorf("after dismissing: %v", list)
	}
	add(open, "Die Zwerge, Band 4: Das Schicksal der Zwerge", "")
	if list, _ := a.suggestions(editor, "series"); len(list) != 2 {
		t.Errorf("after another book came: %v", list)
	}

	// A title locked by hand stays as it is; the book still takes the series.
	a.call(editor, http.MethodPatch, "/books/"+ids["Drachenseele (Rain Wild Chronicles #3)"].String(), map[string]any{"locks": map[string]bool{"title": true}})
	status := a.applyCleanup(editor, map[string]any{"kind": "series", "picks": []map[string]string{{"subject": "rain wild chronicles"}}})
	if got, want := outcomes(status), map[string]string{
		"Drachenhüter": "changed", "Drachenkämpfer": "changed", "Drachenseele (Rain Wild Chronicles #3)": "changed title",
	}; !sameOutcomes(got, want) {
		t.Errorf("outcomes %v, want %v", got, want)
	}
	for title, want := range map[string]struct {
		title string
		index float64
	}{
		"Rain Wild Chronicles 01 - Drachenhüter":   {"Drachenhüter", 1},
		"Rain Wild Chronicles 02 - Drachenkämpfer": {"Drachenkämpfer", 2},
		"Drachenseele (Rain Wild Chronicles #3)":   {"Drachenseele (Rain Wild Chronicles #3)", 3},
	} {
		book := a.book(ids[title])
		if book.Title != want.title || book.Series != "Rain Wild Chronicles" || book.SeriesIndex == nil || *book.SeriesIndex != want.index {
			t.Errorf("%s: %q in %q at %v", title, book.Title, book.Series, book.SeriesIndex)
		}
		if !slices.Contains(book.Locked, catalog.FieldSeries) {
			t.Errorf("%s: the series is a person's edit, and locked", title)
		}
	}
	if got := a.book(ids["Rain Wild Chronicles 04 - Drachenblut"]); got.Series != "" {
		t.Errorf("the book the editor does not see was changed: %q", got.Series)
	}
	if list, _ := a.suggestions(editor, "series"); len(list) != 1 || list[0]["subject"] != "die zwerge" {
		t.Errorf("after applying: %v", list)
	}
	// The book of the hidden library is in a known series now.
	if list, _ := a.suggestions(admin, "series"); len(list) != 3 || list[0]["subject"] != "die zwerge" || list[1]["subject"] != "geheime reihe" || list[2]["subject"] != "rain wild chronicles" {
		t.Errorf("the administrator's suggestions after applying: %v", list)
	}
}

func TestASeriesInATitleIsOneRuleInGoAndSQL(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	for _, title := range []string{
		"Rain Wild Chronicles 01 - Drachenhüter", "Rain Wild Chronicles 2 – Drachenkämpfer", "Wheel of Time #2: The Great Hunt",
		"Die Zwerge, Band 3: Das Schicksal der Zwerge", "Die Zwerge Bd. 4 - Die Rache der Zwerge", "Hauptmann Brandt - 2500 - Projekt Saturn",
		"Kommissar Falkner 2913: Die beste Waffe", "Star Trek - Deep Space Nine 12 - Der Weg", "Die drei ??? Folge 125 - Feuermond",
		"Der Herr der Ringe (1) - Die Gefährten", "The Expanse Book 1.5: The Butcher of Anderson", "Drachenhüter (Rain Wild Chronicles #1)",
		"Die Gefährten (Der Herr der Ringe, Band 1)", "Ödland (Die Ödland-Chroniken Teil 2)", "Mord (in Paris) (Kommissar Dupin Nr. 3)",
		"Es - Teil 1 - Der Club der Verlierer", "Fahrenheit 451 - Ein Roman", "Übel 3 - Ärger", "ÖDLAND 2 - Straße",
		"1984 - Roman", "2001 - Odyssee im Weltraum", "Berlin 1945 - Das Ende", "Catch22 - Der Roman", "Krieg und Frieden - Teil 1",
		"Krieg und Frieden, Teil 1", "Teil 2 - Die Rückkehr", "Band 3: Das Ende", "Der Hobbit (Teil 1)", "Drachenhüter (Band 1)",
		"Die 39 Stufen", "Rain Wild Chronicles 01 Drachenhüter", "Das letzte Evangelium: Historischer Roman (German Edition)",
		"Sterbewohl: Kriminalroman", "Die Legenden der Albae: Die Vergessenen", "Ocean's 11 - ", "A 1 - 2", "",
		"  Rain Wild Chronicles 01 - Drachenhüter  ", "Saga 1 - A - Saga 2 - B", "X (Y #1) (Z #2)",
	} {
		var sql, want *string
		place, ok := cleanup.SeriesInTitle(title)
		if err := a.pool.QueryRow(context.Background(), `SELECT title_series_key($1), CASE WHEN $2 THEN loose_key($3) END`,
			title, ok, place.Series).Scan(&sql, &want); err != nil {
			t.Fatal(err)
		}
		if (sql == nil) != (want == nil) || (sql != nil && *sql != *want) {
			t.Errorf("%q: Go reads %+v %v, SQL %v", title, place, ok, deref(sql))
		}
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// preview asks what a suggestion does to its books.
func (a *app) preview(c *http.Client, query string) (int, map[string]any) {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/cleanup/preview?"+query, nil)
	return status, body
}

func previewBooks(body map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, b := range body["books"].([]any) {
		book := b.(map[string]any)
		out[book["title"].(string)] = book
	}
	return out
}

func previewFields(book map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, f := range book["fields"].([]any) {
		field := f.(map[string]any)
		out[field["name"].(string)] = field
	}
	return out
}

func TestAPreviewShowsEachBookBeforeAndAfterAsApplyDoesIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	open := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	hidden := uuid.MustParse(a.library(admin, "Hidden", "private"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	for _, b := range []catalog.NewBook{
		{LibraryID: open, Title: "Das letzte Evangelium", Contributors: []catalog.NewContributor{{Name: "Goldstein, Barbara"}}},
		{LibraryID: open, Title: "Der vergessene Papst", Contributors: []catalog.NewContributor{{Name: "Goldstein, Barbara"}}},
		{LibraryID: open, Title: "Die Kardinälin", Contributors: []catalog.NewContributor{{Name: "Barbara Goldstein"}}},
		{LibraryID: open, Title: "Die Gesperrte", Contributors: []catalog.NewContributor{{Name: "Barbara Goldstein"}}},
		{LibraryID: hidden, Title: "Die Evangelistin", Contributors: []catalog.NewContributor{{Name: "Barbara Goldstein"}}},
		{LibraryID: open, Title: "Ohne Namen", Contributors: []catalog.NewContributor{{Name: "authors_sort"}}},
		{LibraryID: open, Title: "Kommissar Falkner 2913: Die beste Waffe", Series: "Kommissar Falkner", Contributors: []catalog.NewContributor{{Name: "Kommissar Falkner"}}},
		{LibraryID: open, Title: "Todeskind", Series: "Ulrike Weidner", Contributors: []catalog.NewContributor{{Name: "Ulrike Weidner"}}},
		{LibraryID: open, Title: "Todesstoß", Contributors: []catalog.NewContributor{{Name: "Ulrike Weidner"}}},
		{LibraryID: open, Title: "Tod im Moor (Kindle Edition)"},
		{LibraryID: open, Title: "Rain Wild Chronicles 01 - Drachenhüter"},
		{LibraryID: open, Title: "Rain Wild Chronicles 02 - Drachenkämpfer"},
	} {
		id, err := books.CreateBook(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		ids[b.Title] = id
	}
	a.call(editor, http.MethodPatch, "/books/"+ids["Die Gesperrte"].String(), map[string]any{"locks": map[string]bool{"contributors": true}})

	if status, _ := a.preview(reader, "kind=authors&subject=barbara+goldstein"); status != 403 {
		t.Errorf("a reader previews: %d", status)
	}
	if status, _ := a.preview(editor, "kind=authors&subject=terry+pratchett"); status != 404 {
		t.Errorf("a suggestion there is not: %d", status)
	}
	if status, _ := a.preview(editor, "kind=colours&subject=x"); status != 422 {
		t.Errorf("a kind there is not: %d", status)
	}

	status, body := a.preview(editor, "kind=authors&subject=barbara+goldstein&to=Goldstein,+Barbara")
	if status != 200 || body["total"].(float64) != 2 {
		t.Fatalf("the preview: %d %v", status, body)
	}
	shown := previewBooks(body)
	if _, ok := shown["Die Evangelistin"]; ok {
		t.Error("a book of a library the editor does not see is previewed")
	}
	moved := previewFields(shown["Die Kardinälin"])["contributors"]
	if moved["before"] != "Barbara Goldstein" || moved["after"] != "Goldstein, Barbara" || moved["locked"] != nil {
		t.Errorf("the change of a book: %v", moved)
	}
	if kept := previewFields(shown["Die Gesperrte"])["contributors"]; kept["locked"] != true || kept["after"] != "Goldstein, Barbara" {
		t.Errorf("a locked book stays: %v", kept)
	}
	_, body = a.preview(editor, "kind=authors&subject=barbara+goldstein&to=Goldstein,+Barbara&limit=1")
	if len(body["books"].([]any)) != 1 || body["total"].(float64) != 2 {
		t.Errorf("a page of the preview: %v", body)
	}
	_, body = a.preview(editor, "kind=authors&subject=barbara+goldstein&to=Goldstein,+Barbara&offset=1&limit=5")
	if len(body["books"].([]any)) != 1 {
		t.Errorf("the rest of the preview: %v", body)
	}

	// What the preview says is what apply does.
	a.applyCleanup(editor, map[string]any{
		"kind": "authors", "picks": []map[string]string{{"subject": "barbara goldstein", "to": "Goldstein, Barbara"}},
	})
	if got := a.book(ids["Die Kardinälin"]).Authors(); !slices.Equal(got, []string{"Goldstein, Barbara"}) {
		t.Errorf("after applying: %v", got)
	}
	if got := a.book(ids["Die Gesperrte"]).Authors(); !slices.Equal(got, []string{"Barbara Goldstein"}) {
		t.Errorf("the locked book changed: %v", got)
	}

	_, body = a.preview(editor, "kind=placeholders&subject=authors+sort")
	if f := previewFields(previewBooks(body)["Ohne Namen"])["contributors"]; f["before"] != "authors_sort" || f["after"] != "" {
		t.Errorf("a placeholder author: %v", f)
	}
	_, body = a.preview(editor, "kind=clashes&fix=clearSeries&subject=ulrike+weidner")
	if f := previewFields(previewBooks(body)["Todeskind"])["series"]; f["before"] != "Ulrike Weidner" || f["after"] != "" {
		t.Errorf("a series cleared: %v", f)
	}
	_, body = a.preview(editor, "kind=clashes&subject=kommissar+falkner")
	if f := previewFields(previewBooks(body)["Kommissar Falkner 2913: Die beste Waffe"])["contributors"]; f["before"] != "Kommissar Falkner" || f["after"] != "" {
		t.Errorf("an author removed: %v", f)
	}
	_, body = a.preview(editor, "kind=titles&subject="+ids["Tod im Moor (Kindle Edition)"].String())
	if f := previewFields(previewBooks(body)["Tod im Moor (Kindle Edition)"])["title"]; f["after"] != "Tod im Moor" {
		t.Errorf("a title: %v", f)
	}
	_, body = a.preview(editor, "kind=series&subject=rain+wild+chronicles")
	drachen := previewFields(previewBooks(body)["Rain Wild Chronicles 01 - Drachenhüter"])
	if drachen["series"]["after"] != "Rain Wild Chronicles #1" || drachen["title"]["after"] != "Drachenhüter" {
		t.Errorf("a series in titles: %v", drachen)
	}

	// A title locked by hand is shown as staying.
	a.call(editor, http.MethodPatch, "/books/"+ids["Tod im Moor (Kindle Edition)"].String(), map[string]any{"locks": map[string]bool{"title": true}})
	_, body = a.preview(editor, "kind=titles&subject="+ids["Tod im Moor (Kindle Edition)"].String())
	if f := previewFields(previewBooks(body)["Tod im Moor (Kindle Edition)"])["title"]; f["locked"] != true || f["after"] != "Tod im Moor" {
		t.Errorf("a locked title: %v", f)
	}
}

func evidenceOf(book map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range book["evidence"].([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

func TestAPreviewSaysWhatTheProvidersAreKnownToSayAndAsksNone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))

	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path == "/isbn/9780141439587" {
			_ = json.NewEncoder(w).Encode([]metadata.Record{{
				ID: "emma", Title: "Emma", Series: "Highbury", SeriesIndex: floatPtr(1.0),
				Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
			}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	meta := metadata.NewService(a.pool, []metadata.Provider{shelfProvider{base: srv.URL}}, metadata.Options{AllowPrivate: true})
	a.server.Cleanup.Meta = meta

	books := catalog.NewService(a.pool)
	emma, err := books.CreateBook(context.Background(), catalog.NewBook{
		LibraryID: lib, Title: "Emma (Kindle Edition)",
		Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780141439587"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Stolz und Vorurteil (Kindle Edition)"})
	if err != nil {
		t.Fatal(err)
	}
	query := func(id uuid.UUID) string { return "kind=titles&subject=" + id.String() }

	_, body := a.preview(editor, query(emma))
	if got := evidenceOf(previewBooks(body)["Emma (Kindle Edition)"]); len(got) != 0 {
		t.Errorf("evidence before anything was asked: %v", got)
	}

	// An earlier lookup left its answer in the cache.
	if _, err := meta.Candidates(context.Background(), a.book(emma)); err != nil {
		t.Fatal(err)
	}
	before := asked.Load()
	_, body = a.preview(editor, query(emma))
	got := evidenceOf(previewBooks(body)["Emma (Kindle Edition)"])
	if len(got) != 1 || got[0]["source"] != "cached" || got[0]["field"] != "title" || got[0]["value"] != "Emma" || got[0]["verdict"] != "agrees" {
		t.Errorf("the cached answer: %v", got)
	}

	// A match kept for a book, pending or applied, is evidence too; a title
	// that is the one it has now is not backing for the change.
	record, _ := json.Marshal(metadata.Record{ID: "stolz", Title: "Stolz und Vorurteil (Kindle Edition)"})
	if _, err := a.pool.Exec(context.Background(), `
		INSERT INTO metadata_matches (book_id, provider, record_id, score, record) VALUES ($1, 'shelf', 'stolz', 0.5, $2)`,
		other, record); err != nil {
		t.Fatal(err)
	}
	_, body = a.preview(editor, query(other))
	got = evidenceOf(previewBooks(body)["Stolz und Vorurteil (Kindle Edition)"])
	if len(got) != 1 || got[0]["source"] != "match" || got[0]["verdict"] != "differs" {
		t.Errorf("the kept match: %v", got)
	}
	if asked.Load() != before {
		t.Errorf("a preview asked the provider %d times", asked.Load()-before)
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestAClashThatIsToChooseOffersBothFixesAndNeverTakesTheSeriesByDefault(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	ids := map[string]uuid.UUID{}
	create := func(b catalog.NewBook) {
		t.Helper()
		b.LibraryID = lib
		id, err := books.CreateBook(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		ids[b.Title] = id
	}
	falkner := []catalog.NewContributor{{Name: "Kommissar Falkner"}}
	// Forty books by a house name, of which only some carry the
	// series, as series data from files often does.
	for i := range 40 {
		b := catalog.NewBook{Title: fmt.Sprintf("Kommissar Falkner %d: Fall %d", 2900+i, i), Contributors: falkner}
		switch {
		case i < 30:
			b.Series = "Kommissar Falkner"
			n := float64(2900 + i)
			b.SeriesIndex = &n
		case i >= 38:
			b.Title = fmt.Sprintf("Der Fall Nummer %d", i)
		}
		create(b)
	}
	// A real author, whose books are mostly in other series.
	rose := []catalog.NewContributor{{Name: "Ulrike Weidner"}}
	create(catalog.NewBook{Title: "Todeskind", Series: "Ulrike Weidner", Contributors: rose})
	for i := range 4 {
		create(catalog.NewBook{Title: fmt.Sprintf("Weidner Roman %d", i), Series: "Baltimore", Contributors: rose})
	}
	// A house name whose titles say nothing, that a provider calls a series.
	brandt := []catalog.NewContributor{{Name: "Hauptmann Brandt"}}
	create(catalog.NewBook{Title: "Sternenozean", Series: "Hauptmann Brandt", Contributors: brandt})
	create(catalog.NewBook{Title: "Der Unsterbliche", Contributors: brandt})
	create(catalog.NewBook{Title: "Das Atlan-Projekt", Contributors: brandt})
	record, _ := json.Marshal(metadata.Record{ID: "ru", Title: "Der Unsterbliche", Series: "Hauptmann Brandt"})
	if _, err := a.pool.Exec(ctx, `INSERT INTO metadata_matches (book_id, provider, record_id, score, record, state)
		VALUES ($1, 'shelf', 'ru', 0.9, $2, 'pending')`, ids["Der Unsterbliche"], record); err != nil {
		t.Fatal(err)
	}

	list, _ := a.suggestions(editor, "clashes")
	byName := map[string]map[string]any{}
	for _, s := range list {
		byName[found(s)[0]] = s
	}
	for name, suggested := range map[string]string{
		"Kommissar Falkner": "nameIsSeries", "Ulrike Weidner": "clearSeries", "Hauptmann Brandt": "nameIsSeries",
	} {
		s := byName[name]
		if s["fix"] != "choose" || s["suggested"] != suggested {
			t.Errorf("%s: fix %v, suggested %v, want choose and %s", name, s["fix"], s["suggested"], suggested)
		}
	}
	subject := byName["Kommissar Falkner"]["subject"].(string)

	// Nothing takes data away unasked.
	for name, req := range map[string]map[string]any{
		"no fix":         {"kind": "clashes", "picks": []map[string]string{{"subject": subject}}},
		"an unknown fix": {"kind": "clashes", "picks": []map[string]string{{"subject": subject, "fix": "deleteAll"}}},
		"another fix":    {"kind": "clashes", "picks": []map[string]string{{"subject": subject, "fix": "removeAuthor"}}},
	} {
		if status, body, _ := a.call(editor, http.MethodPost, "/cleanup/apply", req); status != 422 {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
	if status, body, _ := a.call(editor, http.MethodPost, "/cleanup/apply", map[string]any{"kind": "clashes"}); status != 404 {
		t.Errorf("apply all with nothing but clashes to choose: %d %v", status, body)
	}
	if status, _ := a.preview(editor, "kind=clashes&subject="+url.QueryEscape(subject)); status != 422 {
		t.Errorf("a preview without a choice: %d", status)
	}
	if got := a.book(ids["Kommissar Falkner 2900: Fall 0"]); got.Series != "Kommissar Falkner" || len(got.Authors()) != 1 {
		t.Errorf("a book changed without a choice: %q %v", got.Series, got.Authors())
	}

	// Each choice says what it does to the books, the series' places too.
	_, body := a.preview(editor, "kind=clashes&fix=clearSeries&limit=200&subject="+url.QueryEscape(subject))
	if body["total"].(float64) != 30 {
		t.Errorf("clearing the series changes %v books, want the 30 in it", body["total"])
	}
	cleared := previewFields(previewBooks(body)["Kommissar Falkner 2900: Fall 0"])["series"]
	if cleared["before"] != "Kommissar Falkner #2900" || cleared["after"] != "" {
		t.Errorf("the places that would be lost: %v", cleared)
	}
	_, body = a.preview(editor, "kind=clashes&fix=nameIsSeries&limit=200&subject="+url.QueryEscape(subject))
	if body["total"].(float64) != 40 {
		t.Errorf("the name being the series changes %v books, want all 40", body["total"])
	}
	shown := previewBooks(body)
	given := previewFields(shown["Kommissar Falkner 2931: Fall 31"])
	if given["series"]["before"] != "" || given["series"]["after"] != "Kommissar Falkner #2931" || given["contributors"]["after"] != "" {
		t.Errorf("a book given the series at the place its title says: %v", given)
	}
	if untitled := previewFields(shown["Der Fall Nummer 39"]); untitled["series"]["after"] != "Kommissar Falkner" {
		t.Errorf("a book whose title says no place: %v", untitled)
	}

	// The choice made is the one applied.
	a.applyCleanup(editor, map[string]any{
		"kind": "clashes", "picks": []map[string]string{{"subject": subject, "fix": "nameIsSeries"}},
	})
	for _, title := range []string{"Kommissar Falkner 2900: Fall 0", "Kommissar Falkner 2931: Fall 31", "Der Fall Nummer 39"} {
		if got := a.book(ids[title]); got.Series != "Kommissar Falkner" || len(got.Authors()) != 0 {
			t.Errorf("%s: series %q, authors %v", title, got.Series, got.Authors())
		}
	}
	if got := a.book(ids["Kommissar Falkner 2931: Fall 31"]); got.SeriesIndex == nil || *got.SeriesIndex != 2931 {
		t.Errorf("the place read from the title: %v", got.SeriesIndex)
	}
	if got := a.book(ids["Kommissar Falkner 2900: Fall 0"]); got.SeriesIndex == nil || *got.SeriesIndex != 2900 {
		t.Errorf("the place it had: %v", got.SeriesIndex)
	}
	if got := a.book(ids["Todeskind"]); got.Series != "Ulrike Weidner" {
		t.Errorf("another clash was changed: %q", got.Series)
	}
}
