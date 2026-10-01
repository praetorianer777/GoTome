//go:build integration

package test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func (a *app) storage(c *http.Client) map[string]any {
	a.t.Helper()
	status, body, _ := a.call(c, http.MethodGet, "/auth/storage", nil)
	if status != 200 {
		a.t.Fatalf("storage: %d %v", status, body)
	}
	return body
}

func TestQuotas(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	lib := a.library(admin, "Shelf", "shared")

	if st := a.storage(editor); st["usedBytes"] != 0.0 || st["quotaBytes"] != nil {
		t.Errorf("no quota yet: %v", st)
	}
	if status, _, _ := a.call(editor, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": 1 << 30}); status != 403 {
		t.Errorf("an editor raises their own quota: %d", status)
	}
	status, body, _ := a.call(admin, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": 100})
	if status != 200 || body["quotaBytes"] != 100.0 {
		t.Fatalf("set the quota: %d %v", status, body)
	}

	if status, body := a.upload(editor, lib, "Sixty.pdf", bytes.Repeat([]byte("6"), 60)); status != 200 {
		t.Fatalf("an upload that fits: %d %v", status, body)
	}
	if st := a.storage(editor); st["usedBytes"] != 60.0 || st["quotaBytes"] != 100.0 {
		t.Errorf("after one: %v", st)
	}

	// Too large for what is left, found while it arrives.
	status, body = a.upload(editor, lib, "Fifty.pdf", bytes.Repeat([]byte("5"), 50))
	if status != 413 || errorCode(body) != "over_quota" || !strings.Contains(body["error"].(map[string]any)["message"].(string), "60 bytes of the 100 bytes") {
		t.Errorf("over the quota: %d %v", status, body)
	}
	// Too large by what the request says it holds, refused before the file.
	status, body = a.upload(editor, lib, "Large.pdf", bytes.Repeat([]byte("L"), 5000))
	if status != 413 || errorCode(body) != "over_quota" {
		t.Errorf("announced over the quota: %d %v", status, body)
	}
	if files := libraryFiles(t, a.libraryRoot(lib)); len(files) != 1 {
		t.Errorf("refused uploads left %v", files)
	}

	// The administrator sees it in the list of accounts.
	_, body, _ = a.call(admin, http.MethodGet, "/users", nil)
	for _, u := range body["users"].([]any) {
		if u := u.(map[string]any); u["id"] == editorID && (u["usedBytes"] != 60.0 || u["quotaBytes"] != 100.0) {
			t.Errorf("the editor in the list: %v", u)
		}
	}

	// A file that is gone takes up nothing.
	if err := os.Remove(filepath.Join(a.libraryRoot(lib), "Sixty", "Sixty.pdf")); err != nil {
		t.Fatal(err)
	}
	a.scanNow(lib)
	if st := a.storage(editor); st["usedBytes"] != 0.0 {
		t.Errorf("after the file went: %v", st)
	}
	if status, body := a.upload(editor, lib, "Fifty.pdf", bytes.Repeat([]byte("5"), 50)); status != 200 {
		t.Errorf("there is room again: %d %v", status, body)
	}

	// No quota is no limit.
	a.call(admin, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": nil})
	if status, body := a.upload(editor, lib, "Big.pdf", bytes.Repeat([]byte("B"), 5000)); status != 200 {
		t.Errorf("without a quota: %d %v", status, body)
	}
	if status, body, _ := a.call(admin, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": -1}); status != 422 || fieldError(body, "quotaBytes") == "" {
		t.Errorf("a negative quota: %d %v", status, body)
	}
}

func TestTwoUploadsAtOnceDoNotBothFitIntoOneRoom(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	// Two libraries, so that the lock on a library's paths does not already
	// put one upload after the other.
	libs := []string{a.library(admin, "One", "shared"), a.library(admin, "Two", "shared")}
	a.call(admin, http.MethodPut, "/users/"+editorID+"/quota", map[string]any{"quotaBytes": 100})

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Go(func() {
			statuses[i], _ = a.upload(editor, libs[i], "Part "+string(rune('A'+i))+".pdf", bytes.Repeat([]byte{byte('a' + i)}, 60))
		})
	}
	wg.Wait()
	if !(statuses[0] == 200 && statuses[1] == 413 || statuses[0] == 413 && statuses[1] == 200) {
		t.Errorf("two uploads of 60 bytes into 100: %v, want one taken and one refused", statuses)
	}
	if st := a.storage(editor); st["usedBytes"] != 60.0 {
		t.Errorf("used: %v", st)
	}
}
