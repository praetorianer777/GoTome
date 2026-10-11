//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// koreader calls the progress sync as KOReader does: its own headers, the
// MD5 of the password in place of it.
func (a *app) koreader(user, token, method, path string, body any) (int, map[string]any) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, a.url+httpapi.APIPrefix+"/kosync"+path, reader)
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-auth-user", user)
	req.Header.Set("x-auth-key", auth.SyncKey(token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func md5Of(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func TestKOReaderSyncsTheProgressOfBooksThePersonSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	shared := a.library(admin, "Shared", "shared")
	vault := a.library(admin, "Vault", "private")
	epubBytes := []byte("PK an epub of sorts")
	_, epub := a.upload(admin, shared, "Emma.epub", epubBytes)
	pdfBytes := []byte("%PDF-1.4 a pdf of sorts")
	_, pdf := a.upload(admin, shared, "Pride.pdf", pdfBytes)
	secretBytes := []byte("PK a secret")
	_, secret := a.upload(admin, vault, "Secret.epub", secretBytes)
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET page_count = 100 WHERE id = $1`, pdf["fileId"]); err != nil {
		t.Fatal(err)
	}
	if _, err := a.server.Sync.HashFiles(ctx); err != nil {
		t.Fatal(err)
	}
	token := func(c *http.Client) string {
		_, made, _ := a.call(c, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
		return made["token"].(string)
	}
	readerToken, strangerToken := token(reader), token(stranger)

	if status, out := a.koreader("reader", readerToken, http.MethodGet, "/users/auth", nil); status != 200 || out["authorized"] != "OK" {
		t.Errorf("sign-in: %d %v", status, out)
	}
	if status, _ := a.koreader("reader", "wrong-token", http.MethodGet, "/users/auth", nil); status != 401 {
		t.Errorf("a wrong key: %d", status)
	}
	if status, _ := a.koreader("reader", readerToken, http.MethodPost, "/users/create", map[string]any{"username": "x", "password": "y"}); status != 403 {
		t.Errorf("registering: %d", status)
	}

	document := md5Of(epubBytes)
	xpointer := "/body/DocFragment[3]/body/p[2]/text().0"
	status, saved := a.koreader("reader", readerToken, http.MethodPut, "/syncs/progress", map[string]any{
		"document": document, "progress": xpointer, "percentage": 0.3, "device": "Tolino", "device_id": "TOLINO1",
	})
	if status != 200 || saved["document"] != document || saved["timestamp"] == nil {
		t.Fatalf("push: %d %v", status, saved)
	}
	_, web, _ := a.call(reader, http.MethodGet, "/books/"+epub["bookId"].(string)+"/progress", nil)
	ebook, _ := web["ebook"].(map[string]any)
	if ebook["locator"] != "koreader:"+xpointer || ebook["fraction"] != 0.3 || ebook["fileId"] != epub["fileId"] {
		t.Errorf("as the web reader sees it: %v", ebook)
	}
	if status, got := a.koreader("reader", readerToken, http.MethodGet, "/syncs/progress/"+document, nil); status != 200 ||
		got["progress"] != xpointer || got["percentage"] != 0.3 || got["device_id"] != "TOLINO1" {
		t.Errorf("pull: %d %v", status, got)
	}

	// The web reader reads on; KOReader is offered its chapter.
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+epub["bookId"].(string)+"/progress/ebook", map[string]any{
		"fileId": epub["fileId"], "locator": "epubcfi(/6/14!/4/2/1:0)", "fraction": 0.5, "clientId": "browser",
	}); status != 200 || out["saved"] != true {
		t.Fatalf("the web reader: %d %v", status, out)
	}
	if _, got := a.koreader("reader", readerToken, http.MethodGet, "/syncs/progress/"+document, nil); got["progress"] != "/body/DocFragment[7]" ||
		got["percentage"] != 0.5 || got["device"] != "GOtome" {
		t.Errorf("pull after the web reader: %v", got)
	}
	// A place behind it from KOReader is not written over it.
	a.koreader("reader", readerToken, http.MethodPut, "/syncs/progress", map[string]any{
		"document": document, "progress": "/body/DocFragment[2]", "percentage": 0.2, "device": "Tolino", "device_id": "TOLINO1",
	})
	if _, got := a.koreader("reader", readerToken, http.MethodGet, "/syncs/progress/"+document, nil); got["percentage"] != 0.5 {
		t.Errorf("after a place behind: %v", got)
	}

	// A PDF's place is its page.
	pdfDocument := md5Of(pdfBytes)
	if status, _ := a.koreader("reader", readerToken, http.MethodPut, "/syncs/progress", map[string]any{
		"document": pdfDocument, "progress": "12", "percentage": 0.12, "device": "Tolino", "device_id": "TOLINO1",
	}); status != 200 {
		t.Fatalf("push a PDF: %d", status)
	}
	if _, got := a.koreader("reader", readerToken, http.MethodGet, "/syncs/progress/"+pdfDocument, nil); got["progress"] != "12" {
		t.Errorf("pull a PDF: %v", got)
	}

	// What the person does not see is a document GOtome does not know.
	secretDocument := md5Of(secretBytes)
	for name, d := range map[string]string{"unknown": md5Of([]byte("nothing")), "a book of a private library": secretDocument} {
		if status, got := a.koreader("reader", readerToken, http.MethodGet, "/syncs/progress/"+d, nil); status != 200 || len(got) != 0 {
			t.Errorf("pull %s: %d %v", name, status, got)
		}
		if status, _ := a.koreader("reader", readerToken, http.MethodPut, "/syncs/progress", map[string]any{
			"document": d, "progress": "/body", "percentage": 0.1, "device": "Tolino", "device_id": "TOLINO1",
		}); status != 404 {
			t.Errorf("push %s: %d", name, status)
		}
	}
	if _, got := a.koreader("stranger", strangerToken, http.MethodGet, "/syncs/progress/"+document, nil); len(got) != 0 {
		t.Errorf("someone else's place: %v", got)
	}
	_ = secret

	// New bytes are a new document, worked out again.
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET sha256 = sha256(sha256) WHERE id = $1`, epub["fileId"]); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	if err := a.pool.QueryRow(ctx, `SELECT koreader_md5 IS NULL FROM book_files WHERE id = $1`, epub["fileId"]).Scan(&cleared); err != nil || !cleared {
		t.Errorf("the MD5 of a changed file: cleared %v, %v", cleared, err)
	}
}
