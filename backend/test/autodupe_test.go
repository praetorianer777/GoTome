//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// dupBook is a PDF in the library, described as given.
func (a *app) dupBook(c *http.Client, lib, file, title, author string) (book, fileID string) {
	a.t.Helper()
	status, up := a.upload(c, lib, file, []byte("%PDF-1.4 "+file))
	if status != 200 {
		a.t.Fatalf("upload %s: %d %v", file, status, up)
	}
	book, fileID = up["bookId"].(string), up["fileId"].(string)
	if title != "" {
		body := map[string]any{"title": title}
		if author != "" {
			body["contributors"] = []map[string]string{{"name": author, "role": "author"}}
		}
		if status, out, _ := a.call(c, http.MethodPatch, "/books/"+book, body); status != 200 {
			a.t.Fatalf("describe: %d %v", status, out)
		}
	}
	return book, fileID
}

// seedPair makes two books a pair of duplicates on the evidence given.
func (a *app) seedPair(x, y, kind, detail string) string {
	a.t.Helper()
	if y < x {
		x, y = y, x
	}
	var id string
	err := a.pool.QueryRow(context.Background(), `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
		INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, $3, $4 FROM p RETURNING pair_id::text`, x, y, kind, detail).Scan(&id)
	if err != nil {
		a.t.Fatal(err)
	}
	return id
}

func (a *app) pairState(id string) string {
	a.t.Helper()
	var s string
	if err := a.pool.QueryRow(context.Background(), `SELECT state FROM duplicate_pairs WHERE id = $1`, id).Scan(&s); err != nil {
		a.t.Fatal(err)
	}
	return s
}

func (a *app) startDuplicates(c *http.Client, classes []string, dry bool) (int, map[string]any) {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/curation/duplicates", map[string]any{"classes": classes, "dryRun": dry})
	return status, out
}

// runDuplicates queues a run and works its job to the end, as the queue
// would, and returns the run's ID.
func (a *app) runDuplicates(c *http.Client, classes []string, dry bool) string {
	a.t.Helper()
	status, out := a.startDuplicates(c, classes, dry)
	if status != 202 {
		a.t.Fatalf("start: %d %v", status, out)
	}
	run := out["runId"].(string)
	for {
		done, err := a.server.AutoDuplicates.Work(context.Background(), uuid.MustParse(run))
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			return run
		}
	}
}

func (a *app) runInfo(c *http.Client, run string) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/curation/runs/"+run, nil)
	if status != 200 {
		a.t.Fatalf("run: %d %v", status, body)
	}
	return body["run"].(map[string]any)
}

func (a *app) setSetting(admin *http.Client, key, value string) {
	a.t.Helper()
	if status, out, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{key: value}}); status != 200 {
		a.t.Fatalf("setting %s: %d %v", key, status, out)
	}
}

func (a *app) bookRow(book string) (merged bool, files int) {
	a.t.Helper()
	if err := a.pool.QueryRow(context.Background(), `SELECT merged_into_id IS NOT NULL,
		(SELECT count(*) FROM book_files f WHERE f.book_id = $1 AND f.trashed_at IS NULL)::int FROM books WHERE id = $1`, book).Scan(&merged, &files); err != nil {
		a.t.Fatal(err)
	}
	return merged, files
}

func TestSameTextPairsAreMergedByARunAndTheRunUndone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	x, xFile := a.dupBook(admin, lib, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	y, yFile := a.dupBook(admin, lib, "Sternwanderer Kopie.pdf", "Der Sternwanderer", "Lena Brandt")
	if status, out, _ := a.call(admin, http.MethodPut, "/books/"+y+"/reading", map[string]any{"status": "reading"}); status != 200 {
		t.Fatalf("reading: %d %v", status, out)
	}
	pair := a.seedPair(x, y, "content", "ab")
	before := a.stateOf(x, y)

	if status, _ := a.startDuplicates(admin, nil, false); status != 422 {
		t.Errorf("a run of no class: %d", status)
	}
	if status, _ := a.startDuplicates(admin, []string{"person"}, false); status != 422 {
		t.Errorf("a run of a class the rules do not handle: %d", status)
	}
	run := a.runDuplicates(admin, []string{"same_text"}, false)

	if merged, _ := a.bookRow(x); !merged {
		t.Errorf("the book nobody reads was not merged away")
	}
	if merged, files := a.bookRow(y); merged || files != 1 {
		t.Errorf("the book someone reads: merged %v, %d live files", merged, files)
	}
	if got := a.pairState(pair); got != "replaced" {
		t.Errorf("the pair is %s", got)
	}
	if n := len(a.trash(admin).Files); n != 1 {
		t.Errorf("%d files in the trash, want the one copy", n)
	}
	info := a.runInfo(admin, run)
	if info["rule"] != "auto-duplicates" || info["automatic"] != true || info["dryRun"] != false || info["state"] != "done" || info["actions"] != 1.0 || info["skipped"] != 0.0 {
		t.Errorf("run: %v", info)
	}
	actions := a.runActions(admin, run)
	if len(actions) != 1 || actions[0]["kind"] != "replace" || actions[0]["rule"] != "same_text" || actions[0]["trashedFiles"] != 1.0 {
		t.Fatalf("actions: %v", actions)
	}
	if reason, _ := actions[0]["reason"].(string); !strings.Contains(reason, "reading state") || strings.Contains(reason, "Sternwanderer") {
		t.Errorf("the reason: %q", reason)
	}
	var root, rel string
	if err := a.pool.QueryRow(ctx, `SELECT l.root_path, f.rel_path FROM book_files f JOIN libraries l ON l.id = f.library_id WHERE f.trashed_at IS NOT NULL`).Scan(&root, &rel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
		t.Errorf("the trashed file is still in place: %v", err)
	}
	_ = xFile
	_ = yFile

	if outcomes := a.undoRun(admin, run); len(outcomes) != 1 || outcomes[0].(map[string]any)["state"] != "undone" {
		t.Fatalf("undo: %v", outcomes)
	}
	a.sameState("after undoing the run", before, a.stateOf(x, y))
	if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
		t.Errorf("the file is not back: %v", err)
	}
	if got := a.pairState(pair); got != "open" {
		t.Errorf("the pair after the undo is %s", got)
	}
}

func TestADryRunSaysWhatItWouldDoAndChangesNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	x, _ := a.dupBook(admin, lib, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	y, _ := a.dupBook(admin, lib, "Sternwanderer Kopie.pdf", "Der Sternwanderer", "Lena Brandt")
	z, _ := a.dupBook(admin, lib, "Mondkarte.pdf", "Die Mondkarte", "Lena Brandt")
	w, _ := a.dupBook(admin, lib, "Wetterbuch.pdf", "Das Wetterbuch", "Hanno Vogt")
	p1 := a.seedPair(x, y, "sha256", "ab")
	p2 := a.seedPair(z, w, "content", "cd")
	before := a.stateOf(x, y, z, w)

	run := a.runDuplicates(admin, []string{"same_text"}, true)
	a.sameState("after a dry run", before, a.stateOf(x, y, z, w))
	if a.pairState(p1) != "open" || a.pairState(p2) != "open" {
		t.Errorf("a pair changed in a dry run")
	}
	if n := len(a.trash(admin).Files); n != 0 {
		t.Errorf("a dry run trashed %d files", n)
	}
	info := a.runInfo(admin, run)
	if info["dryRun"] != true || info["state"] != "done" || info["actions"] != 2.0 {
		t.Fatalf("run: %v", info)
	}
	kinds := map[string]int{}
	for _, act := range a.runActions(admin, run) {
		kinds[act["kind"].(string)]++
		if act["reason"] == "" {
			t.Errorf("an action without a reason: %v", act)
		}
	}
	if kinds["replace"] != 1 || kinds["skip"] != 1 {
		t.Errorf("what the dry run would do: %v", kinds)
	}

	if status, _, _ := a.call(admin, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 409 {
		t.Errorf("undoing a dry run: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/curation/runs/"+run+"/confirm", nil); status != 409 {
		t.Errorf("confirming a dry run: %d", status)
	}
	action := a.runActions(admin, run)[0]["id"].(string)
	if status, out, _ := a.call(admin, http.MethodPost, "/curation/actions/"+action+"/undo", nil); status != 200 || out["state"] != "notUndoable" {
		t.Errorf("undoing a dry run's action: %d %v", status, out)
	}
}

func TestEachClassIsHandledAsTheTableSays(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	type seeded struct{ x, y, pair string }
	seed := func(n int, kind, detail string) seeded {
		x, _ := a.dupBook(admin, lib, fmt.Sprintf("Buch %d a.pdf", n), fmt.Sprintf("Buch %d", n), "Lena Brandt")
		y, _ := a.dupBook(admin, lib, fmt.Sprintf("Buch %d b.pdf", n), fmt.Sprintf("Buch %d", n), "Lena Brandt")
		return seeded{x, y, a.seedPair(x, y, kind, detail)}
	}
	pairs := map[string]seeded{
		"same_text":    seed(1, "sha256", "aa"),
		"overlap_high": seed(2, "overlap", "jaccard=0.97 a_in_b=0.98 b_in_a=0.99"),
		"overlap_mid":  seed(3, "overlap", "jaccard=0.85 a_in_b=0.9 b_in_a=0.9"),
		"contained":    seed(4, "overlap", "jaccard=0.20 a_in_b=0.10 b_in_a=0.97"),
		"part_inside":  seed(5, "overlap", "jaccard=0.30 a_in_b=0.88 b_in_a=0.10"),
		"person":       seed(6, "overlap", "jaccard=0.60 a_in_b=0.7 b_in_a=0.7"),
	}
	var all []string
	for _, p := range pairs {
		all = append(all, p.x, p.y)
	}
	before := a.stateOf(all...)

	status, body, _ := a.call(admin, http.MethodGet, "/curation/duplicates", nil)
	if status != 200 {
		t.Fatalf("classes: %d %v", status, body)
	}
	counts := map[string]float64{}
	for _, c := range body["classes"].([]any) {
		c := c.(map[string]any)
		counts[c["class"].(string)] = c["pairs"].(float64)
		if c["daily"] != false {
			t.Errorf("%v is daily without a setting", c)
		}
	}
	for _, class := range []string{"same_text", "overlap_high", "overlap_mid", "contained", "part_inside"} {
		if counts[class] != 1 {
			t.Errorf("%s counts %v pairs", class, counts[class])
		}
	}

	run := a.runDuplicates(admin, []string{"same_text", "overlap_high", "contained"}, false)
	for class, want := range map[string]string{
		"same_text": "replaced", "overlap_high": "replaced", "overlap_mid": "open", "contained": "kept_both", "part_inside": "open", "person": "open",
	} {
		if got := a.pairState(pairs[class].pair); got != want {
			t.Errorf("%s: the pair is %s, want %s", class, got, want)
		}
	}
	var kind string
	if err := a.pool.QueryRow(context.Background(), `SELECT kind FROM book_relations WHERE book_a = least($1::uuid, $2::uuid) AND book_b = greatest($1::uuid, $2::uuid)`,
		pairs["contained"].x, pairs["contained"].y).Scan(&kind); err != nil || kind != "contains" {
		t.Errorf("the relation of the contained pair: %q %v", kind, err)
	}
	for _, class := range []string{"overlap_mid", "part_inside", "person"} {
		p := pairs[class]
		if m, _ := a.bookRow(p.x); m {
			t.Errorf("%s was merged", class)
		}
		if m, _ := a.bookRow(p.y); m {
			t.Errorf("%s was merged", class)
		}
	}
	if info := a.runInfo(admin, run); info["actions"] != 3.0 {
		t.Errorf("run: %v", info)
	}

	for _, o := range a.undoRun(admin, run) {
		if o.(map[string]any)["state"] != "undone" {
			t.Errorf("undo: %v", o)
		}
	}
	a.sameState("after undoing the run of three classes", before, a.stateOf(all...))
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM book_relations`).Scan(&n); err != nil || n != 0 {
		t.Errorf("relations after the undo: %d %v", n, err)
	}

	run = a.runDuplicates(admin, []string{"part_inside", "overlap_mid"}, false)
	if a.pairState(pairs["part_inside"].pair) != "kept_both" || a.pairState(pairs["overlap_mid"].pair) != "replaced" {
		t.Errorf("the classes switched on later were not handled")
	}
	_ = run
}

func TestAPairWithAConflictWaitsForAPersonUnlessTheSettingMergesWhatAgrees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	x, _ := a.dupBook(admin, lib, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	y, _ := a.dupBook(admin, lib, "Mondkarte.pdf", "Die Mondkarte", "Lena Brandt")
	pair := a.seedPair(x, y, "content", "ab")
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+y, map[string]any{"description": "Eine Karte der Nacht."}); status != 200 {
		t.Fatalf("describe: %d %v", status, out)
	}
	before := a.stateOf(x, y)

	dry := a.runDuplicates(admin, []string{"same_text"}, true)
	if acts := a.runActions(admin, dry); len(acts) != 1 || acts[0]["kind"] != "skip" || !strings.Contains(acts[0]["reason"].(string), "title") {
		t.Errorf("the dry run: %v", acts)
	}
	run := a.runDuplicates(admin, []string{"same_text"}, false)
	a.sameState("a pair with a conflict", before, a.stateOf(x, y))
	if info := a.runInfo(admin, run); info["skipped"] != 1.0 || info["actions"] != 0.0 {
		t.Errorf("run: %v", info)
	}
	if a.pairState(pair) != "open" {
		t.Errorf("the pair is %s", a.pairState(pair))
	}

	if err := a.settings.Update(context.Background(), uuid.MustParse(adminID), map[string]*string{settings.DuplicateConflict: ptr("mergeAgreeing")}); err != nil {
		t.Fatal(err)
	}
	run = a.runDuplicates(admin, []string{"same_text"}, false)
	acts := a.runActions(admin, run)
	if len(acts) != 1 || !strings.Contains(acts[0]["reason"].(string), "kept its own title") {
		t.Fatalf("actions: %v", acts)
	}
	_, merged := a.bookRow(x)
	_ = merged
	survivor := x
	if m, _ := a.bookRow(x); m {
		survivor = y
	}
	_, book, _ := a.call(admin, http.MethodGet, "/books/"+survivor, nil)
	if book["description"] != "Eine Karte der Nacht." {
		t.Errorf("the description that agreed was not taken: %v", book["description"])
	}
	if title := book["title"]; title != "Der Sternwanderer" && title != "Die Mondkarte" {
		t.Errorf("the title: %v", title)
	}
	a.undoRun(admin, run)
	a.sameState("after undoing", before, a.stateOf(x, y))
}

func TestALockedFieldOfTheSurvivorOutlastsTheMerge(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	x, _ := a.dupBook(admin, lib, "Mondkarte.pdf", "", "")
	y, _ := a.dupBook(admin, lib, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	a.seedPair(x, y, "content", "ab")
	a.runDuplicates(admin, []string{"same_text"}, false)
	if m, _ := a.bookRow(x); !m {
		t.Fatalf("the book with only a guessed title survived")
	}
	_, book, _ := a.call(admin, http.MethodGet, "/books/"+y, nil)
	if book["title"] != "Der Sternwanderer" {
		t.Errorf("the title: %v", book["title"])
	}
	fields, _ := book["fields"].(map[string]any)
	if title, _ := fields["title"].(map[string]any); title["locked"] != true || title["source"] != "manual" {
		t.Errorf("the lock on the title: %v", fields["title"])
	}
}

func TestAPairInALibraryTheCallerDoesNotSeeIsNeverCountedOrTouched(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("edda", "editor")
	vault := a.library(admin, "Vault", "private")
	x, _ := a.dupBook(admin, vault, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	y, _ := a.dupBook(admin, vault, "Sternwanderer Kopie.pdf", "Der Sternwanderer", "Lena Brandt")
	pair := a.seedPair(x, y, "content", "ab")
	before := a.stateOf(x, y)

	_, body, _ := a.call(editor, http.MethodGet, "/curation/duplicates", nil)
	for _, c := range body["classes"].([]any) {
		if c.(map[string]any)["pairs"] != 0.0 {
			t.Errorf("the editor is told of a private pair: %v", c)
		}
	}
	status, out := a.startDuplicates(editor, []string{"same_text"}, false)
	if status != 202 || out["pairs"] != 0.0 {
		t.Fatalf("start: %d %v", status, out)
	}
	run := out["runId"].(string)
	for {
		done, err := a.server.AutoDuplicates.Work(context.Background(), uuid.MustParse(run))
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	a.sameState("after a run of someone outside the library", before, a.stateOf(x, y))
	if a.pairState(pair) != "open" {
		t.Errorf("the pair is %s", a.pairState(pair))
	}
	_, body, _ = a.call(admin, http.MethodGet, "/curation/duplicates", nil)
	if body["classes"].([]any)[0].(map[string]any)["pairs"] != 1.0 {
		t.Errorf("the administrator's count: %v", body)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/curation/runs/"+run+"/undo", nil); status != 202 {
		t.Errorf("an editor undoes their own empty run: %d", status)
	}
}

func TestTheDailyRunHandlesThePairsOfTheClassesTheSettingsName(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Novels", "shared")
	x, _ := a.dupBook(admin, lib, "Sternwanderer.pdf", "Der Sternwanderer", "Lena Brandt")
	y, _ := a.dupBook(admin, lib, "Sternwanderer Kopie.pdf", "Der Sternwanderer", "Lena Brandt")
	a.seedPair(x, y, "sha256", "ab")

	if err := a.server.AutoDuplicates.Daily(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM curation_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a run with no class set: %d %v", n, err)
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.DuplicateClasses: "same_text,nonsense"}}); status != 422 {
		t.Errorf("a class that does not exist: %d %v", status, out)
	}
	a.setSetting(admin, settings.DuplicateClasses, "same_text, contained")
	_, body, _ := a.call(admin, http.MethodGet, "/curation/duplicates", nil)
	daily := map[string]bool{}
	for _, c := range body["classes"].([]any) {
		daily[c.(map[string]any)["class"].(string)] = c.(map[string]any)["daily"] == true
	}
	if !daily["same_text"] || !daily["contained"] || daily["overlap_high"] {
		t.Errorf("daily: %v", daily)
	}
	for range 2 {
		if err := a.server.AutoDuplicates.Daily(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var run string
	if err := a.pool.QueryRow(ctx, `SELECT id::text FROM curation_runs`).Scan(&run); err != nil {
		t.Fatalf("the daily run, queued twice while it waits: %v", err)
	}
	for {
		done, err := a.server.AutoDuplicates.Work(ctx, uuid.MustParse(run))
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	info := a.runInfo(admin, run)
	if info["rule"] != "auto-duplicates" || info["actions"] != 1.0 || info["startedBy"] != nil {
		t.Errorf("run: %v", info)
	}
	if err := a.server.AutoDuplicates.Daily(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM curation_runs WHERE state IN ('planned', 'running')`).Scan(&left); err != nil || left != 1 {
		t.Errorf("a next day's run: %d %v", left, err)
	}
	// Nothing to do: the run leaves no trace.
	next := ""
	if err := a.pool.QueryRow(ctx, `SELECT id::text FROM curation_runs WHERE state = 'planned'`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	for {
		done, err := a.server.AutoDuplicates.Work(ctx, uuid.MustParse(next))
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM curation_runs`).Scan(&left); err != nil || left != 1 {
		t.Errorf("runs after an empty day's: %d %v", left, err)
	}
}

func TestOnlyOneRunOfTheRulesGoesAtATime(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	if status, out := a.startDuplicates(admin, []string{"same_text"}, true); status != 202 {
		t.Fatalf("start: %d %v", status, out)
	}
	if status, _ := a.startDuplicates(admin, []string{"same_text"}, true); status != 409 {
		t.Errorf("a second run while the first waits: %d", status)
	}
}
