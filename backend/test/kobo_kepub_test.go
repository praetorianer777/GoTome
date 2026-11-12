//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

func TestAKoboReaderIsSentAKepubOnlyWhereItsOwnerSwitchedThatOn(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	path := filepath.Join(t.TempDir(), "Emma.epub")
	writeEPUB(t, path, emmaMetadata, nil, "Chapter one of Emma. A second sentence follows!")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	status, up := a.upload(admin, lib, "Emma.epub", original)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	book := up["bookId"].(string)
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	if made["device"].(map[string]any)["kepub"] != false {
		t.Errorf("a new device is sent KEPUBs: %v", made["device"])
	}
	token := made["token"].(string)
	if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{book}}); status != 200 {
		t.Fatalf("send: %d %v", status, out)
	}
	newEntitlement := func() []struct{ Format, Url string } {
		t.Helper()
		status, raw, _ := a.kobo(token, http.MethodGet, "/v1/library/sync", "", nil)
		var items []koboItem
		if status != 200 || json.Unmarshal(raw, &items) != nil || len(items) != 1 || items[0].NewEntitlement == nil {
			t.Fatalf("sync: %d %s", status, raw)
		}
		return items[0].NewEntitlement.BookMetadata.DownloadUrls
	}

	get := func(url string) (int, http.Header, []byte) {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, b
	}

	urls := newEntitlement()
	if len(urls) != 2 || urls[0].Format != "EPUB3" || strings.HasSuffix(urls[0].Url, "/kepub") {
		t.Fatalf("with the switch off: %+v", urls)
	}
	status, header, body := get(urls[0].Url)
	if status != 200 || !bytes.Equal(body, original) || strings.Contains(header.Get("Content-Disposition"), "kepub") {
		t.Fatalf("the EPUB: %d %s", status, header.Get("Content-Disposition"))
	}

	if status, out, _ := a.call(reader, http.MethodPatch, "/auth/devices/"+device, map[string]any{"kepub": true}); status != 200 || out["kepub"] != true {
		t.Fatalf("switching on: %d %v", status, out)
	}
	urls = newEntitlement()
	if len(urls) != 1 || urls[0].Format != "KEPUB" || !strings.HasSuffix(urls[0].Url, "/kepub") {
		t.Fatalf("with the switch on: %+v", urls)
	}
	status, header, kepub := get(urls[0].Url)
	if status != 200 || bytes.Equal(kepub, original) {
		t.Fatalf("the KEPUB: %d", status)
	}
	if !strings.Contains(header.Get("Content-Disposition"), "Emma.kepub.epub") {
		t.Errorf("the name: %q", header.Get("Content-Disposition"))
	}
	sum := sha256.Sum256(kepub)
	if header.Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` {
		t.Errorf("the ETag is not the KEPUB's hash: %q", header.Get("ETag"))
	}
	zr, err := zip.NewReader(bytes.NewReader(kepub), int64(len(kepub)))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "OEBPS/ch0.xhtml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			doc = string(b)
		}
	}
	if !strings.Contains(doc, `<span class="koboSpan" id="kobo.1.1">Chapter one of Emma.</span> <span class="koboSpan" id="kobo.1.2">A second sentence follows!</span>`) {
		t.Errorf("the document: %s", doc)
	}

	req, _ := http.NewRequest(http.MethodGet, urls[0].Url, nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 206 || resp.Header.Get("ETag") != header.Get("ETag") {
		t.Fatalf("a part: %v %v", resp, err)
	}
	resp.Body.Close()

	// The OPDS download of the same device stays the EPUB.
	_, books, _ := a.call(reader, http.MethodGet, "/books/"+book, nil)
	fileID := books["files"].([]any)[0].(map[string]any)["id"].(string)
	dev := deviceClient("reader", token)
	if status, _, body := a.fetch(dev, httpapi.APIPrefix+"/opds/device/files/"+fileID); status != 200 || !bytes.Equal(body, original) {
		t.Errorf("the OPDS download: %d", status)
	}

	if status, _, _ := a.call(reader, http.MethodPatch, "/auth/devices/00000000-0000-0000-0000-000000000000", map[string]any{"kepub": true}); status != 404 {
		t.Errorf("a device that is nobody's: %d", status)
	}

	// A KEPUB outlives neither its file's hash nor the file.
	cache, _ := filepath.Glob(filepath.Join(a.dataDir, "device-copies", "*.kepub.epub"))
	if len(cache) != 1 {
		t.Fatalf("%d KEPUBs in the cache", len(cache))
	}
	if err := a.copies.Trim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache[0]); err != nil {
		t.Errorf("a KEPUB of a file in the library was removed: %v", err)
	}
	if _, err := a.pool.Exec(context.Background(), `UPDATE book_files SET sha256 = decode(repeat('00', 32), 'hex') WHERE id = $1`, fileID); err != nil {
		t.Fatal(err)
	}
	if err := a.copies.Trim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache[0]); err == nil {
		t.Error("the KEPUB of a file that changed stays")
	}
}
