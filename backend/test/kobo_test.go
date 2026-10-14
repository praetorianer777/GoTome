//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// kobo calls GOtome as a Kobo reader whose api_endpoint carries the token.
func (a *app) kobo(token, method, path string, synctoken string, body any) (int, []byte, http.Header) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.url+httpapi.APIPrefix+"/kobo/"+token+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	// Kobo's own token, which a reader sends wherever its store is.
	req.Header.Set("Authorization", "Bearer kobo-store-token")
	if synctoken != "" {
		req.Header.Set("x-kobo-synctoken", synctoken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, resp.Header
}

type koboItem struct {
	NewEntitlement *struct {
		BookEntitlement struct {
			ID        string
			IsRemoved bool
		}
		BookMetadata struct {
			Title        string
			DownloadUrls []struct{ Format, Url string }
		}
		ReadingState *struct {
			CurrentBookmark struct{ ProgressPercent *float64 }
		}
	}
	ChangedEntitlement *struct {
		BookEntitlement struct {
			ID        string
			IsRemoved bool
		}
	}
	ChangedReadingState *struct {
		ReadingState struct {
			EntitlementID   string `json:"EntitlementId"`
			CurrentBookmark struct{ ProgressPercent *float64 }
		}
	}
}

func TestAKoboReaderSyncsItsBooksAndReadingPlaces(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	vault := a.library(admin, "Vault", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	epub := func(title string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), title+".epub")
		writeEPUB(t, path, emmaMetadata, nil, "Chapter one of "+title+".")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		status, up := a.upload(admin, vault, title+".epub", content)
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		return up["bookId"].(string)
	}
	emma, persuasion := epub("Emma"), epub("Persuasion")
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	token := made["token"].(string)
	send := func(book string) {
		t.Helper()
		if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{book}}); status != 200 || out["queued"] != 1.0 {
			t.Fatalf("send: %d %v", status, out)
		}
	}
	sync := func(synctoken string) ([]koboItem, http.Header) {
		t.Helper()
		status, raw, h := a.kobo(token, http.MethodGet, "/v1/library/sync", synctoken, nil)
		if status != 200 {
			t.Fatalf("sync: %d %s", status, raw)
		}
		var items []koboItem
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatalf("sync answer: %v %s", err, raw)
		}
		return items, h
	}

	status, raw, h := a.kobo(token, http.MethodGet, "/v1/initialization", "", nil)
	var init struct{ Resources map[string]string }
	if status != 200 || json.Unmarshal(raw, &init) != nil || h.Get("x-kobo-apitoken") == "" {
		t.Fatalf("initialization: %d %s", status, raw)
	}
	if got := init.Resources["library_sync"]; !strings.HasSuffix(got, "/kobo/"+token+"/v1/library/sync") {
		t.Errorf("library_sync is %q", got)
	}
	if got := init.Resources["store_host"]; got != "www.kobo.com" {
		t.Errorf("the store is %q", got)
	}
	for _, path := range []string{"/v1/user/profile", "/v1/user/loyalty/benefits", "/v1/deals"} {
		if status, raw, _ := a.kobo(token, http.MethodGet, path, "", nil); status != 200 {
			t.Errorf("%s: %d %s", path, status, raw)
		}
	}
	if status, raw, _ := a.kobo(token, http.MethodPost, "/v1/analytics/gettests", "", map[string]any{}); status != 200 {
		t.Errorf("gettests: %d %s", status, raw)
	}

	// A book sent arrives with its download, once.
	send(emma)
	items, h := sync("")
	if len(items) != 1 || items[0].NewEntitlement == nil || items[0].NewEntitlement.BookEntitlement.ID != emma {
		t.Fatalf("first sync: %+v", items)
	}
	synctoken := h.Get("x-kobo-synctoken")
	if synctoken == "" {
		t.Fatal("no sync token")
	}
	urls := items[0].NewEntitlement.BookMetadata.DownloadUrls
	if len(urls) != 2 || urls[0].Format != "EPUB3" || urls[1].Format != "EPUB" || !strings.Contains(urls[0].Url, "/kobo/"+token+"/download/") {
		t.Fatalf("downloads: %+v", urls)
	}
	resp, err := http.Get(urls[0].Url)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("download: %v %v", resp, err)
	}
	resp.Body.Close()
	if items, _ := sync(synctoken); len(items) != 0 {
		t.Errorf("a sync without changes: %+v", items)
	}
	if status, raw, _ := a.kobo(token, http.MethodGet, "/v1/library/"+emma+"/metadata", synctoken, nil); status != 200 || !strings.Contains(string(raw), `"EntitlementId":"`+emma) {
		t.Errorf("metadata: %d %s", status, raw)
	}

	// Reading on the reader moves the place in GOtome.
	status, raw, _ = a.kobo(token, http.MethodPut, "/v1/library/"+emma+"/state", synctoken, map[string]any{
		"ReadingStates": []map[string]any{{"EntitlementId": emma, "CurrentBookmark": map[string]any{"ProgressPercent": 42}, "StatusInfo": map[string]any{"Status": "Reading"}}},
	})
	if status != 200 || !strings.Contains(string(raw), `"CurrentBookmarkResult":{"Result":"Success"}`) {
		t.Fatalf("put state: %d %s", status, raw)
	}
	_, progress, _ := a.call(reader, http.MethodGet, "/books/"+emma+"/progress", nil)
	ebook := progress["ebook"].(map[string]any)
	if ebook["fraction"] != 0.42 || !strings.HasPrefix(ebook["clientId"].(string), "kobo:") {
		t.Errorf("the place in GOtome: %v", ebook)
	}
	if items, _ := sync(synctoken); len(items) != 0 {
		t.Errorf("the reader is told of its own place: %+v", items)
	}

	// Reading further in the browser moves it on the reader.
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+emma+"/progress/ebook", map[string]any{
		"locator": "epubcfi(/6/4)", "fraction": 0.6, "clientId": "browser", "basedOn": ebook["updatedAt"],
	}); status != 200 || out["saved"] != true {
		t.Fatalf("the browser's place: %d %v", status, out)
	}
	items, _ = sync(synctoken)
	if len(items) != 1 || items[0].ChangedReadingState == nil || *items[0].ChangedReadingState.ReadingState.CurrentBookmark.ProgressPercent != 60 {
		t.Fatalf("the browser's place on the reader: %s", mustJSON(items))
	}

	// A book taken off the queue leaves the reader; one archived there
	// leaves the queue.
	send(persuasion)
	items, _ = sync(synctoken)
	if len(items) != 1 || items[0].NewEntitlement == nil || items[0].NewEntitlement.BookEntitlement.ID != persuasion {
		t.Fatalf("the second book: %s", mustJSON(items))
	}
	_, deliveries, _ := a.call(reader, http.MethodGet, "/books/"+persuasion+"/deliveries", nil)
	waiting := deliveries["deliveries"].([]any)[0].(map[string]any)["id"].(string)
	if status, _, _ := a.call(reader, http.MethodDelete, "/deliveries/"+waiting, nil); status != 204 {
		t.Fatalf("taking it back: %d", status)
	}
	items, _ = sync(synctoken)
	if len(items) != 1 || items[0].ChangedEntitlement == nil || !items[0].ChangedEntitlement.BookEntitlement.IsRemoved {
		t.Fatalf("taken back: %s", mustJSON(items))
	}
	if status, _, _ := a.kobo(token, http.MethodDelete, "/v1/library/"+emma, synctoken, nil); status != 204 {
		t.Fatalf("archive: %d", status)
	}
	if _, d, _ := a.call(reader, http.MethodGet, "/books/"+emma+"/deliveries", nil); len(d["deliveries"].([]any)) != 0 {
		t.Errorf("archived, still sent: %v", d)
	}
	if items, _ := sync(synctoken); len(items) != 0 {
		t.Errorf("after archiving: %s", mustJSON(items))
	}

	// A reader that syncs from the start, or with a token of an older
	// GOtome, is told everything again.
	send(emma)
	if items, _ := sync(""); len(items) != 1 || items[0].NewEntitlement == nil {
		t.Errorf("from the start: %s", mustJSON(items))
	}
	if items, _ := sync(synctoken); len(items) != 0 {
		t.Errorf("after the start: %s", mustJSON(items))
	}
	if items, _ := sync("Z290b21l"); len(items) != 1 || items[0].NewEntitlement == nil {
		t.Errorf("with an older token: %s", mustJSON(items))
	}

	// A wrong or revoked token reaches nothing, and the log keeps no token.
	if status, _, _ := a.kobo("abcd-efgh-ijkm-npqr-stuv", http.MethodGet, "/v1/library/sync", "", nil); status != 401 {
		t.Errorf("a wrong token: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/auth/devices/"+device, nil); status != 204 {
		t.Fatalf("revoke: %d", status)
	}
	if status, _, _ := a.kobo(token, http.MethodGet, "/v1/initialization", "", nil); status != 401 {
		t.Errorf("a revoked token: %d", status)
	}
	if strings.Contains(a.logs.String(), token) {
		t.Error("the log holds the device token")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
