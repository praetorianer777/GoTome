//go:build integration

package test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (a *app) jobs(c *http.Client, state string) []map[string]any {
	a.t.Helper()
	path := "/jobs"
	if state != "" {
		path += "?state=" + state
	}
	status, body, _ := a.call(c, http.MethodGet, path, nil)
	if status != 200 {
		a.t.Fatalf("jobs: %d %v", status, body)
	}
	var out []map[string]any
	for _, j := range body["jobs"].([]any) {
		out = append(out, j.(map[string]any))
	}
	return out
}

func TestJobsAreForEditorsOfLibrariesTheySee(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")

	shared, hidden := t.TempDir(), t.TempDir()
	writeEPUB(t, filepath.Join(shared, "Emma.epub"), emmaMetadata, nil, "Emma.")
	if err := os.WriteFile(filepath.Join(shared, "Broken.epub"), []byte("not a zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEPUB(t, filepath.Join(hidden, "Secret.epub"), emmaMetadata, nil, "Secret.")
	sharedLib := a.externalLibrary(admin, "Shared", "shared", shared)
	hiddenLib := a.externalLibrary(admin, "Hidden", "private", hidden)
	a.scanNow(sharedLib)
	a.scanNow(hiddenLib)

	if status, _, _ := a.call(reader, http.MethodGet, "/jobs", nil); status != 403 {
		t.Errorf("a reader sees the jobs: %d", status)
	}
	byPath := map[string]map[string]any{}
	for _, j := range a.jobs(editor, "waiting") {
		if j["kind"] == "ingest.extract_file" {
			byPath[j["filePath"].(string)] = j
		}
	}
	if len(byPath) != 2 || byPath["Emma.epub"] == nil || byPath["Emma.epub"]["libraryName"] != "Shared" || byPath["Emma.epub"]["bookId"] == nil {
		t.Fatalf("the editor's waiting reads: %v", byPath)
	}
	var secret map[string]any
	for _, j := range a.jobs(admin, "") {
		if j["filePath"] == "Secret.epub" {
			secret = j
		}
	}
	if secret == nil {
		t.Fatal("the administrator does not see the hidden library's job")
	}
	secretID := fmt.Sprint(int64(secret["id"].(float64)))
	for _, action := range []string{"retry", "cancel"} {
		if status, _, _ := a.call(editor, http.MethodPost, "/jobs/"+secretID+"/"+action, nil); status != 404 {
			t.Errorf("%s a hidden library's job: %d", action, status)
		}
	}

	// Cancelled, a job is failed; retried, it waits again.
	emma := fmt.Sprint(int64(byPath["Emma.epub"]["id"].(float64)))
	status, body, _ := a.call(editor, http.MethodPost, "/jobs/"+emma+"/cancel", nil)
	if status != 200 || body["state"] != "cancelled" {
		t.Fatalf("cancel: %d %v", status, body)
	}
	if failed := a.jobs(editor, "failed"); len(failed) != 1 || failed[0]["filePath"] != "Emma.epub" {
		t.Errorf("failed after the cancel: %v", failed)
	}
	status, body, _ = a.call(editor, http.MethodPost, "/jobs/"+emma+"/retry", nil)
	if status != 200 || body["state"] != "available" {
		t.Errorf("retry: %d %v", status, body)
	}
	if status, _, _ := a.call(editor, http.MethodGet, "/jobs?state=lost", nil); status != 422 {
		t.Errorf("an unknown state: %d", status)
	}
}

func TestFailuresShowAndAreTriedAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "Broken.epub"), []byte("not a zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := a.externalLibrary(admin, "Shelf", "shared", folder)
	a.scanNow(lib)
	broken := a.shelfFiles()["Broken.epub"]
	a.extract(broken.ID)

	// The book page says the file could not be read; why, only to those who
	// may have it read again.
	_, asEditor, _ := a.call(editor, http.MethodGet, "/books/"+broken.BookID.String(), nil)
	_, asReader, _ := a.call(reader, http.MethodGet, "/books/"+broken.BookID.String(), nil)
	ef := asEditor["files"].([]any)[0].(map[string]any)
	rf := asReader["files"].([]any)[0].(map[string]any)
	if ef["extractState"] != "failed" || ef["extractError"] == nil || rf["extractState"] != "failed" || rf["extractError"] != nil {
		t.Errorf("editor sees %v, reader %v", ef, rf)
	}
	_, libs, _ := a.call(editor, http.MethodGet, "/libraries/"+lib, nil)
	if libs["filesFailed"] != 1.0 {
		t.Errorf("the library's failed files: %v", libs)
	}

	if status, _, _ := a.call(reader, http.MethodPost, "/files/"+broken.ID.String()+"/extraction", nil); status != 403 {
		t.Errorf("a reader has a file read again: %d", status)
	}
	status, body, _ := a.call(editor, http.MethodPost, "/files/"+broken.ID.String()+"/extraction", nil)
	if status != 202 || body["queued"] != 1.0 || a.shelfFiles()["Broken.epub"].State != "pending" {
		t.Errorf("read the file again: %d %v, state %s", status, body, a.shelfFiles()["Broken.epub"].State)
	}
	a.extract(broken.ID)
	status, body, _ = a.call(editor, http.MethodPost, "/libraries/"+lib+"/extractions", map[string]any{"failedOnly": true})
	if status != 202 || body["queued"] != 1.0 {
		t.Errorf("read the library's failed files again: %d %v", status, body)
	}

	// A scan of a folder that is gone fails for good, and says why.
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/libraries/"+lib+"/scans", nil); status != 202 {
		t.Fatalf("scan: %d", status)
	}
	a.workJobs()
	var failed map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for failed == nil && time.Now().Before(deadline) {
		for _, j := range a.jobs(editor, "failed") {
			if j["kind"] == "ingest.scan_library" {
				failed = j
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if failed == nil || !strings.Contains(fmt.Sprint(failed["lastError"]), "cannot be read") || failed["libraryName"] != "Shelf" {
		t.Fatalf("the failed scan: %v", failed)
	}
	status, body, _ = a.call(editor, http.MethodPost, fmt.Sprintf("/jobs/%d/retry", int64(failed["id"].(float64))), nil)
	if status != 200 || body["state"] == "cancelled" {
		t.Errorf("retry the scan: %d %v", status, body)
	}
}
