//go:build integration

package test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// readOnly takes the right to add files away from a folder for the rest of
// the test, as a mount with :ro would, or until the returned func gives it
// back.
func readOnly(t *testing.T, folder string) (writable func()) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root writes into every folder")
	}
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	writable = func() { _ = os.Chmod(folder, 0o755) }
	t.Cleanup(writable)
	return writable
}

func entries(t *testing.T, folder string) []string {
	t.Helper()
	list, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestAReadOnlyFolderIsNotMadeWritable(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	folder := t.TempDir()
	writable := readOnly(t, folder)

	status, body, _ := a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": "NAS", "mode": "external", "rootPath": folder, "visibility": "shared", "writable": true,
	})
	if msg := fieldError(body, "writable"); status != 422 || !strings.Contains(msg, folder) || !strings.Contains(msg, ":ro") {
		t.Errorf("a writable library on a read-only folder: %d %v", status, body)
	}

	id := a.externalLibrary(admin, "NAS", "shared", folder)
	status, body, _ = a.call(admin, http.MethodPatch, "/libraries/"+id, map[string]any{"writable": true})
	if msg := fieldError(body, "writable"); status != 422 || !strings.Contains(msg, folder) || !strings.Contains(msg, "permission denied") {
		t.Errorf("making a read-only folder's library writable: %d %v", status, body)
	}
	if _, lib, _ := a.call(admin, http.MethodGet, "/libraries/"+id, nil); lib["writable"] != false {
		t.Errorf("the refused library is %v", lib)
	}

	writable()
	if status, body, _ := a.call(admin, http.MethodPatch, "/libraries/"+id, map[string]any{"writable": true}); status != 200 || body["writable"] != true {
		t.Errorf("making a writable folder's library writable: %d %v", status, body)
	}
	if names := entries(t, folder); len(names) != 0 {
		t.Errorf("the check left %v in the folder", names)
	}
}

func TestAFolderThatTurnedReadOnlyIsToldOnce(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	events := a.withEvents()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	for _, name := range []string{"Emma.epub", "Persuasion.epub"} {
		writeEPUB(t, filepath.Join(folder, name), emmaMetadata, nil, "Emma Woodhouse.")
	}
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}
	a.flush(events)
	if names := entries(t, folder); len(names) != 2 {
		t.Fatalf("after a scan the folder holds %v", names)
	}

	writable := readOnly(t, folder)
	a.scanNow(lib)
	_, body, _ := a.call(admin, http.MethodGet, "/libraries/"+lib, nil)
	if body["unwritableSince"] == nil || body["unwritableReason"] != "permission denied" || body["writable"] != true {
		t.Errorf("the library after the scan: %v", body)
	}

	// Every file's write is passed by, and none of them fails.
	original := map[string][]byte{}
	for name, f := range files {
		original[name], _ = os.ReadFile(filepath.Join(folder, name))
		if status, body, _ := a.call(editor, http.MethodPatch, "/books/"+f.BookID.String(), map[string]any{"title": "Changed " + name}); status != 200 {
			t.Fatalf("edit: %d %v", status, body)
		}
		if err := a.writeBack(f.ID); err != nil {
			t.Errorf("writing %s into a read-only folder: %v", name, err)
		}
		if now, _ := os.ReadFile(filepath.Join(folder, name)); !bytes.Equal(now, original[name]) {
			t.Errorf("%s was changed", name)
		}
	}
	emma := files["Emma.epub"]
	status, body, _ := a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/trash", nil)
	if e, _ := body["error"].(map[string]any); status != 409 || !strings.Contains(e["message"].(string), "permission denied") {
		t.Errorf("trashing from a read-only folder: %d %v", status, body)
	}
	if a.shelfFiles()["Emma.epub"].ID != emma.ID {
		t.Error("the file was trashed")
	}
	a.scanNow(lib)

	a.flush(events)
	var unwritable []told
	for _, n := range a.told(admin) {
		if n.Kind == "library.unwritable" {
			unwritable = append(unwritable, n)
		}
	}
	if len(unwritable) != 1 || unwritable[0].Data["library"] != "Shelf" || unwritable[0].Data["error"] != "permission denied" {
		t.Errorf("the administrator is told %v", unwritable)
	}
	if got := a.told(editor); slices.Contains(kindsOf(got), "library.unwritable") {
		t.Errorf("the editor is told %v", got)
	}
	// Writable again: the next scan finds it, and the trash works.
	writable()
	a.scanNow(lib)
	if _, body, _ := a.call(admin, http.MethodGet, "/libraries/"+lib, nil); body["unwritableSince"] != nil {
		t.Errorf("the library once its folder is writable again: %v", body)
	}
	if status, body, _ := a.call(editor, http.MethodPost, "/files/"+emma.ID.String()+"/trash", nil); status != 204 {
		t.Errorf("trashing once the folder is writable: %d %v", status, body)
	}
	if names := entries(t, folder); !slices.Equal(names, []string{".trash", "Persuasion.epub"}) {
		t.Errorf("the folder holds %v", names)
	}
}

func TestAnEditWhileReadOnlyIsWrittenOnceTheFolderIsWritable(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	for _, name := range []string{"Emma.epub", "Persuasion.epub"} {
		writeEPUB(t, filepath.Join(folder, name), emmaMetadata, nil, "Emma Woodhouse.")
	}
	lib := a.writableLibrary(admin, "Shelf", folder)
	a.scanNow(lib)
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}
	emma, persuasion := files["Emma.epub"], files["Persuasion.epub"]
	emmaPath := filepath.Join(folder, "Emma.epub")

	writable := readOnly(t, folder)
	if status, body, _ := a.call(editor, http.MethodPatch, "/books/"+emma.BookID.String(), map[string]any{"title": "Emma (Annotated)"}); status != 200 {
		t.Fatalf("edit: %d %v", status, body)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if title := readEPUB(t, emmaPath).Metadata.Title; title != "Emma" {
		t.Fatalf("a read-only folder's file says %q", title)
	}
	if n := a.writeJobs(emma.ID); n != 1 {
		t.Fatalf("%d writes queued while read-only, want the edit's", n)
	}

	// Found writable at the next scan: the write passed by is queued again,
	// once, and the file that was not passed by is left alone.
	writable()
	a.scanNow(lib)
	a.scanNow(lib)
	if n := a.writeJobs(emma.ID); n != 2 {
		t.Errorf("%d writes queued after the folder is writable, want 2", n)
	}
	if n := a.writeJobs(persuasion.ID); n != 0 {
		t.Errorf("%d writes queued for a file nobody changed", n)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if title := readEPUB(t, emmaPath).Metadata.Title; title != "Emma (Annotated)" {
		t.Errorf("once writable the file says %q", title)
	}
	a.scanNow(lib)
	if n := a.writeJobs(emma.ID); n != 2 {
		t.Errorf("%d writes queued after the file was written, want 2", n)
	}

	// Made writable again by hand instead of by a scan.
	writable = readOnly(t, folder)
	if status, body, _ := a.call(editor, http.MethodPatch, "/books/"+emma.BookID.String(), map[string]any{"title": "Emma (Second)"}); status != 200 {
		t.Fatalf("edit: %d %v", status, body)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	writable()
	if status, body, _ := a.call(admin, http.MethodPatch, "/libraries/"+lib, map[string]any{"writable": true}); status != 200 {
		t.Fatalf("make writable: %d %v", status, body)
	}
	if n := a.writeJobs(emma.ID); n != 4 {
		t.Errorf("%d writes queued after the library was made writable, want 4", n)
	}
	if err := a.writeBack(emma.ID); err != nil {
		t.Fatal(err)
	}
	if title := readEPUB(t, emmaPath).Metadata.Title; title != "Emma (Second)" {
		t.Errorf("once made writable the file says %q", title)
	}
}
