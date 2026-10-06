//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// withEvents has the app's scans and duplicate checks tell of what happens,
// as serve wires them, and returns the events to flush.
func (a *app) withEvents() *notify.Events {
	a.t.Helper()
	events := &notify.Events{Pool: a.pool, Queue: a.server.Duplicates.Queue}
	a.scans.Events = events
	a.server.Duplicates.Events = events
	return events
}

// flush tells every waiting event now.
func (a *app) flush(events *notify.Events) {
	a.t.Helper()
	if _, err := events.Flush(context.Background(), time.Now().Add(time.Hour), true); err != nil {
		a.t.Fatal(err)
	}
}

type told struct {
	Kind string
	Data map[string]any
	Link string
}

func (a *app) told(c *http.Client) []told {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/notifications", nil)
	if status != 200 {
		a.t.Fatalf("notifications: %d %v", status, out)
	}
	var list []told
	for _, n := range out["notifications"].([]any) {
		n := n.(map[string]any)
		list = append(list, told{Kind: n["kind"].(string), Data: n["data"].(map[string]any), Link: n["link"].(string)})
	}
	slices.SortFunc(list, func(x, y told) int {
		return strings.Compare(x.Kind+fmt.Sprint(x.Data["library"]), y.Kind+fmt.Sprint(y.Data["library"]))
	})
	return list
}

func kindsOf(list []told) []string {
	var out []string
	for _, n := range list {
		out = append(out, n.Kind)
	}
	return out
}

func TestAnImportIsToldOnceToThoseWhoWantToHearOfIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	events := a.withEvents()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	quiet, _ := a.signedIn("quiet", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	if status, out, _ := a.call(quiet, http.MethodPut, "/me/notification-settings", map[string]any{
		"kinds": []map[string]any{{"kind": "books.added", "app": false}},
	}); status != 200 {
		t.Fatalf("settings: %d %v", status, out)
	}

	folder := t.TempDir()
	for i := range 5 {
		writeEPUB(t, filepath.Join(folder, fmt.Sprintf("Book %d.epub", i)),
			fmt.Sprintf("<dc:title>Book %d</dc:title>", i), nil, "Some text.")
	}
	for _, name := range []string{"Broken.epub", "Torn.epub"} {
		os.WriteFile(filepath.Join(folder, name), []byte("not a zip"), 0o644)
	}
	shared := a.externalLibrary(admin, "Shelf", "shared", folder)
	vaultFolder := t.TempDir()
	writeEPUB(t, filepath.Join(vaultFolder, "Secret.epub"), "<dc:title>Secret</dc:title>", nil, "Hidden.")
	private := a.externalLibrary(admin, "Vault", "private", vaultFolder)
	a.scanNow(shared)
	a.scanNow(private)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	a.flush(events)

	// Seven books, the two unreadable files among them: one notification
	// of the books, one of the files.
	shelf := fmt.Sprint(told{Kind: "books.added", Data: map[string]any{"library": "Shelf", "count": 7.0}, Link: "/?library=" + shared})
	vault := fmt.Sprint(told{Kind: "books.added", Data: map[string]any{"library": "Vault", "count": 1.0}, Link: "/?library=" + private})
	files := fmt.Sprint(told{Kind: "files.unreadable", Data: map[string]any{"count": 2.0}, Link: "/jobs"})
	str := func(list []told) []string {
		var out []string
		for _, n := range list {
			out = append(out, fmt.Sprint(n))
		}
		return out
	}
	if got, want := str(a.told(admin)), []string{shelf, vault, files}; !slices.Equal(got, want) {
		t.Errorf("the administrator is told\n  %v\nwant\n  %v", got, want)
	}
	if got, want := str(a.told(editor)), []string{shelf, files}; !slices.Equal(got, want) {
		t.Errorf("the editor is told\n  %v\nwant\n  %v", got, want)
	}
	for name, c := range map[string]*http.Client{"reader": reader, "stranger": stranger} {
		if got := str(a.told(c)); !slices.Equal(got, []string{shelf}) {
			t.Errorf("the %s is told %v, want only of the shared library's new books", name, got)
		}
	}
	if got := a.told(quiet); len(got) != 0 {
		t.Errorf("a reader who chose not to hear of new books is told %v", got)
	}

	// Scanned again, nothing new: nothing more to tell.
	a.scanNow(shared)
	a.flush(events)
	if got := a.told(reader); len(got) != 1 {
		t.Errorf("an unchanged library told the reader again: %v", got)
	}
}

func TestDuplicatesAFailedScanAndAFulfilledWishAreTold(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withProvider()
	events := a.withEvents()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	folder := t.TempDir()
	lib := a.externalLibrary(admin, "Shelf", "shared", folder)

	_, wish, _ := a.call(reader, http.MethodPost, "/books/wishes", map[string]any{
		"library": lib, "provider": "shelf", "title": "Emma",
		"identifiers": []map[string]string{{"type": "isbn", "value": "978-0-14-143958-7"}},
	})
	writeEPUB(t, filepath.Join(folder, "Emma.epub"),
		`<dc:title>Emma</dc:title><dc:identifier opf:scheme="ISBN">0-14-143958-0</dc:identifier>`, nil, "Emma Woodhouse.")
	writeEPUB(t, filepath.Join(folder, "Persuasion.epub"), `<dc:title>Persuasion</dc:title>`, nil, "Anne Elliot.")
	same, err := os.ReadFile(filepath.Join(folder, "Persuasion.epub"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(folder, "Persuasion (copy).epub"), same, 0o644)
	a.scanNow(lib)
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}
	for _, name := range []string{"Persuasion.epub", "Persuasion (copy).epub"} {
		if err := a.server.Duplicates.Check(context.Background(), files[name].BookID); err != nil {
			t.Fatal(err)
		}
	}
	a.flush(events)

	got := a.told(reader)
	if !slices.Contains(kindsOf(got), "wish.fulfilled") || slices.Contains(kindsOf(got), "duplicates.found") {
		t.Fatalf("the wisher is told %v", got)
	}
	for _, n := range got {
		if n.Kind == "wish.fulfilled" && (n.Data["title"] != "Emma" || n.Link != "/books/"+wish["id"].(string)) {
			t.Errorf("the fulfilled wish: %v", n)
		}
	}
	if got := a.told(editor); !slices.Contains(kindsOf(got), "duplicates.found") || slices.Contains(kindsOf(got), "wish.fulfilled") {
		t.Errorf("the editor is told %v", got)
	}
	var duplicates []told
	for _, n := range a.told(editor) {
		if n.Kind == "duplicates.found" {
			duplicates = append(duplicates, n)
		}
	}
	if len(duplicates) != 1 || duplicates[0].Data["count"] != 1.0 {
		t.Errorf("one new pair, checked from both books, is told as %v", duplicates)
	}

	// The disk goes: the administrator hears of it, the editor does not.
	if err := os.Rename(folder, folder+"-gone"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(folder+"-gone", folder) })
	a.scans.Run(context.Background(), uuid.MustParse(lib), 0)
	a.flush(events)
	if got := a.told(admin); !slices.Contains(kindsOf(got), "scan.failed") {
		t.Errorf("the administrator is told %v", got)
	}
	if got := a.told(editor); slices.Contains(kindsOf(got), "scan.failed") {
		t.Errorf("the editor is told of the failed scan: %v", got)
	}
}

func TestEventsWaitWhileMoreAreComing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	events := a.withEvents()
	admin, _ := a.signedIn("admin", "admin")
	folder := t.TempDir()
	writeEPUB(t, filepath.Join(folder, "Emma.epub"), "<dc:title>Emma</dc:title>", nil, "Text.")
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", folder))

	wait, err := events.Flush(context.Background(), time.Now(), false)
	if err != nil || wait <= 0 || wait > time.Minute {
		t.Fatalf("flush right after an event: wait %s, %v", wait, err)
	}
	if got := a.told(admin); len(got) != 0 {
		t.Errorf("told before the events stopped coming: %v", got)
	}
	if n := a.countRows(`SELECT count(*) FROM river_job WHERE kind = 'notify.flush_events'`); n != 1 {
		t.Errorf("%d flush jobs queued", n)
	}
	wait, err = events.Flush(context.Background(), time.Now().Add(2*time.Minute), false)
	if err != nil || wait != 0 || len(a.told(admin)) != 1 {
		t.Errorf("flush a while later: wait %s, %v, told %v", wait, err, a.told(admin))
	}
}

func TestPeopleChooseOnlyWhatTheyMayHearOf(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")
	admin, _ := a.signedIn("admin", "admin")

	_, out, _ := a.call(reader, http.MethodGet, "/me/notification-settings", nil)
	var kinds []string
	for _, k := range out["kinds"].([]any) {
		k := k.(map[string]any)
		kinds = append(kinds, k["kind"].(string))
		if k["app"] != true {
			t.Errorf("%v is off by default", k)
		}
	}
	if !slices.Equal(kinds, []string{"books.added", "wish.fulfilled"}) {
		t.Errorf("a reader may choose %v", kinds)
	}
	if status, _, _ := a.call(reader, http.MethodPut, "/me/notification-settings", map[string]any{
		"kinds": []map[string]any{{"kind": "duplicates.found", "app": true}},
	}); status != 422 {
		t.Errorf("a reader chose duplicates: %d", status)
	}
	_, out, _ = a.call(admin, http.MethodGet, "/me/notification-settings", nil)
	if n := len(out["kinds"].([]any)); n != len(notify.EventKinds) {
		t.Errorf("an administrator may choose %d kinds, want all %d", n, len(notify.EventKinds))
	}
	_, out, _ = a.call(admin, http.MethodPut, "/me/notification-settings", map[string]any{
		"kinds": []map[string]any{{"kind": "scan.failed", "app": false}},
	})
	for _, k := range out["kinds"].([]any) {
		if k := k.(map[string]any); k["kind"] == "scan.failed" && k["app"] != false {
			t.Errorf("the choice was not kept: %v", k)
		}
	}
}
