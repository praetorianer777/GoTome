//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// organiseShelf is a managed library with books lying in a messy folder.
type organiseShelf struct {
	lib  string
	root string
}

func (a *app) putFile(root, rel, content string) {
	a.t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

// describeFile gives the book of the file at rel the details a pattern is
// made from.
func (a *app) describeFile(rel, title, author, series string, index *float64) {
	a.t.Helper()
	ctx := context.Background()
	var book uuid.UUID
	if err := a.pool.QueryRow(ctx, `SELECT book_id FROM book_files WHERE rel_path = $1`, rel).Scan(&book); err != nil {
		a.t.Fatalf("%s: %v", rel, err)
	}
	var seriesID *uuid.UUID
	if series != "" {
		var id uuid.UUID
		if err := a.pool.QueryRow(ctx, `INSERT INTO series (name, name_key) VALUES ($1, lower($1))
			ON CONFLICT (name_key) DO UPDATE SET name = EXCLUDED.name RETURNING id`, series).Scan(&id); err != nil {
			a.t.Fatal(err)
		}
		seriesID = &id
	}
	if _, err := a.pool.Exec(ctx, `UPDATE books SET title = $2, series_id = $3, series_index = $4 WHERE id = $1`, book, title, seriesID, index); err != nil {
		a.t.Fatal(err)
	}
	var authorID uuid.UUID
	if err := a.pool.QueryRow(ctx, `INSERT INTO authors (name, sort_name, name_key) VALUES ($1, $1, lower($1))
		ON CONFLICT (name_key) DO UPDATE SET name = EXCLUDED.name RETURNING id`, author).Scan(&authorID); err != nil {
		a.t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO book_contributors (book_id, author_id, role, position) VALUES ($1, $2, 'author', 0)
		ON CONFLICT DO NOTHING`, book, authorID); err != nil {
		a.t.Fatal(err)
	}
}

func (a *app) organiseShelf(admin *http.Client) organiseShelf {
	a.t.Helper()
	lib := a.library(admin, "Archive", "shared")
	var root string
	if err := a.pool.QueryRow(context.Background(), `SELECT root_path FROM libraries WHERE id = $1`, lib).Scan(&root); err != nil {
		a.t.Fatal(err)
	}
	return organiseShelf{lib: lib, root: root}
}

// filePaths is every file's record: path by ID.
func (a *app) filePaths() map[string]string {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), `SELECT id::text, rel_path FROM book_files`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, rel string
		if err := rows.Scan(&id, &rel); err != nil {
			a.t.Fatal(err)
		}
		out[id] = rel
	}
	return out
}

// organise starts a run and works its job to the end; it returns the run.
func (a *app) organise(c *http.Client, lib string) string {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/libraries/"+lib+"/organise", map[string]any{})
	if status != 202 {
		a.t.Fatalf("organise: %d %v", status, body)
	}
	run := body["runId"].(string)
	for range 5 {
		done, _, err := a.scans.WorkOrganise(context.Background(), uuid.MustParse(run))
		if err != nil {
			a.t.Fatal(err)
		}
		if done {
			return run
		}
	}
	a.t.Fatal("the run does not finish")
	return ""
}

func (a *app) previewOrganise(c *http.Client, lib string, pattern string) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodPost, "/libraries/"+lib+"/organise/preview", map[string]any{"pattern": pattern})
	if status != 200 {
		a.t.Fatalf("preview: %d %v", status, body)
	}
	return body
}

func TestOrganisingMovesFilesByTheirMetadataAndCanBeUndone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	s := a.organiseShelf(admin)
	two := 2.0
	a.putFile(s.root, "inbox/emma.pdf", "%PDF-1.4 emma in a series")
	a.putFile(s.root, "inbox/emma.jpg", "cover")
	a.putFile(s.root, "inbox/emma.sdr/metadata.pdf.lua", "return {}")
	a.putFile(s.root, "inbox/emma-b.pdf", "%PDF-1.4 emma, second copy")
	a.putFile(s.root, "inbox/emma-c.pdf", "%PDF-1.4 emma, third copy")
	a.putFile(s.root, "inbox/readme.md", "not a book")
	a.putFile(s.root, "old/persuasion.pdf", "%PDF-1.4 persuasion")
	a.scanNow(s.lib)
	a.describeFile("inbox/emma.pdf", "Emma", "Jane Austen", "Austen Novels", &two)
	a.describeFile("inbox/emma-b.pdf", "Emma", "Jane Austen", "", nil)
	a.describeFile("inbox/emma-c.pdf", "Emma", "Jane Austen", "", nil)
	a.describeFile("old/persuasion.pdf", "Persuasion", "Jane Austen", "", nil)
	ids := a.filePaths()
	idOf := func(rel string) string {
		for id, p := range ids {
			if p == rel {
				return id
			}
		}
		t.Fatalf("no file at %s", rel)
		return ""
	}

	preview := a.previewOrganise(admin, s.lib, "")
	if preview["allowed"] != true || preview["moves"] != 4.0 || preview["collisions"] != 1.0 || preview["unchanged"] != 0.0 || preview["skipped"] != 0.0 {
		t.Fatalf("preview: %v", preview)
	}
	if preview["pattern"] != "{author}/[{series}/][{series_index} - ]{title}.{ext}" {
		t.Errorf("pattern: %v", preview["pattern"])
	}
	if onDisk(filepath.Join(s.root, "Jane Austen")) {
		t.Error("the preview moved files")
	}

	run := a.organise(admin, s.lib)
	now := a.filePaths()
	want := map[string]bool{
		"Jane Austen/Austen Novels/02 - Emma.pdf": true, "Jane Austen/Emma.pdf": true,
		"Jane Austen/Emma (2).pdf": true, "Jane Austen/Persuasion.pdf": true,
	}
	for _, p := range now {
		if !want[p] {
			t.Errorf("unexpected path %s", p)
		}
		delete(want, p)
		if !onDisk(filepath.Join(s.root, filepath.FromSlash(p))) {
			t.Errorf("%s is not on disk", p)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing paths %v", want)
	}
	if now[idOf("inbox/emma.pdf")] != "Jane Austen/Austen Novels/02 - Emma.pdf" {
		t.Errorf("the series book is at %s", now[idOf("inbox/emma.pdf")])
	}
	moved := func(rel string) bool { return onDisk(filepath.Join(s.root, filepath.FromSlash(rel))) }
	if !moved("Jane Austen/Austen Novels/02 - Emma.jpg") || !moved("Jane Austen/Austen Novels/02 - Emma.sdr/metadata.pdf.lua") {
		t.Error("the cover and the KOReader folder did not move along")
	}
	if moved("inbox/emma.jpg") || moved("inbox/emma.sdr") {
		t.Error("the sidecars were left behind")
	}
	if !moved("inbox/readme.md") || !moved("inbox") {
		t.Error("the folder with another file in it was removed or emptied")
	}
	if moved("old") {
		t.Error("the folder GOtome emptied stayed")
	}

	// The scan finds the same files in their new places.
	a.scanNow(s.lib)
	after := a.filePaths()
	if len(after) != len(now) {
		t.Fatalf("the scan changed the files: %v -> %v", now, after)
	}
	for id, p := range now {
		if after[id] != p {
			t.Errorf("file %s: %s -> %s", id, p, after[id])
		}
	}
	var missing, books int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE missing_at IS NOT NULL), (SELECT count(*) FROM books) FROM book_files`).Scan(&missing, &books); err != nil {
		t.Fatal(err)
	}
	if missing != 0 || books != 4 {
		t.Errorf("%d missing files, %d books", missing, books)
	}

	// Repeating changes nothing.
	if again := a.previewOrganise(admin, s.lib, ""); again["moves"] != 0.0 || again["unchanged"] != 4.0 {
		t.Errorf("a second preview: %v", again)
	}

	// The run is undone: every file, with its sidecars, goes back.
	runs := a.runs(admin)
	if runs[0]["id"] != run || runs[0]["kind"] != "organise" || runs[0]["actions"] != 4.0 {
		t.Fatalf("runs: %v", runs[0])
	}
	for _, o := range a.undoRun(admin, run) {
		if o.(map[string]any)["state"] != "undone" {
			t.Errorf("undo: %v", o)
		}
	}
	back := a.filePaths()
	for id, p := range ids {
		if back[id] != p {
			t.Errorf("file %s is at %s, was %s", id, back[id], p)
		}
		if !moved(p) {
			t.Errorf("%s is not back on disk", p)
		}
	}
	if !moved("inbox/emma.jpg") || !moved("inbox/emma.sdr/metadata.pdf.lua") || !moved("old") {
		t.Error("the sidecars or the emptied folder did not come back")
	}
	if moved("Jane Austen") {
		t.Error("the folders the run made stayed")
	}
	a.scanNow(s.lib)
	if got := a.filePaths(); len(got) != len(ids) {
		t.Errorf("the scan after the undo: %v", got)
	}
}

func TestAnExternalLibraryIsOrganisedOnlyWhenOptedIn(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	folder := t.TempDir()
	a.putFile(folder, "loose/emma.pdf", "%PDF-1.4 emma")
	lib := a.externalLibrary(admin, "Archive", "shared", folder)
	a.scanNow(lib)
	a.describeFile("loose/emma.pdf", "Emma", "Jane Austen", "", nil)

	if p := a.previewOrganise(admin, lib, ""); p["allowed"] != false || p["blocked"] != "read-only" {
		t.Errorf("read-only: %v", p)
	}
	if status, _, _ := a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"writable": true}); status != 200 {
		t.Fatalf("writable: %d", status)
	}
	if p := a.previewOrganise(admin, lib, ""); p["allowed"] != false || p["blocked"] != "off" {
		t.Errorf("not opted in: %v", p)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/libraries/"+lib+"/organise", map[string]any{}); status != 409 {
		t.Errorf("organise without the opt-in: %d", status)
	}
	if !onDisk(filepath.Join(folder, "loose", "emma.pdf")) {
		t.Fatal("the file moved")
	}

	status, body, _ := a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"organiseEnabled": true, "organisePattern": "{title} - {author}.{ext}"})
	if status != 200 || body["organiseEnabled"] != true || body["organisePattern"] != "{title} - {author}.{ext}" {
		t.Fatalf("opt in: %d %v", status, body)
	}
	a.organise(admin, lib)
	if !onDisk(filepath.Join(folder, "Emma - Jane Austen.pdf")) || onDisk(filepath.Join(folder, "loose")) {
		t.Errorf("after: %v", a.filePaths())
	}
	status, body, _ = a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"organisePattern": "{author}/{title}"})
	if status != 422 || fieldError(body, "organisePattern") == "" {
		t.Errorf("a pattern without {ext}: %d %v", status, body)
	}
	status, body, _ = a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"organisePattern": ""})
	if status != 200 || body["organisePattern"] != "{author}/[{series}/][{series_index} - ]{title}.{ext}" {
		t.Errorf("reset: %d %v", status, body)
	}
}

func TestAFailedRenameIsTakenBackAndTheRestGoesOn(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	s := a.organiseShelf(admin)
	a.putFile(s.root, "a/emma.pdf", "%PDF-1.4 emma")
	a.putFile(s.root, "a/emma.jpg", "cover")
	a.putFile(s.root, "b/iliad.pdf", "%PDF-1.4 iliad")
	a.scanNow(s.lib)
	a.describeFile("a/emma.pdf", "Emma", "Jane Austen", "", nil)
	a.describeFile("b/iliad.pdf", "Iliad", "Homer", "", nil)
	mkdirs(t, s.root, "Jane Austen")
	readOnly(t, filepath.Join(s.root, "Jane Austen"))
	before := a.filePaths()

	run := a.organise(admin, s.lib)

	after := a.filePaths()
	for id, p := range before {
		switch p {
		case "a/emma.pdf":
			if after[id] != p || !onDisk(filepath.Join(s.root, "a", "emma.pdf")) || !onDisk(filepath.Join(s.root, "a", "emma.jpg")) {
				t.Errorf("the refused book moved: %s", after[id])
			}
		case "b/iliad.pdf":
			if after[id] != "Homer/Iliad.pdf" || !onDisk(filepath.Join(s.root, "Homer", "Iliad.pdf")) {
				t.Errorf("the other book did not move: %s", after[id])
			}
		}
	}
	actions := a.runActions(admin, run)
	var organised, skipped int
	for _, act := range actions {
		switch act["kind"] {
		case "organise":
			organised++
		case "skip":
			skipped++
			if act["reason"] == "" {
				t.Error("a failed book has no reason")
			}
		}
	}
	if organised != 1 || skipped != 1 {
		t.Errorf("actions: %v", actions)
	}
	if entries := entries(t, filepath.Join(s.root, "Jane Austen")); len(entries) != 0 {
		t.Errorf("the refused folder holds %v", entries)
	}
}

func TestAudioPartsStayTogetherInOrder(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	s := a.organiseShelf(admin)
	for _, name := range []string{"cd2.mp3", "cd1.mp3", "cd10.mp3"} {
		a.putFile(s.root, "tapes/"+name, "ID3 "+name)
	}
	a.scanNow(s.lib)
	var book uuid.UUID
	if err := a.pool.QueryRow(context.Background(), `SELECT book_id FROM book_files LIMIT 1`).Scan(&book); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"cd1.mp3", "cd2.mp3", "cd10.mp3"} {
		if _, err := a.pool.Exec(context.Background(), `UPDATE book_files SET part_index = $2 WHERE rel_path = $1`, "tapes/"+name, i); err != nil {
			t.Fatal(err)
		}
	}
	a.describeFile("tapes/cd1.mp3", "Odyssey", "Homer", "", nil)

	a.organise(admin, s.lib)

	var got []string
	for _, p := range a.filePaths() {
		got = append(got, p)
	}
	slices.Sort(got)
	want := []string{"Homer/Odyssey/01 - Odyssey.mp3", "Homer/Odyssey/02 - Odyssey.mp3", "Homer/Odyssey/03 - Odyssey.mp3"}
	if !slices.Equal(got, want) {
		t.Fatalf("paths %v, want %v", got, want)
	}
	data, err := os.ReadFile(filepath.Join(s.root, "Homer", "Odyssey", "01 - Odyssey.mp3"))
	if err != nil || string(data) != "ID3 cd1.mp3" {
		t.Errorf("part one holds %q (%v)", data, err)
	}
	data, _ = os.ReadFile(filepath.Join(s.root, "Homer", "Odyssey", "03 - Odyssey.mp3"))
	if string(data) != "ID3 cd10.mp3" {
		t.Errorf("part three holds %q", data)
	}
}
