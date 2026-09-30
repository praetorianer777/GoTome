//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// folder is a library on a folder of its own, scanned without the job queue.
type folder struct {
	t       *testing.T
	pool    *pgxpool.Pool
	root    string
	library uuid.UUID
	scanner *ingest.Scanner
}

func newFolder(t *testing.T) *folder {
	t.Helper()
	f := &folder{t: t, pool: dbtest.New(t), root: t.TempDir()}
	err := f.pool.QueryRow(context.Background(),
		`INSERT INTO libraries (name, root_path, mode, writable, visibility)
		 VALUES ('Books', $1, 'external', false, 'shared') RETURNING id`, f.root).Scan(&f.library)
	if err != nil {
		t.Fatal(err)
	}
	f.scanner = ingest.NewScanner(f.pool, slog.New(slog.DiscardHandler))
	return f
}

func (f *folder) path(relPath string) string {
	return filepath.Join(f.root, filepath.FromSlash(relPath))
}

func (f *folder) write(relPath, content string) {
	f.t.Helper()
	full := f.path(relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *folder) scan() ingest.Result {
	f.t.Helper()
	res, err := f.scanner.Scan(context.Background(), f.library, f.root)
	if err != nil {
		f.t.Fatalf("scan: %v", err)
	}
	return res
}

// file is a row of book_files as the tests look at it.
type file struct {
	ID, BookID   uuid.UUID
	Title        string
	SHA256       []byte
	Part         *int32
	Missing      bool
	ExtractState string
	UpdatedAt    time.Time
}

func (f *folder) files() map[string]file {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT bf.rel_path, bf.id, bf.book_id, b.title, bf.sha256, bf.part_index,
		        bf.missing_at IS NOT NULL, bf.extract_state, bf.updated_at
		 FROM book_files bf JOIN books b ON b.id = bf.book_id
		 WHERE bf.library_id = $1`, f.library)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]file{}
	for rows.Next() {
		var relPath string
		var x file
		if err := rows.Scan(&relPath, &x.ID, &x.BookID, &x.Title, &x.SHA256, &x.Part, &x.Missing, &x.ExtractState, &x.UpdatedAt); err != nil {
			f.t.Fatal(err)
		}
		out[relPath] = x
	}
	return out
}

func (f *folder) count(table string) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func sum(content string) []byte {
	s := sha256.Sum256([]byte(content))
	return s[:]
}

func TestFirstScanImportsAndGroups(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Herbert/Dune.epub", "dune as epub")
	f.write("Herbert/Dune.pdf", "dune as pdf")
	f.write("Austen/Emma/02 - Two.mp3", "emma two")
	f.write("Austen/Emma/01 - One.mp3", "emma one")
	f.write("Austen/Emma/10 - Ten.mp3", "emma ten")
	f.write("Austen/Emma/cover.jpg", "not a book")
	f.write("Loose.mobi", "loose")

	res := f.scan()
	want := ingest.Result{Seen: 6, Added: 6, Books: 3, Hashed: 6, Complete: true}
	if res != want {
		t.Fatalf("first scan = %+v, want %+v", res, want)
	}

	files := f.files()
	if len(files) != 6 {
		t.Fatalf("%d files, want 6: %v", len(files), files)
	}
	for relPath, content := range map[string]string{
		"Herbert/Dune.epub": "dune as epub", "Herbert/Dune.pdf": "dune as pdf",
		"Austen/Emma/01 - One.mp3": "emma one", "Loose.mobi": "loose",
	} {
		if got := files[relPath]; !bytes.Equal(got.SHA256, sum(content)) {
			t.Errorf("%s: sha256 = %x, want that of its content", relPath, got.SHA256)
		}
	}
	for relPath, x := range files {
		if len(x.SHA256) != 32 {
			t.Errorf("%s has no SHA-256", relPath)
		}
	}

	epub, pdf := files["Herbert/Dune.epub"], files["Herbert/Dune.pdf"]
	if epub.BookID != pdf.BookID || epub.Title != "Dune" || epub.Part != nil {
		t.Errorf("Dune in two formats: %+v and %+v, want one book called Dune", epub, pdf)
	}
	for relPath, part := range map[string]int32{
		"Austen/Emma/01 - One.mp3": 0, "Austen/Emma/02 - Two.mp3": 1, "Austen/Emma/10 - Ten.mp3": 2,
	} {
		x := files[relPath]
		if x.Title != "Emma" || x.Part == nil || *x.Part != part || x.BookID != files["Austen/Emma/01 - One.mp3"].BookID {
			t.Errorf("%s: %+v, want part %d of the one book Emma", relPath, x, part)
		}
	}
	if files["Loose.mobi"].Title != "Loose" {
		t.Errorf("Loose.mobi is the book %q", files["Loose.mobi"].Title)
	}

	var sources string
	err := f.pool.QueryRow(context.Background(), "SELECT field_sources::text FROM books WHERE id = $1", epub.BookID).Scan(&sources)
	if err != nil || sources != `{"title": "filename"}` {
		t.Errorf("field sources = %s (%v), want the title marked as read off the file name", sources, err)
	}
}

func TestSecondScanOfAnUnchangedLibraryOnlyWalks(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Herbert/Dune.epub", "dune")
	f.write("Austen/Emma/01.mp3", "one")
	f.write("Austen/Emma/02.mp3", "two")
	f.scan()
	before := f.files()

	res := f.scan()
	if want := (ingest.Result{Seen: 3, Complete: true}); res != want {
		t.Fatalf("second scan = %+v, want %+v: nothing read, nothing written", res, want)
	}
	for relPath, x := range f.files() {
		if !x.UpdatedAt.Equal(before[relPath].UpdatedAt) {
			t.Errorf("%s was written to by a scan that found nothing new", relPath)
		}
	}
}

func TestScanNoticesAModifiedFile(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Dune.epub", "first edition")
	f.write("Emma.epub", "emma")
	f.scan()
	before := f.files()
	if _, err := f.pool.Exec(context.Background(), "UPDATE book_files SET extract_state = 'done', content_sha256 = sha256"); err != nil {
		t.Fatal(err)
	}

	f.write("Dune.epub", "second edition, revised")
	res := f.scan()
	if want := (ingest.Result{Seen: 2, Changed: 1, Hashed: 1, Complete: true}); res != want {
		t.Fatalf("scan after a change = %+v, want %+v", res, want)
	}
	after := f.files()
	dune := after["Dune.epub"]
	if dune.ID != before["Dune.epub"].ID || dune.BookID != before["Dune.epub"].BookID {
		t.Error("the changed file became another file or another book")
	}
	if !bytes.Equal(dune.SHA256, sum("second edition, revised")) || dune.ExtractState != "pending" {
		t.Errorf("changed file: sha256 %x, extraction %s; want the new hash and extraction due again", dune.SHA256, dune.ExtractState)
	}
	if after["Emma.epub"].ExtractState != "done" {
		t.Error("the file that did not change lost its extraction")
	}

	// Touched but not changed: read again, found to be the same, left alone.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(f.path("Emma.epub"), later, later); err != nil {
		t.Fatal(err)
	}
	res = f.scan()
	if want := (ingest.Result{Seen: 2, Hashed: 1, Complete: true}); res != want {
		t.Fatalf("scan after a touch = %+v, want %+v", res, want)
	}
	if f.files()["Emma.epub"].ExtractState != "done" {
		t.Error("a file that was only touched lost its extraction")
	}
	if res := f.scan(); res.Hashed != 0 {
		t.Error("the touched file was read again on the scan after")
	}
}

func TestScanFollowsAMovedFile(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("inbox/Dune.epub", "dune")
	f.write("inbox/Emma/01.mp3", "one")
	f.write("inbox/Emma/02.mp3", "two")
	f.scan()
	before := f.files()

	if err := os.MkdirAll(f.path("Herbert"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path("inbox/Dune.epub"), f.path("Herbert/Dune - Frank Herbert.epub")); err != nil {
		t.Fatal(err)
	}
	// A whole folder renamed.
	if err := os.Rename(f.path("inbox/Emma"), f.path("Emma by Austen")); err != nil {
		t.Fatal(err)
	}

	res := f.scan()
	if want := (ingest.Result{Seen: 3, Moved: 3, Hashed: 3, Complete: true}); res != want {
		t.Fatalf("scan after moves = %+v, want %+v", res, want)
	}
	after := f.files()
	if len(after) != 3 || f.count("books") != 2 {
		t.Fatalf("%d files in %d books after the moves, want 3 in 2: %v", len(after), f.count("books"), after)
	}
	for from, to := range map[string]string{
		"inbox/Dune.epub":   "Herbert/Dune - Frank Herbert.epub",
		"inbox/Emma/01.mp3": "Emma by Austen/01.mp3",
		"inbox/Emma/02.mp3": "Emma by Austen/02.mp3",
	} {
		was, is := before[from], after[to]
		if is.ID != was.ID || is.BookID != was.BookID || is.Missing {
			t.Errorf("%s -> %s: %+v became %+v, want the same file of the same book", from, to, was, is)
		}
		if !equalPart(was.Part, is.Part) {
			t.Errorf("%s lost its place among the parts", to)
		}
	}
	// What a person called the book stays, whatever the file is called now.
	if after["Herbert/Dune - Frank Herbert.epub"].Title != "Dune" {
		t.Errorf("the moved book is now called %q", after["Herbert/Dune - Frank Herbert.epub"].Title)
	}
}

func equalPart(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestScanMarksRemovedFilesMissingAndBringsThemBack(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Dune.epub", "dune")
	f.write("Emma.epub", "emma")
	f.scan()
	before := f.files()

	if err := os.Remove(f.path("Dune.epub")); err != nil {
		t.Fatal(err)
	}
	res := f.scan()
	if want := (ingest.Result{Seen: 1, Missing: 1, Complete: true}); res != want {
		t.Fatalf("scan after a removal = %+v, want %+v", res, want)
	}
	after := f.files()
	if len(after) != 2 || !after["Dune.epub"].Missing || after["Emma.epub"].Missing || f.count("books") != 2 {
		t.Fatalf("after the removal: %v; want both files known, Dune marked missing", after)
	}
	if res := f.scan(); res.Missing != 0 {
		t.Errorf("a file already missing was counted again: %+v", res)
	}

	// The same file comes back, as a restored backup does.
	f.write("Dune.epub", "dune")
	res = f.scan()
	if res.Restored != 1 || res.Added != 0 || res.Changed != 0 {
		t.Fatalf("scan after the file came back = %+v, want one restored", res)
	}
	back := f.files()["Dune.epub"]
	if back.Missing || back.ID != before["Dune.epub"].ID || back.BookID != before["Dune.epub"].BookID {
		t.Errorf("the file that came back is %+v, want the file it was", back)
	}
}

func TestScanAddsANewFormatToTheBookItBelongsTo(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Herbert/Dune.epub", "dune")
	f.write("Austen/Emma/01.mp3", "one")
	f.write("Austen/Emma/03.mp3", "three")
	f.scan()

	f.write("Herbert/Dune.pdf", "dune, typeset")
	f.write("Herbert/Dune Messiah.epub", "messiah")
	f.write("Austen/Emma/02.mp3", "two")
	res := f.scan()
	if want := (ingest.Result{Seen: 6, Added: 3, Books: 1, Hashed: 3, Complete: true}); res != want {
		t.Fatalf("scan = %+v, want %+v", res, want)
	}
	files := f.files()
	if files["Herbert/Dune.pdf"].BookID != files["Herbert/Dune.epub"].BookID {
		t.Error("the PDF of Dune did not join the book its EPUB is")
	}
	if files["Herbert/Dune Messiah.epub"].BookID == files["Herbert/Dune.epub"].BookID {
		t.Error("Dune Messiah joined Dune")
	}
	for relPath, part := range map[string]int32{"Austen/Emma/01.mp3": 0, "Austen/Emma/02.mp3": 1, "Austen/Emma/03.mp3": 2} {
		x := files[relPath]
		if x.Part == nil || *x.Part != part || x.BookID != files["Austen/Emma/01.mp3"].BookID {
			t.Errorf("%s: %+v, want part %d of Emma", relPath, x, part)
		}
	}
}

func TestScanTouchesNothingWhenTheFolderIsGone(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	f.write("Dune.epub", "dune")
	f.scan()

	// An unmounted disk: the folder is not there at all.
	if err := os.Rename(f.root, f.root+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(f.root+"-unmounted", f.root) })
	_, err := f.scanner.Scan(context.Background(), f.library, f.root)
	if !errors.Is(err, ingest.ErrNoFolder) {
		t.Fatalf("scan of a folder that is gone: %v, want ErrNoFolder", err)
	}
	if f.files()["Dune.epub"].Missing {
		t.Error("an unmounted folder made its books missing")
	}
}

func TestScanLeavesAloneWhatItCannotSee(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	f := newFolder(t)
	f.write("Open/Dune.epub", "dune")
	f.write("Locked/Emma.epub", "emma")
	f.scan()

	if err := os.Chmod(f.path("Locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.path("Locked"), 0o755) })
	res := f.scan()
	if want := (ingest.Result{Seen: 1, Skipped: 1, Complete: true}); res != want {
		t.Fatalf("scan = %+v, want %+v", res, want)
	}
	if f.files()["Locked/Emma.epub"].Missing {
		t.Error("a file in a folder that could not be read was marked missing")
	}
}

func TestScanInPassesReachesTheSameResult(t *testing.T) {
	t.Parallel()
	f := newFolder(t)
	for _, author := range []string{"Austen", "Herbert", "Le Guin", "Woolf"} {
		f.write(author+"/Book.epub", author)
	}
	f.scan()
	if err := os.Remove(f.path("Woolf/Book.epub")); err != nil {
		t.Fatal(err)
	}
	for _, author := range []string{"Borges", "Calvino", "Eco"} {
		f.write(author+"/Book.epub", author)
	}

	// A budget so small that every pass stops after one folder; the last one
	// has no folder left to stop before and goes on to the end.
	f.scanner.Budget = time.Nanosecond
	var total ingest.Result
	passes := 0
	for {
		res := f.scan()
		passes++
		total.Added += res.Added
		total.Missing += res.Missing
		if !res.Complete && res.Missing != 0 {
			t.Fatal("a pass that did not finish decided that files are missing")
		}
		if res.Complete {
			break
		}
		if passes > 10 {
			t.Fatal("the scan does not finish")
		}
	}
	if passes != 3 || total.Added != 3 || total.Missing != 1 {
		t.Errorf("%d passes added %d and found %d missing; want 3 passes, 3 added, 1 missing", passes, total.Added, total.Missing)
	}
	if got := len(f.files()); got != 7 {
		t.Errorf("%d files known, want 7", got)
	}
}

// workJobs starts a job runner that works the app's queued scans and the
// reading of the files they find.
func (a *app) workJobs() {
	a.t.Helper()
	workers := jobs.NewWorkers()
	river.AddWorker(workers, &ingest.ScanWorker{Service: a.scans})
	river.AddWorker(workers, &ingest.ScanAllWorker{Service: a.scans})
	river.AddWorker(workers, &ingest.ExtractWorker{Service: a.scans})
	runner, err := jobs.New(a.pool, jobs.Config{Logger: slog.New(slog.DiscardHandler), Workers: workers})
	if err != nil {
		a.t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(func() { _ = runner.Stop(context.Background()) })
}

// lastScan waits for the library's newest scan to reach the state.
func (a *app) lastScan(c *http.Client, libraryID, state string) map[string]any {
	a.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, body, _ := a.call(c, http.MethodGet, "/libraries/"+libraryID, nil)
		if status != 200 {
			a.t.Fatalf("get library: %d %v", status, body)
		}
		scan, _ := body["lastScan"].(map[string]any)
		if scan["state"] == state {
			return scan
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("the scan did not reach %s: %v", state, scan)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (a *app) externalLibrary(admin *http.Client, name, visibility, root string) string {
	a.t.Helper()
	status, body, _ := a.call(admin, http.MethodPost, "/libraries", map[string]any{
		"name": name, "mode": "external", "rootPath": root, "visibility": visibility,
	})
	if status != 201 {
		a.t.Fatalf("create library %s: %d %v", name, status, body)
	}
	return body["id"].(string)
}

func TestScanIsAskedForThroughTheAPI(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")

	books := t.TempDir()
	if err := os.WriteFile(filepath.Join(books, "Dune.epub"), []byte("dune"), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := a.externalLibrary(admin, "Shared", "shared", books)
	private := a.externalLibrary(admin, "Private", "private", t.TempDir())

	// Adding a library asks for its first scan.
	status, body, _ := a.call(admin, http.MethodGet, "/libraries/"+shared, nil)
	first, _ := body["lastScan"].(map[string]any)
	if status != 200 || first["state"] != "queued" {
		t.Fatalf("a new library: %d %v, want a scan waiting", status, body)
	}

	// Asking again while it waits is the same scan.
	status, again, _ := a.call(editor, http.MethodPost, "/libraries/"+shared+"/scans", nil)
	if status != 202 || again["id"] != first["id"] || again["state"] != "queued" {
		t.Errorf("an editor asking again: %d %v, want 202 with scan %v", status, again, first["id"])
	}
	if n := a.count("library_scans WHERE library_id = '" + shared + "'"); n != 1 {
		t.Errorf("%d scans on record for the library, want 1", n)
	}

	if status, body, _ := a.call(reader, http.MethodPost, "/libraries/"+shared+"/scans", nil); status != 403 {
		t.Errorf("a reader asking for a scan: %d %v, want 403", status, body)
	}
	// A library the editor may not see is answered like one that is not there.
	_, missing, _ := a.call(editor, http.MethodPost, "/libraries/"+uuid.NewString()+"/scans", nil)
	status, hidden, _ := a.call(editor, http.MethodPost, "/libraries/"+private+"/scans", nil)
	delete(hidden["error"].(map[string]any), "requestId")
	delete(missing["error"].(map[string]any), "requestId")
	if status != 404 || !mapsEqual(hidden, missing) {
		t.Errorf("an editor asking for a scan of a hidden library: %d %v, want the 404 of %v", status, hidden, missing)
	}

	a.workJobs()
	done := a.lastScan(editor, shared, "done")
	if done["id"] != first["id"] || done["filesSeen"] != 1.0 || done["filesAdded"] != 1.0 || done["booksAdded"] != 1.0 {
		t.Errorf("the finished scan: %v, want scan %v with one file and one book", done, first["id"])
	}
	if done["startedAt"] == nil || done["finishedAt"] == nil {
		t.Errorf("the finished scan has no times: %v", done)
	}

	// A finished scan does not stand in the way of the next.
	status, next, _ := a.call(editor, http.MethodPost, "/libraries/"+shared+"/scans", nil)
	if status != 202 || next["id"] == first["id"] {
		t.Fatalf("asking after the scan finished: %d %v, want a new scan", status, next)
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		scan := a.lastScan(editor, shared, "done")
		if scan["id"] == next["id"] {
			if scan["filesAdded"] != 0.0 || scan["filesSeen"] != 1.0 {
				t.Errorf("the second scan: %v, want nothing added", scan)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second scan did not finish")
		}
	}
}

func (a *app) count(from string) int {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+from).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestScanOfAFolderThatIsGoneFailsInWords(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := filepath.Join(t.TempDir(), "books")
	if err := os.Mkdir(books, 0o755); err != nil {
		t.Fatal(err)
	}
	id := a.externalLibrary(admin, "NAS", "shared", books)
	if err := os.Remove(books); err != nil {
		t.Fatal(err)
	}

	a.workJobs()
	failed := a.lastScan(reader, id, "failed")
	message, _ := failed["error"].(string)
	if message == "" || bytes.Contains([]byte(message), []byte(books)) {
		t.Errorf("the failed scan says %q; want words, and not where the folder lies", message)
	}
	// Not tried over and over: the folder will be no more there in a second.
	time.Sleep(2500 * time.Millisecond)
	if n := a.count("library_scans"); n != 1 {
		t.Errorf("%d scans on record, want the one that failed", n)
	}
}

func TestScheduleAsksForEveryLibrary(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	one := a.externalLibrary(admin, "One", "shared", t.TempDir())
	two := a.externalLibrary(admin, "Two", "private", t.TempDir())
	a.workJobs()
	a.lastScan(admin, one, "done")
	a.lastScan(admin, two, "done")

	if err := a.scans.RequestAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if a.count("library_scans WHERE state = 'done' AND requested_by IS NULL") == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the scheduled scans did not finish: %d scans on record", a.count("library_scans"))
		}
	}
	if got := []int{a.count("library_scans WHERE library_id = '" + one + "'"), a.count("library_scans WHERE library_id = '" + two + "'")}; !slices.Equal(got, []int{2, 2}) {
		t.Errorf("scans per library = %v, want 2 each", got)
	}
}

func TestAskingAgainRevivesAScanThatLostItsJob(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	id := a.externalLibrary(admin, "NAS", "shared", t.TempDir())
	_, body, _ := a.call(admin, http.MethodGet, "/libraries/"+id, nil)
	stuck := body["lastScan"].(map[string]any)

	// What a run cut off once too often leaves behind: the scan is still
	// open, and no job is going to finish it.
	if _, err := a.pool.Exec(context.Background(), "DELETE FROM river_job"); err != nil {
		t.Fatal(err)
	}
	a.workJobs()
	status, again, _ := a.call(admin, http.MethodPost, "/libraries/"+id+"/scans", nil)
	if status != 202 || again["id"] != stuck["id"] {
		t.Fatalf("asking again: %d %v, want the open scan %v", status, again, stuck["id"])
	}
	if done := a.lastScan(admin, id, "done"); done["id"] != stuck["id"] {
		t.Errorf("the scan that finished is %v, want %v", done["id"], stuck["id"])
	}
}
