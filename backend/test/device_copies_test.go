//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/devicecopy"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// copyRig is a heavy EPUB in a library GOtome may not write, a device of a
// reader and the book sent to it.
type copyRig struct {
	a        *app
	admin    *http.Client
	reader   *http.Client
	device   *http.Client
	token    string
	deviceID string
	root     string
	name     string
	original []byte
	file     uuid.UUID
	book     string
	lib      string
}

func newCopyRig(t *testing.T) *copyRig {
	t.Helper()
	r := &copyRig{a: newApp(t), name: "Der Schwalbenturm.epub"}
	r.admin, _ = r.a.signedIn("admin", "admin")
	r.reader, _ = r.a.signedIn("reader", "reader")
	r.root = t.TempDir()
	r.original = heavyEPUB(t, "Der Schwalbenturm")
	if err := os.WriteFile(filepath.Join(r.root, r.name), r.original, 0o444); err != nil {
		t.Fatal(err)
	}
	r.lib = r.a.externalLibrary(r.admin, "Mounted", "shared", r.root)
	r.a.scanNow(r.lib)
	f := r.a.shelfFiles()[r.name]
	r.file, r.book = f.ID, f.BookID.String()
	r.a.extract(r.file)
	_, made, _ := r.a.call(r.reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	r.deviceID = made["device"].(map[string]any)["id"].(string)
	r.token = made["token"].(string)
	r.device = deviceClient("reader", r.token)
	return r
}

func (r *copyRig) send() {
	r.a.t.Helper()
	if status, out, _ := r.a.call(r.reader, http.MethodPost, "/devices/"+r.deviceID+"/books", map[string]any{"books": []string{r.book}}); status != 200 {
		r.a.t.Fatalf("send: %d %v", status, out)
	}
}

func (r *copyRig) download() (int, http.Header, []byte) {
	r.a.t.Helper()
	return r.a.fetch(r.device, httpapi.APIPrefix+"/opds/device/files/"+r.file.String())
}

func (r *copyRig) libraryFileIsUntouched() {
	r.a.t.Helper()
	got, err := os.ReadFile(filepath.Join(r.root, r.name))
	if err != nil || !bytes.Equal(got, r.original) {
		r.a.t.Errorf("the library's file changed: %v", err)
	}
	entries, err := os.ReadDir(r.root)
	if err != nil || len(entries) != 1 {
		r.a.t.Errorf("the library's folder holds %d entries: %v", len(entries), err)
	}
}

func (r *copyRig) makeCopy() {
	r.a.t.Helper()
	if err := r.a.copies.Make(context.Background(), r.file); err != nil {
		r.a.t.Fatalf("make: %v", err)
	}
}

func TestABookWithFindingsReachesADeviceFixedFromAReadOnlyLibrary(t *testing.T) {
	t.Parallel()
	r := newCopyRig(t)
	a := r.a
	ctx := context.Background()

	r.send()
	var queued int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'devicecopy.make' AND args->>'fileId' = $1`, r.file.String()).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("%d copies queued: %v", queued, err)
	}

	status, _, body := a.fetch(r.device, httpapi.APIPrefix+"/files/"+r.file.String()+"/download")
	if status != 200 || !bytes.Equal(body, r.original) {
		t.Fatalf("before the copy is ready the file is sent as it lies: %d", status)
	}
	if _, books, _ := a.call(r.reader, http.MethodGet, "/devices/"+r.deviceID+"/books", nil); books["books"].([]any)[0].(map[string]any)["sentFixed"] != false {
		t.Errorf("sent unfixed: %v", books)
	}

	r.makeCopy()
	status, header, fixed := r.download()
	if status != 200 || bytes.Equal(fixed, r.original) {
		t.Fatalf("the fixed copy: %d, same as the file %v", status, bytes.Equal(fixed, r.original))
	}
	m, err := epub.Measure(bytes.NewReader(fixed), int64(len(fixed)))
	if err != nil || len(m.Findings()) != 0 {
		t.Errorf("the copy's findings: %v, %v", m.Findings(), err)
	}
	sum := sha256.Sum256(fixed)
	if header.Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` {
		t.Errorf("the ETag: %q", header.Get("ETag"))
	}
	r.libraryFileIsUntouched()

	// Ranges go by the copy, and a part is no delivery.
	req, _ := http.NewRequest(http.MethodGet, a.url+httpapi.APIPrefix+"/files/"+r.file.String()+"/download", nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := r.device.Do(req)
	if err != nil || resp.StatusCode != 206 || resp.Header.Get("ETag") != header.Get("ETag") {
		t.Fatalf("a part: %v %v", resp, err)
	}
	resp.Body.Close()

	// The handler marks the delivery once the body has gone out.
	for range 100 {
		var delivered bool
		if err := a.pool.QueryRow(ctx, `SELECT delivered_at IS NOT NULL FROM deliveries WHERE book_id = $1`, r.book).Scan(&delivered); err != nil {
			t.Fatal(err)
		}
		if delivered {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, books, _ := a.call(r.reader, http.MethodGet, "/devices/"+r.deviceID+"/books", nil)
	if got := books["books"].([]any)[0].(map[string]any); got["sentFixed"] != true || got["state"] != "delivered" {
		t.Errorf("the device's list: %v", got)
	}

	// A browser is sent the file itself.
	if status, _, body := a.fetch(r.reader, httpapi.APIPrefix+"/files/"+r.file.String()+"/download"); status != 200 || !bytes.Equal(body, r.original) {
		t.Errorf("a browser's download: %d", status)
	}

	// Off, a device is sent the file as it lies.
	if status, out, _ := a.call(r.admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.DevicesFixOnSend: "off"}}); status != 200 {
		t.Fatalf("settings: %d %v", status, out)
	}
	if _, _, body := a.fetch(r.device, httpapi.APIPrefix+"/files/"+r.file.String()+"/download"); !bytes.Equal(body, r.original) {
		t.Error("with fixing off the device is sent a copy")
	}
	r.libraryFileIsUntouched()
}

func TestFixedCopiesAreKeptBySourceAndVersion(t *testing.T) {
	t.Parallel()
	r := newCopyRig(t)
	a := r.a
	ctx := context.Background()
	r.send()
	r.makeCopy()

	type note struct {
		state    string
		source   []byte
		sha      []byte
		version  int16
		fileSize int64
	}
	notes := func() []note {
		t.Helper()
		rows, err := a.pool.Query(ctx, `SELECT state, source_sha256, sha256, fix_version, coalesce(size_bytes, 0) FROM device_copies ORDER BY made_at`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []note
		for rows.Next() {
			var n note
			if err := rows.Scan(&n.state, &n.source, &n.sha, &n.version, &n.fileSize); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		return out
	}
	first := notes()
	if len(first) != 1 || first[0].state != "ready" || first[0].version != epub.FixVersion {
		t.Fatalf("after the first copy: %+v", first)
	}
	dir := devicecopy.Dir(a.dataDir)
	files := func() []string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	if len(files()) != 1 {
		t.Fatalf("the cache: %v", files())
	}

	// A hit: nothing is made again, nothing is asked for.
	r.makeCopy()
	if again := notes(); len(again) != 1 || !bytes.Equal(again[0].sha, first[0].sha) {
		t.Errorf("a hit: %+v", again)
	}
	r.send()
	if err := a.copies.Request(ctx, []uuid.UUID{uuid.MustParse(r.book)}); err != nil {
		t.Fatal(err)
	}
	var jobs int
	_ = a.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'devicecopy.make' AND state = 'available'`).Scan(&jobs)
	if jobs != 1 {
		t.Errorf("%d jobs for a book that has its copy", jobs)
	}

	// The version is raised: another copy, the first stays for devices that hold it.
	a.copies.Version++
	if ready, err := a.copies.Ready(ctx, first[0].source); err != nil || ready != nil {
		t.Errorf("a copy of an older version is sent: %v %v", ready, err)
	}
	r.makeCopy()
	if got := notes(); len(got) != 2 || got[1].version != epub.FixVersion+1 || len(files()) != 2 {
		t.Errorf("after raising the version: %+v, %v", got, files())
	}
	a.copies.Version--

	// The source changes: a copy of its own; the old bytes' copy is not sent for it.
	changed := heavyEPUB(t, "Der Schwalbenturm, zweite Auflage")
	path := filepath.Join(r.root, r.name)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	a.scanNow(r.lib)
	a.extract(r.file)
	status, _, body := r.download()
	if status != 200 || !bytes.Equal(body, changed) {
		t.Errorf("a source with no copy yet is sent as it lies: %d", status)
	}
	r.makeCopy()
	if got := notes(); len(got) != 3 || bytes.Equal(got[2].source, first[0].source) {
		t.Fatalf("after the source changed: %+v", got)
	}
	r.send()
	_, _, body = r.download()
	if bytes.Equal(body, changed) || bytes.Equal(body, r.original) {
		t.Error("the changed source is not sent its own fixed copy")
	}

	// Trimming takes a copy unused for long out of the cache, not out of
	// the record of what a device may hold.
	current := notes()[2].source
	if _, err := a.pool.Exec(ctx, `UPDATE device_copies SET last_used_at = now() - interval '100 days' WHERE fix_version = $1 AND source_sha256 = $2`, epub.FixVersion, current); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.epub"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := a.now.AddDate(0, 0, -2)
	_ = os.Chtimes(filepath.Join(dir, "stray.epub"), old, old)
	if err := a.copies.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if left := files(); len(left) != 2 || slices.Contains(left, "stray.epub") {
		t.Errorf("after trimming: %v", left)
	}
	var state string
	if err := a.pool.QueryRow(ctx, `SELECT state FROM device_copies WHERE fix_version = $1 AND source_sha256 = $2`, epub.FixVersion, current).Scan(&state); err != nil || state != "evicted" {
		t.Errorf("the trimmed copy: %q, %v", state, err)
	}
	r.libraryFileIsUntouched2(changed)
}

func md5PartialOf(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.epub")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := kosync.PartialMD5(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func (r *copyRig) libraryFileIsUntouched2(want []byte) {
	r.a.t.Helper()
	if got, err := os.ReadFile(filepath.Join(r.root, r.name)); err != nil || !bytes.Equal(got, want) {
		r.a.t.Errorf("the library's file: %v", err)
	}
}

func TestKOReaderSyncsOnAFixedCopyAndOnTheFile(t *testing.T) {
	t.Parallel()
	r := newCopyRig(t)
	a := r.a
	ctx := context.Background()
	if _, err := a.server.Sync.HashFiles(ctx); err != nil {
		t.Fatal(err)
	}
	r.send()
	r.makeCopy()
	_, _, fixed := r.download()
	if bytes.Equal(fixed, r.original) {
		t.Fatal("no fixed copy")
	}

	onCopy, onFile := md5PartialOf(t, fixed), md5PartialOf(t, r.original)
	if onCopy == onFile {
		t.Fatal("the copy has the file's hash")
	}
	xpointer := "/body/DocFragment[3]/body/p[2]/text().0"
	status, saved := a.koreader("reader", r.token, http.MethodPut, "/syncs/progress", map[string]any{
		"document": onCopy, "progress": xpointer, "percentage": 0.25, "device": "Tolino", "device_id": "TOLINO1",
	})
	if status != 200 || saved["document"] != onCopy {
		t.Fatalf("saving on the copy: %d %v", status, saved)
	}
	for _, document := range []string{onCopy, onFile} {
		status, got := a.koreader("reader", r.token, http.MethodGet, "/syncs/progress/"+document, nil)
		if status != 200 || got["progress"] != xpointer || got["percentage"] != 0.25 {
			t.Errorf("the place by %s: %d %v", document, status, got)
		}
	}
	var book string
	if err := a.pool.QueryRow(ctx, `SELECT book_id FROM reading_progress WHERE locator = $1`, "koreader:"+xpointer).Scan(&book); err != nil || book != r.book {
		t.Errorf("the place is kept for the book: %q, %v", book, err)
	}
	if status, _ := a.koreader("reader", r.token, http.MethodPut, "/syncs/progress", map[string]any{
		"document": md5Of([]byte("another file")), "progress": xpointer, "percentage": 0.1, "device": "Tolino", "device_id": "TOLINO1",
	}); status != 404 {
		t.Errorf("an unknown document: %d", status)
	}

	// The copy goes from the cache; the device that holds it is still known.
	if _, err := a.pool.Exec(ctx, `UPDATE device_copies SET last_used_at = now() - interval '100 days'`); err != nil {
		t.Fatal(err)
	}
	if err := a.copies.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if status, got := a.koreader("reader", r.token, http.MethodGet, "/syncs/progress/"+onCopy, nil); status != 200 || got["progress"] != xpointer {
		t.Errorf("after trimming: %d %v", status, got)
	}

	// The tolino app's hashes of what it holds find the book by the copy's.
	sum := sha256.Sum256(fixed)
	status, out, _ := a.call(r.device, http.MethodPost, "/device/matches", map[string]any{"hashes": []string{hex.EncodeToString(sum[:])}})
	if status != 200 || len(out["matches"].([]any)) != 1 {
		t.Errorf("matching the copy by its SHA-256: %d %v", status, out)
	}
}
