//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// mkdirs makes the folders under root.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// refusedAt checks that an answer is the 409 of a folder GOtome cannot
// write to, naming the folder and the system's reason.
func refusedAt(t *testing.T, what string, status int, body map[string]any, folder string) {
	t.Helper()
	msg := errorMessage(body)
	if status != 409 || !strings.Contains(msg, folder) || !strings.Contains(msg, "permission denied") {
		t.Errorf("%s: %d %v, want 409 naming %s", what, status, body, folder)
	}
}

// markedFor checks that the library is marked as one whose folder cannot be
// written, for the reason given.
func (a *app) markedFor(admin *http.Client, lib, reason string) {
	a.t.Helper()
	_, body, _ := a.call(admin, http.MethodGet, "/libraries/"+lib, nil)
	if body["unwritableSince"] == nil || body["unwritableReason"] != reason {
		a.t.Errorf("the library is %v, want it marked for %q", body, reason)
	}
}

func TestATrashOrRestoreAFolderRefusesIsAConflict(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	mkdirs(t, folder, "Austen", "Others")
	writeEPUB(t, filepath.Join(folder, "Austen", "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	writeEPUB(t, filepath.Join(folder, "Others", "Persuasion.epub"), emmaMetadata, nil, "Anne Elliot.")
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	files := a.shelfFiles()
	emma, persuasion := files["Austen/Emma.epub"], files["Others/Persuasion.epub"]
	austen := filepath.Join(folder, "Austen")
	trash := func(id string) (int, map[string]any) {
		status, body, _ := a.call(editor, http.MethodPost, "/files/"+id+"/trash", nil)
		return status, body
	}

	// The library's folder can be written, its author's folder cannot: the
	// probe passes and the rename is refused.
	writable := readOnly(t, austen)
	status, body := trash(emma.ID.String())
	refusedAt(t, "trashing from a folder that cannot be written", status, body, austen)
	a.markedFor(admin, lib, "permission denied ("+austen+")")
	if _, err := os.Stat(filepath.Join(austen, "Emma.epub")); err != nil {
		t.Errorf("the file moved: %v", err)
	}
	if list := a.trash(editor); len(list.Files) != 0 {
		t.Errorf("the trash holds %+v", list.Files)
	}
	if _, err := os.Stat(filepath.Join(folder, ".trash", emma.ID.String())); err == nil {
		t.Error("the refused move left its folder in the trash")
	}

	// A trash folder that is someone else's refuses the mkdir.
	writable()
	a.scanNow(lib)
	trashDir := filepath.Join(folder, ".trash")
	readOnly(t, trashDir)
	status, body = trash(persuasion.ID.String())
	refusedAt(t, "trashing into a trash that cannot be written", status, body, trashDir)
	if _, err := os.Stat(filepath.Join(folder, "Others", "Persuasion.epub")); err != nil {
		t.Errorf("the file moved: %v", err)
	}
	if err := os.Chmod(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a.scanNow(lib)

	// Restoring into a folder that cannot be written.
	if status, body := trash(emma.ID.String()); status != 204 {
		t.Fatalf("trash: %d %v", status, body)
	}
	readOnly(t, austen)
	status, body, _ = a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/restore", nil)
	refusedAt(t, "restoring into a folder that cannot be written", status, body, austen)
	if list := a.trash(editor); len(list.Files) != 1 || list.Files[0].ID != emma.ID.String() {
		t.Errorf("the trash holds %+v", list.Files)
	}
	if _, err := os.Stat(filepath.Join(trashDir, emma.ID.String(), "Emma.epub")); err != nil {
		t.Errorf("the file left the trash: %v", err)
	}
}

func TestAReplaceAFolderRefusesChangesNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	mkdirs(t, folder, "Kept", "Locked")
	writeEPUB(t, filepath.Join(folder, "Kept", "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	writeEPUB(t, filepath.Join(folder, "Locked", "Emma.epub"), emmaMetadata, nil, "Emma Woodhouse.")
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	files := a.shelfFiles()
	keep, gone := files["Kept/Emma.epub"].BookID.String(), files["Locked/Emma.epub"].BookID.String()
	x, y := keep, gone
	if y < x {
		x, y = y, x
	}
	var pair string
	if err := a.pool.QueryRow(ctx, `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
		INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, 'title_author', 'emma / austen' FROM p RETURNING pair_id`, x, y).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(folder, "Locked")
	writable := readOnly(t, locked)

	status, body, _ := a.call(editor, http.MethodPost, "/duplicates/"+pair+"/replace", map[string]any{"keep": keep})
	refusedAt(t, "replacing a book whose folder cannot be written", status, body, locked)
	a.markedFor(admin, lib, "permission denied ("+locked+")")
	if _, book, _ := a.call(editor, http.MethodGet, "/books/"+gone, nil); book["id"] != gone {
		t.Errorf("the other book became %v", book["id"])
	}
	var state string
	if err := a.pool.QueryRow(ctx, "SELECT state FROM duplicate_pairs WHERE id = $1", pair).Scan(&state); err != nil || state != "open" {
		t.Errorf("the pair: %q %v", state, err)
	}
	if list := a.trash(editor); len(list.Files) != 0 {
		t.Errorf("the trash holds %+v", list.Files)
	}
	if _, err := os.Stat(filepath.Join(locked, "Emma.epub")); err != nil {
		t.Errorf("the file moved: %v", err)
	}

	// Once the folder can be written, the same click works.
	writable()
	status, body, _ = a.call(editor, http.MethodPost, "/duplicates/"+pair+"/replace", map[string]any{"keep": keep})
	if status != 204 {
		t.Fatalf("replace once writable: %d %v", status, body)
	}
	if list := a.trash(editor); len(list.Files) != 1 {
		t.Errorf("the trash holds %+v", list.Files)
	}
}

func TestAFixAFolderRefusesIsAConflict(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	mkdirs(t, folder, "Locked")
	path := filepath.Join(folder, "Locked", "Heavy.epub")
	original := heavyEPUB(t, "Heavy")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	file := a.shelfFiles()["Locked/Heavy.epub"]
	a.extract(file.ID)
	locked := filepath.Join(folder, "Locked")
	readOnly(t, locked)

	status, body, _ := a.call(editor, http.MethodPost, "/files/"+file.ID.String()+"/fix", map[string]any{"findings": []string{epub.LargeFonts}})
	refusedAt(t, "fixing a file whose folder cannot be written", status, body, locked)
	a.markedFor(admin, lib, "permission denied ("+locked+")")
	if now, _ := os.ReadFile(path); !bytes.Equal(now, original) {
		t.Error("the file was changed")
	}
	if names := entries(t, locked); len(names) != 1 {
		t.Errorf("the folder holds %v", names)
	}
}
