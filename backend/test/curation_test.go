//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// stateOf is the rows of every table a curation action snapshots, for the
// books, as JSON by table.
func (a *app) stateOf(books ...string) map[string]string {
	a.t.Helper()
	ids := make([]uuid.UUID, len(books))
	for i, b := range books {
		ids[i] = uuid.MustParse(b)
	}
	tx, err := a.pool.Begin(context.Background())
	if err != nil {
		a.t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	snap, err := curation.Capture(context.Background(), tx, ids, curation.Tables)
	if err != nil {
		a.t.Fatal(err)
	}
	out := map[string]string{}
	for table, rows := range snap {
		out[table] = string(rows)
	}
	return out
}

func (a *app) sameState(what string, want, got map[string]string) {
	a.t.Helper()
	for _, t := range curation.Tables {
		if want[t.Name] != got[t.Name] {
			a.t.Errorf("%s: %s differs\n  before: %s\n  after:  %s", what, t.Name, want[t.Name], got[t.Name])
		}
	}
}

func (a *app) runs(c *http.Client) []map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/curation/runs", nil)
	if status != 200 {
		a.t.Fatalf("runs: %d %v", status, body)
	}
	var out []map[string]any
	for _, r := range body["runs"].([]any) {
		out = append(out, r.(map[string]any))
	}
	return out
}

func (a *app) runActions(c *http.Client, run string) []map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/curation/runs/"+run, nil)
	if status != 200 {
		a.t.Fatalf("run %s: %d %v", run, status, body)
	}
	var out []map[string]any
	for _, r := range body["actions"].([]any) {
		out = append(out, r.(map[string]any))
	}
	return out
}

type scene struct {
	admin, reader, other *http.Client
	adminID              string
	lib                  string
	keep, gone, third    string
	goneFile             string
}

// richPair is two books of one library that people have read, shelved and
// sent to a device, which are a pair of duplicates.
func (a *app) richPair() scene {
	a.t.Helper()
	t, ctx := a.t, context.Background()
	s := scene{}
	var readerID, otherID string
	s.admin, s.adminID = a.signedIn("admin", "admin")
	s.reader, readerID = a.signedIn("rita", "reader")
	s.other, otherID = a.signedIn("otto", "reader")
	s.lib = a.library(s.admin, "Novels", "shared")
	for _, id := range []string{readerID, otherID} {
		a.call(s.admin, http.MethodPut, "/libraries/"+s.lib+"/members/"+id, nil)
	}
	upload := func(name string) (book, file string) {
		t.Helper()
		status, up := a.upload(s.admin, s.lib, name, []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload %s: %d %v", name, status, up)
		}
		return up["bookId"].(string), up["fileId"].(string)
	}
	s.keep, _ = upload("Emma.pdf")
	s.gone, s.goneFile = upload("Emma Novel.pdf")
	s.third, _ = upload("Persuasion.pdf")
	edit := func(book string, body map[string]any) {
		t.Helper()
		if status, out, _ := a.call(s.admin, http.MethodPatch, "/books/"+book, body); status != 200 {
			t.Fatalf("edit: %d %v", status, out)
		}
	}
	edit(s.keep, map[string]any{"title": "Emma", "contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}}, "tags": []string{"Classic"}})
	edit(s.gone, map[string]any{
		"title": "Emma: A Novel", "description": "A heroine whom no one but myself will much like.",
		"identifiers": []map[string]string{{"type": "isbn", "value": "9780141439587"}},
		"tags":        []string{"Romance"}, "series": map[string]any{"name": "Austen", "index": 4},
	})
	put := func(c *http.Client, path string, body map[string]any) {
		t.Helper()
		if status, out, _ := a.call(c, http.MethodPut, path, body); status != 200 {
			t.Fatalf("PUT %s: %d %v", path, status, out)
		}
	}
	put(s.reader, "/books/"+s.keep+"/reading", map[string]any{"status": "reading"})
	put(s.reader, "/books/"+s.keep+"/progress/ebook", map[string]any{"locator": "page:3", "fraction": 0.3, "clientId": "phone"})
	put(s.reader, "/books/"+s.gone+"/progress/ebook", map[string]any{"fileId": s.goneFile, "locator": "page:8", "fraction": 0.8, "clientId": "tablet"})
	put(s.reader, "/books/"+s.gone+"/reading", map[string]any{"status": "completed", "rating": 5})
	put(s.other, "/books/"+s.gone+"/progress/ebook", map[string]any{"locator": "page:5", "fraction": 0.5, "clientId": "laptop"})
	for _, c := range []struct {
		who   *http.Client
		books []string
	}{{s.reader, []string{s.gone}}, {s.other, []string{s.keep, s.gone}}} {
		_, col, _ := a.call(c.who, http.MethodPost, "/collections", map[string]any{"name": "Shelf", "visibility": "private"})
		a.call(c.who, http.MethodPost, "/collections/"+col["id"].(string)+"/books", map[string]any{"books": c.books})
	}
	_, device, _ := a.call(s.reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	a.call(s.reader, http.MethodPost, "/devices/"+device["device"].(map[string]any)["id"].(string)+"/books", map[string]any{"books": []string{s.gone}})
	if _, err := a.pool.Exec(ctx, `INSERT INTO notifications (user_id, kind, data, link, book_id)
		VALUES ($1, 'bulk.finished', '{}', '/books/' || $2, $2::uuid)`, readerID, s.gone); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO book_notes (user_id, book_id, body) VALUES ($1, $2, 'mine')`, readerID, s.gone); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]string{{s.keep, s.gone}, {s.gone, s.third}} {
		x, y := p[0], p[1]
		if y < x {
			x, y = y, x
		}
		if _, err := a.pool.Exec(ctx, `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
			INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, 'title_author', 'emma' FROM p`, x, y); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (a *app) undoRun(c *http.Client, run string) []any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/curation/runs/"+run+"/undo", nil)
	if status != 200 {
		a.t.Fatalf("undo run: %d %v", status, out)
	}
	return out["actions"].([]any)
}

func TestUndoingAMergeRestoresEveryRowItMoved(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	s := a.richPair()
	before := a.stateOf(s.keep, s.gone)

	if status, out, _ := a.call(s.admin, http.MethodPost, "/books/"+s.keep+"/merge", map[string]any{"from": s.gone, "take": []string{"title"}}); status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	merged := a.stateOf(s.keep, s.gone)
	if merged["user_books"] == before["user_books"] || merged["book_files"] == before["book_files"] {
		t.Fatal("the merge changed nothing the snapshot sees")
	}

	runs := a.runs(s.admin)
	if len(runs) != 1 || runs[0]["kind"] != "duplicates" || runs[0]["state"] != "done" || runs[0]["automatic"] != false ||
		runs[0]["startedByName"] != "admin" || runs[0]["actions"] != 1.0 || runs[0]["rule"] != nil {
		t.Fatalf("runs: %v", runs)
	}
	run := runs[0]["id"].(string)
	actions := a.runActions(s.admin, run)
	if len(actions) != 1 || actions[0]["kind"] != "merge" {
		t.Fatalf("actions: %v", actions)
	}
	keepChanges := map[string]string{}
	for _, b := range actions[0]["books"].([]any) {
		if b.(map[string]any)["id"] == s.keep {
			for _, c := range b.(map[string]any)["changes"].([]any) {
				keepChanges[c.(map[string]any)["field"].(string)] = c.(map[string]any)["after"].(string)
			}
		}
	}
	if keepChanges["title"] != "Emma: A Novel" || keepChanges["files"] != "2" || keepChanges["series"] != "Austen" {
		t.Errorf("what the merge changed about the book kept: %v", keepChanges)
	}

	if status, _, _ := a.call(s.reader, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 403 {
		t.Errorf("a reader undoes: %d", status)
	}
	outcomes := a.undoRun(s.admin, run)
	if len(outcomes) != 1 || outcomes[0].(map[string]any)["state"] != "undone" {
		t.Fatalf("undo: %v", outcomes)
	}
	a.sameState("after undoing the merge", before, a.stateOf(s.keep, s.gone))
	_, book, _ := a.call(s.admin, http.MethodGet, "/books/"+s.gone, nil)
	if book["id"] != s.gone || len(book["files"].([]any)) != 1 {
		t.Errorf("the merged book after the undo: %v", book)
	}
	if got := a.runs(s.admin)[0]; got["state"] != "undone" || got["undone"] != 1.0 {
		t.Errorf("the run after the undo: %v", got)
	}
	if outcomes := a.undoRun(s.admin, run); len(outcomes) != 0 {
		t.Errorf("undoing a run again: %v", outcomes)
	}
}

func TestUndoingAReplaceBringsTheTrashedFilesBack(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	s := a.richPair()
	before := a.stateOf(s.keep, s.gone)
	var pair, root, rel string
	if err := a.pool.QueryRow(ctx, `SELECT id::text FROM duplicate_pairs WHERE book_a = least($1::uuid, $2::uuid) AND book_b = greatest($1::uuid, $2::uuid)`, s.keep, s.gone).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT l.root_path, f.rel_path FROM book_files f JOIN libraries l ON l.id = f.library_id WHERE f.id = $1`, s.goneFile).Scan(&root, &rel); err != nil {
		t.Fatal(err)
	}
	if status, out, _ := a.call(s.admin, http.MethodPost, "/duplicates/"+pair+"/replace", map[string]any{"keep": s.keep}); status != 204 {
		t.Fatalf("replace: %d %v", status, out)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
		t.Fatalf("the file is still in place: %v", err)
	}
	run := a.runs(s.admin)[0]["id"].(string)
	actions := a.runActions(s.admin, run)
	if len(actions) != 1 || actions[0]["kind"] != "replace" || actions[0]["trashedFiles"] != 1.0 {
		t.Fatalf("actions: %v", actions)
	}

	if outcomes := a.undoRun(s.admin, run); outcomes[0].(map[string]any)["state"] != "undone" {
		t.Fatalf("undo: %v", outcomes)
	}
	a.sameState("after undoing the replace", before, a.stateOf(s.keep, s.gone))
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		t.Errorf("the file is not back: %v", err)
	}
	if len(a.trash(s.admin).Files) != 0 {
		t.Errorf("the trash after the undo: %+v", a.trash(s.admin))
	}
}

func TestAReplaceWhoseTrashIsPurgedCannotBeUndone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	s := a.richPair()
	var pair string
	if err := a.pool.QueryRow(ctx, `SELECT id::text FROM duplicate_pairs WHERE book_a = least($1::uuid, $2::uuid) AND book_b = greatest($1::uuid, $2::uuid)`, s.keep, s.gone).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	if status, out, _ := a.call(s.admin, http.MethodPost, "/duplicates/"+pair+"/replace", map[string]any{"keep": s.keep}); status != 204 {
		t.Fatalf("replace: %d %v", status, out)
	}
	if err := a.scans.Purge(ctx, uuid.MustParse(s.goneFile)); err != nil {
		t.Fatal(err)
	}
	after := a.stateOf(s.keep, s.gone)
	o := a.undoRun(s.admin, a.runs(s.admin)[0]["id"].(string))
	if len(o) != 1 || o[0].(map[string]any)["state"] != "notUndoable" {
		t.Fatalf("undo: %v", o)
	}
	a.sameState("a replace that could not be undone", after, a.stateOf(s.keep, s.gone))
	if actions := a.runActions(s.admin, a.runs(s.admin)[0]["id"].(string)); actions[0]["state"] != "done" || actions[0]["note"] == "" {
		t.Errorf("the action says: %v", actions[0])
	}
}

func TestAnActionWhoseBooksChangedSinceIsLeftAndReported(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	s := a.richPair()
	if status, out, _ := a.call(s.admin, http.MethodPost, "/books/"+s.keep+"/merge", map[string]any{"from": s.gone}); status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	run := a.runs(s.admin)[0]["id"].(string)
	action := a.runActions(s.admin, run)[0]["id"].(string)
	if status, out, _ := a.call(s.admin, http.MethodPatch, "/books/"+s.keep, map[string]any{"title": "Emma, revised"}); status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	after := a.stateOf(s.keep, s.gone)

	status, out, _ := a.call(s.admin, http.MethodPost, "/curation/actions/"+action+"/undo", nil)
	if status != 200 || out["state"] != "conflict" || out["conflicts"] == nil {
		t.Fatalf("undo: %d %v", status, out)
	}
	conflicts := out["conflicts"].([]any)
	if conflicts[0].(map[string]any)["table"] != "books" {
		t.Errorf("conflicts: %v", conflicts)
	}
	a.sameState("a conflict", after, a.stateOf(s.keep, s.gone))
	if got := a.runActions(s.admin, run)[0]; got["state"] != "done" || got["note"] == "" {
		t.Errorf("the action after the conflict: %v", got)
	}
	if a.runs(s.admin)[0]["state"] != "done" {
		t.Error("a run with a conflict is not undone")
	}
	if status, _, _ := a.call(s.admin, http.MethodPost, "/curation/actions/"+uuid.NewString()+"/undo", nil); status != 404 {
		t.Errorf("no such action: %d", status)
	}
}

func TestUndoingACleanUpRestoresTheBooksItChanged(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := a.library(admin, "Shelf", "shared")
	books := catalog.NewService(a.pool)
	var ids []string
	for _, b := range []struct{ title, author string }{
		{"Das stille Archiv", "Weidner, Ulrike"},
		{"Der verschollene Abt", "Weidner, Ulrike"},
		{"Die Gräfin", "Ulrike Weidner"},
	} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: uuid.MustParse(lib), Title: b.title, Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id.String())
	}
	before := a.stateOf(ids...)
	a.applyCleanup(editor, map[string]any{
		"kind": "authors", "picks": []map[string]string{{"subject": "ulrike weidner", "to": "Ulrike  Weidner"}},
	})
	changed := a.stateOf(ids...)
	if changed["book_contributors"] == before["book_contributors"] {
		t.Fatal("the clean-up changed nothing")
	}

	runs := a.runs(editor)
	if len(runs) != 1 || runs[0]["kind"] != "cleanup" || runs[0]["state"] != "done" || runs[0]["startedByName"] != "editor" {
		t.Fatalf("runs: %v", runs)
	}
	run := runs[0]["id"].(string)
	actions := a.runActions(editor, run)
	if len(actions) != 2 || actions[0]["kind"] != "edit" {
		t.Fatalf("one action for each book it changed: %v", actions)
	}
	var contributors []any
	for _, c := range actions[0]["books"].([]any)[0].(map[string]any)["changes"].([]any) {
		if c.(map[string]any)["field"] == "contributors" {
			contributors = append(contributors, c)
		}
	}
	if len(contributors) != 1 {
		t.Errorf("the credits before and after: %v", actions[0])
	}

	// One action undone by itself, then the rest of the run.
	first := actions[0]["id"].(string)
	if status, out, _ := a.call(editor, http.MethodPost, "/curation/actions/"+first+"/undo", nil); status != 200 || out["state"] != "undone" {
		t.Fatalf("undo one: %d %v", status, out)
	}
	if got := a.runs(editor)[0]; got["state"] != "done" || got["undone"] != 1.0 {
		t.Errorf("the run after one: %v", got)
	}
	outcomes := a.undoRun(editor, run)
	if len(outcomes) != 1 || outcomes[0].(map[string]any)["state"] != "undone" {
		t.Fatalf("undo the rest: %v", outcomes)
	}
	a.sameState("after undoing the clean-up", before, a.stateOf(ids...))
	if a.runs(editor)[0]["state"] != "undone" {
		t.Error("the run is not undone")
	}
}

func TestOnlyAnAutomaticRunsTrashIsKeptUntilItIsConfirmed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	s := a.richPair()
	manual := s.goneFile
	var pair string
	if err := a.pool.QueryRow(ctx, `SELECT id::text FROM duplicate_pairs WHERE book_a = least($1::uuid, $2::uuid) AND book_b = greatest($1::uuid, $2::uuid)`, s.keep, s.gone).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	if status, out, _ := a.call(s.admin, http.MethodPost, "/duplicates/"+pair+"/replace", map[string]any{"keep": s.keep}); status != 204 {
		t.Fatalf("replace: %d %v", status, out)
	}
	// A file an automatic run trashed, in a run nobody confirmed.
	_, up := a.upload(s.admin, s.lib, "Auto.pdf", []byte("%PDF-1.4 auto"))
	auto := up["fileId"].(string)
	if err := a.scans.Trash(ctx, library.Scope{Viewer: uuid.MustParse(s.adminID), SeesAll: true}, uuid.MustParse(auto)); err != nil {
		t.Fatal(err)
	}
	var run, libID string
	if err := a.pool.QueryRow(ctx, `INSERT INTO curation_runs (kind, rule, automatic, state) VALUES ('duplicates', 'same-file', true, 'done') RETURNING id::text`).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT library_id::text FROM books WHERE id = $1`, s.keep).Scan(&libID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO curation_actions (run_id, library_id, kind, books, trashed_files, before, after)
		VALUES ($1, $2, 'replace', ARRAY[$3::uuid], ARRAY[$4::uuid], '{}', '{}')`, run, libID, s.keep, auto); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET trashed_at = now() - interval '90 days' WHERE trashed_at IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	exists := func(id string) bool {
		var n int
		if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM book_files WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	if _, err := a.scans.PurgeExpired(ctx, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if exists(manual) {
		t.Error("a person's replace keeps its trash for longer than the retention")
	}
	if !exists(auto) {
		t.Fatal("the trash of an automatic run nobody confirmed was purged")
	}
	if status, _, _ := a.call(s.admin, http.MethodPost, "/curation/runs/"+run+"/confirm", nil); status != 204 {
		t.Fatalf("confirm: %d", status)
	}
	if status, _, _ := a.call(s.admin, http.MethodPost, "/curation/runs/"+run+"/confirm", nil); status != 409 {
		t.Errorf("confirm twice: %d", status)
	}
	if _, err := a.scans.PurgeExpired(ctx, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !exists(auto) {
		t.Error("confirming starts the retention; the file was purged at once")
	}
	if _, err := a.pool.Exec(ctx, `UPDATE curation_runs SET confirmed_at = now() - interval '31 days' WHERE id = $1`, run); err != nil {
		t.Fatal(err)
	}
	if _, err := a.scans.PurgeExpired(ctx, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if exists(auto) {
		t.Error("a confirmed run's trash is purged after the retention")
	}
}

func TestAnAutomaticRunsChangesAreWrittenIntoFilesWhenItIsConfirmed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, adminID := a.signedIn("admin", "admin")
	folder := t.TempDir()
	writeEPUB(t, filepath.Join(folder, "Emma.epub"), emmaMetadata, coverPNG(t), "Emma Woodhouse, handsome, clever, and rich.")
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	emma := a.shelfFiles()["Emma.epub"]
	a.extract(emma.ID)

	var run string
	if err := a.pool.QueryRow(ctx, `INSERT INTO curation_runs (kind, rule, automatic, state) VALUES ('cleanup', 'titles', true, 'running') RETURNING id::text`).Scan(&run); err != nil {
		t.Fatal(err)
	}
	title := "Emma (by a rule)"
	err := a.scans.Edit(curation.WithRun(ctx, uuid.MustParse(run), true), library.Scope{Viewer: uuid.MustParse(adminID), SeesAll: true}, emma.BookID, catalog.Edit{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if n := a.writeJobs(emma.ID); n != 0 {
		t.Fatalf("an unconfirmed run queued %d writes", n)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO curation_actions (run_id, library_id, kind, books, before, after)
		VALUES ($1, $2, 'edit', ARRAY[$3::uuid], '{}', '{}')`, run, lib, emma.BookID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE curation_runs SET state = 'done' WHERE id = $1`, run); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/curation/runs/"+run+"/confirm", nil); status != 204 {
		t.Fatalf("confirm: %d", status)
	}
	if n := a.writeJobs(emma.ID); n != 1 {
		t.Errorf("confirming queued %d writes, want 1", n)
	}
	// An edit outside any run is written at once.
	other := "Emma (by hand)"
	if err := a.scans.Edit(ctx, library.Scope{Viewer: uuid.MustParse(adminID), SeesAll: true}, emma.BookID, catalog.Edit{Title: &other}); err != nil {
		t.Fatal(err)
	}
	if n := a.writeJobs(emma.ID); n != 2 {
		t.Errorf("a person's edit queued %d writes in all, want 2", n)
	}
}

func TestRunsAreListedToThoseWhoSeeTheirLibraries(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	hidden := a.library(admin, "Hidden", "private")
	var ids []string
	for _, name := range []string{"One.pdf", "Two.pdf"} {
		status, up := a.upload(admin, hidden, name, []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		ids = append(ids, up["bookId"].(string))
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/books/"+ids[0]+"/merge", map[string]any{"from": ids[1]}); status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	run := a.runs(admin)[0]["id"].(string)
	if got := a.runs(editor); len(got) != 0 {
		t.Errorf("the editor sees %v", got)
	}
	for _, path := range []string{"/curation/runs/" + run, "/curation/runs/" + uuid.NewString()} {
		status, body := a.get(editor, path)
		if status != 404 {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 404 {
		t.Errorf("undo of a run of a library not seen: %d", status)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/runs/"+run+"/confirm", nil); status != 404 {
		t.Errorf("confirm: %d", status)
	}
	var n int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM curation_actions WHERE state = 'undone'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("undone actions: %d %v", n, err)
	}
}
