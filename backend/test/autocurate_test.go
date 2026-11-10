//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/autocurate"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// autoCleanup asks for an automatic clean-up as the client, works the job
// through and returns the run.
func (a *app) autoCleanup(c *http.Client, viewer string, kinds []string, dry bool) string {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/curation/cleanup", map[string]any{"kinds": kinds, "dryRun": dry})
	if status != http.StatusAccepted {
		a.t.Fatalf("automatic clean-up: %d %v", status, body)
	}
	run := body["runId"].(string)
	args := autocurate.CleanupArgs{RunID: uuid.MustParse(run), Kinds: kinds, Viewer: uuid.MustParse(viewer), DryRun: dry}
	for {
		done, err := a.server.Auto.RunCleanup(context.Background(), args)
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			return run
		}
	}
}

func (a *app) autoMatches(c *http.Client, viewer string, body map[string]any) string {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/curation/matches", body)
	if status != http.StatusAccepted {
		a.t.Fatalf("automatic matches: %d %v", status, out)
	}
	run := out["runId"].(string)
	args := autocurate.MatchesArgs{RunID: uuid.MustParse(run), Viewer: uuid.MustParse(viewer), DryRun: body["dryRun"] == true}
	if p, ok := body["provider"].(string); ok {
		args.Group = &autocurate.MatchGroup{Provider: p, Band: body["band"].(int)}
	}
	if err := a.server.Auto.RunMatches(context.Background(), args); err != nil {
		a.t.Fatal(err)
	}
	return run
}

func TestAutomaticCleanupAppliesOnlyWhatTheDataDecides(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	open := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	hidden := uuid.MustParse(a.library(admin, "Hidden", "private"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	add := func(lib uuid.UUID, b catalog.NewBook) {
		t.Helper()
		b.LibraryID = lib
		id, err := books.CreateBook(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		ids[b.Title] = id
	}
	person := func(names ...string) []catalog.NewContributor {
		var out []catalog.NewContributor
		for _, n := range names {
			out = append(out, catalog.NewContributor{Name: n})
		}
		return out
	}
	add(open, catalog.NewBook{Title: "Ohne Namen", Contributors: person("authors_sort", "Real Person")})
	add(hidden, catalog.NewBook{Title: "Verborgen", Contributors: person("Unknown")})
	add(open, catalog.NewBook{Title: "Tod im Moor (Kindle Edition)"})
	add(open, catalog.NewBook{Title: "Stillwasser: Kriminalroman"})
	add(open, catalog.NewBook{Title: "Der Wirbel - Roman"})

	add(open, catalog.NewBook{Title: "Das stille Archiv", Contributors: person("Weidner, Ulrike")})
	add(open, catalog.NewBook{Title: "Der verschollene Abt", Contributors: person("Weidner, Ulrike")})
	add(open, catalog.NewBook{Title: "Die Gräfin", Contributors: person("Ulrike Weidner")})
	add(open, catalog.NewBook{Title: "Zwei Annas", Contributors: person("Anna Maria Prendell")})
	add(open, catalog.NewBook{Title: "Noch eine Anna", Contributors: person("Maria Anna Prendell")})
	add(open, catalog.NewBook{Title: "Ein Anfang", Contributors: person("J.R.R. Fenwick")})
	add(open, catalog.NewBook{Title: "Ein Ende", Contributors: person("Fenwick, J. R. R.")})

	add(open, catalog.NewBook{Title: "Sternwanderer 01 - Leuchtturmwärter"})
	add(open, catalog.NewBook{Title: "Sternwanderer 02 - Wellenbrecher"})
	add(open, catalog.NewBook{Title: "Nebelglocke (Sternwanderer #3)"})
	add(open, catalog.NewBook{Title: "Sternwanderer Prolog", Series: "Sternwanderer"})
	add(open, catalog.NewBook{Title: "Quellwasser 1 - Erste"})
	add(open, catalog.NewBook{Title: "Quellwasser 1 - Zweite"})
	add(open, catalog.NewBook{Title: "Quellwasser 2 - Dritte"})
	add(open, catalog.NewBook{Title: "Quellwasser Prolog", Series: "Quellwasser"})
	add(open, catalog.NewBook{Title: "Nordlicht 1 - Eins"})
	add(open, catalog.NewBook{Title: "Nordlicht 2 - Zwei"})
	add(open, catalog.NewBook{Title: "Nordlicht Vorspiel", Series: "Nordlicht"})
	add(open, catalog.NewBook{Title: "Geheime Reihe 1 - Das Erbe"})
	add(open, catalog.NewBook{Title: "Geheime Reihe 2 - Das Siegel"})
	add(open, catalog.NewBook{Title: "Geheime Reihe 3 - Der Schlüssel"})
	add(hidden, catalog.NewBook{Title: "Geheime Reihe Auftakt", Series: "Geheime Reihe"})
	add(open, catalog.NewBook{Title: "Nebeltal 1 - Aufbruch"})
	add(open, catalog.NewBook{Title: "Nebeltal 2 - Rast"})
	add(open, catalog.NewBook{Title: "Nebeltal 3 - Heimkehr"})
	add(open, catalog.NewBook{Title: "Kommissar Falkner 1: Der Tod", Series: "Kommissar Falkner", Contributors: person("Kommissar Falkner")})
	add(open, catalog.NewBook{Title: "Gesperrt (German Edition)"})
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET locked_fields = ARRAY['title'] WHERE id = $1`, ids["Gesperrt (German Edition)"]); err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(metadata.Record{Series: "Nebeltal"})
	if _, err := a.pool.Exec(context.Background(), `INSERT INTO metadata_matches (book_id, provider, record_id, score, record) VALUES ($1, 'openlibrary', 'nt', 0.7, $2)`,
		ids["Nebeltal 2 - Rast"], record); err != nil {
		t.Fatal(err)
	}

	if status, _, _ := a.call(reader, http.MethodPost, "/curation/cleanup", map[string]any{"kinds": []string{"titles"}}); status != 403 {
		t.Errorf("a reader starts a clean-up: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/cleanup", map[string]any{"kinds": []string{"clashes"}}); status != 422 {
		t.Errorf("a clash is applied by itself: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/cleanup", map[string]any{"kinds": []string{}}); status != 422 {
		t.Errorf("no kinds: %d", status)
	}

	status, counts, _ := a.call(editor, http.MethodGet, "/curation/cleanup", nil)
	if status != 200 {
		t.Fatalf("counts: %d %v", status, counts)
	}
	got := map[string]int{}
	for _, k := range counts["kinds"].([]any) {
		m := k.(map[string]any)
		got[m["kind"].(string)] = int(m["suggestions"].(float64))
	}
	want := map[string]int{"placeholders": 1, "titles": 4, "authors": 2, "series": 2}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %d suggestions, want %d (%v)", k, got[k], n, got)
		}
	}

	all := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		all = append(all, id)
	}
	var allStr []string
	for _, id := range all {
		allStr = append(allStr, id.String())
	}
	before := a.stateOf(allStr...)

	dry := a.autoCleanup(editor, editorID, []string{"placeholders", "titles", "authors", "series"}, true)
	a.sameState("a dry run", before, a.stateOf(allStr...))
	actions := a.runActions(editor, dry)
	if len(actions) < 10 {
		t.Errorf("a dry run records %d actions", len(actions))
	}
	for _, act := range actions {
		if !strings.HasPrefix(act["reason"].(string), "Would change: ") || act["rule"] == nil {
			t.Errorf("an action of a dry run: %v", act)
		}
	}
	if status, body, _ := a.call(editor, http.MethodPost, "/curation/runs/"+dry+"/confirm", nil); status != 409 {
		t.Errorf("confirming a dry run: %d %v", status, body)
	}
	for _, r := range a.runs(editor) {
		if r["id"] == dry && (r["dryRun"] != true || r["automatic"] != true) {
			t.Errorf("the dry run: %v", r)
		}
	}

	run := a.autoCleanup(editor, editorID, []string{"placeholders", "titles", "authors", "series"}, false)
	title := func(name string) string { return a.book(ids[name]).Title }
	authors := func(name string) []string { return a.book(ids[name]).Authors() }
	if got := authors("Ohne Namen"); !slices.Equal(got, []string{"Real Person"}) {
		t.Errorf("the placeholder stayed: %v", got)
	}
	if got := authors("Verborgen"); !slices.Equal(got, []string{"Unknown"}) {
		t.Errorf("a book the editor does not see was changed: %v", got)
	}
	for _, n := range []string{"Tod im Moor", "Stillwasser", "Der Wirbel"} {
		if !a.bookTitleExists(n) {
			t.Errorf("the shop's addition stayed on %q", n)
		}
	}
	if got := title("Gesperrt (German Edition)"); got != "Gesperrt (German Edition)" {
		t.Errorf("a locked title changed to %q", got)
	}
	for _, n := range []string{"Das stille Archiv", "Der verschollene Abt", "Die Gräfin"} {
		if got := authors(n); len(got) != 1 || got[0] == "" || catalog.PersonKey(got[0]) != "ulrike weidner" {
			t.Errorf("%s: %v", n, got)
		}
	}
	spell := func(names ...string) []string {
		var out []string
		for _, n := range names {
			out = append(out, authors(n)[0])
		}
		return out
	}
	if got := spell("Das stille Archiv", "Der verschollene Abt", "Die Gräfin"); got[0] != got[1] || got[1] != got[2] {
		t.Errorf("one person, three spellings still: %v", got)
	}
	if got := spell("Zwei Annas", "Noch eine Anna"); got[0] != "Anna Maria Prendell" || got[1] != "Maria Anna Prendell" {
		t.Errorf("two people were taken for one: %v", got)
	}
	if got := spell("Ein Anfang", "Ein Ende"); got[0] != got[1] {
		t.Errorf("initials: %v", got)
	}
	series := func(name string) string { return a.book(ids[name]).Series }
	for _, n := range []string{"Sternwanderer 01 - Leuchtturmwärter", "Sternwanderer 02 - Wellenbrecher", "Nebelglocke (Sternwanderer #3)"} {
		if series(n) != "Sternwanderer" {
			t.Errorf("%s: series %q", n, series(n))
		}
	}
	for _, n := range []string{"Quellwasser 1 - Erste", "Nordlicht 1 - Eins", "Geheime Reihe 1 - Das Erbe"} {
		if series(n) != "" {
			t.Errorf("%s took series %q although the data does not decide", n, series(n))
		}
	}
	for _, n := range []string{"Nebeltal 1 - Aufbruch", "Nebeltal 2 - Rast", "Nebeltal 3 - Heimkehr"} {
		if series(n) != "Nebeltal" {
			t.Errorf("%s: a provider's record names the series, got %q", n, series(n))
		}
	}

	acts := a.runActions(editor, run)
	for _, act := range acts {
		if act["rule"] == nil || !strings.HasPrefix(act["rule"].(string), "cleanup.") || act["reason"] == "" {
			t.Errorf("an action without its rule: %v", act)
		}
	}
	for _, r := range a.runs(editor) {
		if r["id"] == run && (r["automatic"] != true || r["dryRun"] != false || r["state"] != "done") {
			t.Errorf("the run: %v", r)
		}
	}
	// Automatic runs wait to write into files until confirmed.
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 200 {
		t.Fatalf("undo: %d", status)
	}
	a.sameState("a run undone", before, a.stateOf(allStr...))
	if got := authors("Verborgen"); !slices.Equal(got, []string{"Unknown"}) {
		t.Errorf("hidden: %v", got)
	}
}

func (a *app) bookTitleExists(title string) bool {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM books WHERE title = $1`, title).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n > 0
}

// reviewable gives the app what the review page needs: the matches and a
// metadata service that asks nobody.
func (a *app) reviewable() {
	a.t.Helper()
	a.server.Metadata = metadata.NewService(a.pool, nil, metadata.Options{})
	a.server.Matches = enrich.NewService(a.pool, a.server.Metadata, a.scans, a.covers, a.settings, slog.New(slog.DiscardHandler))
}

func (a *app) addMatch(book uuid.UUID, provider, id string, score float64, r metadata.Record) uuid.UUID {
	a.t.Helper()
	r.ID, r.Provider = id, provider
	data, _ := json.Marshal(r)
	var m uuid.UUID
	err := a.pool.QueryRow(context.Background(), `INSERT INTO metadata_matches (book_id, provider, record_id, score, record)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, book, provider, id, score, data).Scan(&m)
	if err != nil {
		a.t.Fatal(err)
	}
	return m
}

func (a *app) matchState(id uuid.UUID) string {
	a.t.Helper()
	var s string
	if err := a.pool.QueryRow(context.Background(), `SELECT state FROM metadata_matches WHERE id = $1`, id).Scan(&s); err != nil {
		a.t.Fatal(err)
	}
	return s
}

func TestAutomaticMatchesTakeWhatProvidersAgreeOnAndLeaveTheRest(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.reviewable()
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	open := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	hidden := uuid.MustParse(a.library(admin, "Hidden", "private"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	add := func(lib uuid.UUID, b catalog.NewBook) {
		t.Helper()
		b.LibraryID = lib
		id, err := books.CreateBook(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		ids[b.Title] = id
	}
	isbn := func(v string) []catalog.Identifier { return []catalog.Identifier{{Type: catalog.IDISBN, Value: v}} }
	author := []catalog.NewContributor{{Name: "Lena Brandt"}}
	rec := func(title string) metadata.Record {
		return metadata.Record{Title: title, Contributors: []catalog.NewContributor{{Name: "Lena Brandt", Role: catalog.RoleAuthor}}}
	}

	add(open, catalog.NewBook{Title: "das stille archiv", Source: catalog.SourceFilename, Identifiers: isbn("9783596294312"), Contributors: author})
	shared := rec("Das stille Archiv")
	shared.Identifiers = isbn("9783596294312")
	shared.Description, shared.Publisher, shared.Published = "A ledger turns up.", "Nordlicht", "2019"
	whole := a.addMatch(ids["das stille archiv"], "openlibrary", "a", 0.8, shared)
	other := a.addMatch(ids["das stille archiv"], "crossref", "b", 0.6, rec("Archiv"))

	add(open, catalog.NewBook{Title: "Der verschollene Abt", Description: "Eine eigene Beschreibung.", Identifiers: isbn("9780141439587"), Contributors: author})
	partial := rec("Der verschollene Abt")
	partial.Identifiers = isbn("9780141439587")
	partial.Description, partial.Publisher = "A different blurb altogether.", "Nordlicht"
	conflict := a.addMatch(ids["Der verschollene Abt"], "openlibrary", "c", 0.8, partial)
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET locked_fields = ARRAY['publisher'] WHERE id = $1`, ids["Der verschollene Abt"]); err != nil {
		t.Fatal(err)
	}

	add(open, catalog.NewBook{Title: "Die Gräfin", Source: catalog.SourceFilename, Contributors: author})
	one := rec("Die Gräfin")
	one.Work, one.Publisher = "w-grafin", "Nordlicht"
	two := rec("Die Gräfin")
	two.Work = "w-grafin"
	agreeA := a.addMatch(ids["Die Gräfin"], "openlibrary", "d", 0.7, one)
	agreeB := a.addMatch(ids["Die Gräfin"], "hardcover", "e", 0.7, two)

	add(open, catalog.NewBook{Title: "Marl", Source: catalog.SourceFilename, Contributors: author})
	alone := rec("Marl")
	alone.Publisher = "Nordlicht"
	lonely := a.addMatch(ids["Marl"], "openlibrary", "f", 0.7, alone)

	add(open, catalog.NewBook{Title: "Zeitreise", Source: catalog.SourceFilename, Identifiers: isbn("9780306406157"), Contributors: author})
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET published_on = '1995-01-01', published_precision = 'year',
		field_sources = field_sources || '{"published":"file:epub:x"}' WHERE id = $1`, ids["Zeitreise"]); err != nil {
		t.Fatal(err)
	}
	late := rec("Zeitreise")
	late.Identifiers, late.Published, late.Publisher = isbn("9780306406157"), "2015", "Nordlicht"
	tooLate := a.addMatch(ids["Zeitreise"], "openlibrary", "g", 0.9, late)

	add(hidden, catalog.NewBook{Title: "Verborgen", Source: catalog.SourceFilename, Identifiers: isbn("9780195001013"), Contributors: author})
	secret := rec("Verborgen")
	secret.Identifiers, secret.Publisher = isbn("9780195001013"), "Nordlicht"
	hiddenMatch := a.addMatch(ids["Verborgen"], "openlibrary", "h", 0.9, secret)

	var every []string
	for _, id := range ids {
		every = append(every, id.String())
	}
	before := a.stateOf(every...)

	status, plan, _ := a.call(editor, http.MethodGet, "/curation/matches", nil)
	if status != 200 || plan["accept"].(float64) != 3 || plan["dismiss"].(float64) != 1 || plan["settle"].(float64) != 2 {
		t.Fatalf("what the rule would do: %d %v\n%s", status, plan, a.logs.String())
	}
	groups := plan["groups"].([]any)
	if len(groups) == 0 {
		t.Error("no groups of the matches that wait")
	}

	dry := a.autoMatches(editor, editorID, map[string]any{"dryRun": true})
	a.sameState("a dry run", before, a.stateOf(every...))
	if n := len(a.runActions(editor, dry)); n != 4 {
		t.Errorf("a dry run records %d actions, want 4", n)
	}

	run := a.autoMatches(editor, editorID, map[string]any{"dryRun": false})
	whole1 := a.book(ids["das stille archiv"])
	if whole1.Title != "Das stille Archiv" || whole1.Description != "A ledger turns up." || whole1.Publisher != "Nordlicht" || whole1.Published() != "2019" {
		t.Errorf("a match with the book's ISBN was not taken: %+v", whole1)
	}
	if a.matchState(whole) != "applied" || a.matchState(other) != "dismissed" {
		t.Errorf("states: %s %s", a.matchState(whole), a.matchState(other))
	}
	abt := a.book(ids["Der verschollene Abt"])
	if abt.Description != "Eine eigene Beschreibung." || abt.Publisher != "" {
		t.Errorf("a conflict or a locked field was overwritten: %+v", abt)
	}
	if a.matchState(conflict) != "pending" {
		t.Errorf("a match with a field in conflict is %s", a.matchState(conflict))
	}
	if got := a.book(ids["Die Gräfin"]); got.Publisher != "Nordlicht" || a.matchState(agreeA) != "applied" || a.matchState(agreeB) != "dismissed" {
		t.Errorf("two providers of one work: %+v %s %s", got, a.matchState(agreeA), a.matchState(agreeB))
	}
	if a.book(ids["Marl"]).Publisher != "" || a.matchState(lonely) != "pending" {
		t.Error("a match nobody vouches for was taken")
	}
	if a.matchState(tooLate) != "dismissed" || a.book(ids["Zeitreise"]).Publisher != "" {
		t.Errorf("a record years from the file's date: %s", a.matchState(tooLate))
	}
	if a.matchState(hiddenMatch) != "pending" || a.book(ids["Verborgen"]).Publisher != "" {
		t.Error("a book the editor does not see was touched")
	}
	acts := a.runActions(editor, run)
	if len(acts) != 4 {
		t.Fatalf("%d actions, want 4", len(acts))
	}
	for _, act := range acts {
		if act["rule"] == nil || !strings.HasPrefix(act["rule"].(string), "matches.") || act["reason"] == "" {
			t.Errorf("an action: %v", act)
		}
	}

	if status, out, _ := a.call(editor, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 200 {
		t.Fatalf("undo: %d %v", status, out)
	}
	a.sameState("an undone run", before, a.stateOf(every...))
	for m, want := range map[uuid.UUID]string{whole: "pending", other: "pending", conflict: "pending", tooLate: "pending", agreeA: "pending"} {
		if got := a.matchState(m); got != want {
			t.Errorf("after undo, a match is %s, want %s", got, want)
		}
	}
}

func TestAGroupOfMatchesIsAcceptedWholeWithoutAnyoneVouching(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.reviewable()
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	open := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	var ids []uuid.UUID
	var matches []uuid.UUID
	for _, title := range []string{"Marl", "Nachtkind", "Stillwasser"} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: open, Title: title, Source: catalog.SourceFilename})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		matches = append(matches, a.addMatch(id, "openlibrary", title, 0.85, metadata.Record{Title: title, Publisher: "Nordlicht"}))
	}
	strange := a.addMatch(ids[0], "hardcover", "zz", 0.6, metadata.Record{Title: "Marl", Publisher: "Other"})

	status, plan, _ := a.call(editor, http.MethodGet, "/curation/matches", nil)
	if status != 200 || plan["accept"].(float64) != 0 {
		t.Fatalf("nobody vouches: %d %v", status, plan)
	}
	var found map[string]any
	for _, g := range plan["groups"].([]any) {
		m := g.(map[string]any)
		if m["provider"] == "openlibrary" && m["band"].(float64) == 8 {
			found = m
		}
	}
	if found == nil || found["matches"].(float64) != 3 || len(found["sample"].([]any)) != 3 {
		t.Fatalf("the group: %v", plan["groups"])
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/matches", map[string]any{"provider": "openlibrary"}); status != 422 {
		t.Errorf("a group without a band: %d", status)
	}

	a.autoMatches(editor, editorID, map[string]any{"dryRun": false, "provider": "openlibrary", "band": 8})
	for i, id := range ids {
		if a.book(id).Publisher != "Nordlicht" || a.matchState(matches[i]) != "applied" {
			t.Errorf("book %d: %+v %s", i, a.book(id), a.matchState(matches[i]))
		}
	}
	if a.matchState(strange) != "dismissed" {
		t.Errorf("the other match of a book taken whole is %s", a.matchState(strange))
	}
}
