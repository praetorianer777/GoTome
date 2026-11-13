//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	onDevice := func() map[string]map[string]any {
		t.Helper()
		status, out, _ := a.call(reader, http.MethodGet, "/devices/"+device+"/books", nil)
		if status != 200 {
			t.Fatalf("the device's books: %d %v", status, out)
		}
		books := map[string]map[string]any{}
		for _, b := range out["books"].([]any) {
			b := b.(map[string]any)
			books[b["book"].(map[string]any)["id"].(string)] = b
		}
		return books
	}
	books := onDevice()
	if b := books[emma]; b["state"] != "delivered" || b["syncedAt"] == nil || b["removable"] != true {
		t.Errorf("a book downloaded: %v", b)
	}
	if b := books[persuasion]; b["state"] != "synced" || b["deliveredAt"] != nil || b["removable"] != true {
		t.Errorf("a book the reader was told of: %v", b)
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

	// A book the reader has is taken off it by its next sync.
	items, h = sync("")
	if len(items) != 1 || items[0].NewEntitlement == nil {
		t.Fatalf("emma again: %s", mustJSON(items))
	}
	synctoken = h.Get("x-kobo-synctoken")
	resp, err = http.Get(items[0].NewEntitlement.BookMetadata.DownloadUrls[0].Url)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("download: %v %v", resp, err)
	}
	resp.Body.Close()
	b := onDevice()[emma]
	if b["state"] != "delivered" || b["removable"] != true {
		t.Fatalf("downloaded again: %v", b)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/deliveries/"+b["deliveryId"].(string), nil); status != 204 {
		t.Fatalf("taking it off: %d", status)
	}
	if items, _ := sync(synctoken); len(items) != 1 || items[0].ChangedEntitlement == nil || !items[0].ChangedEntitlement.BookEntitlement.IsRemoved {
		t.Errorf("taken off: %s", mustJSON(items))
	}
	if len(onDevice()) != 0 {
		t.Errorf("taken off, still listed: %v", onDevice())
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

func TestEveryKoboRouteAnswersAReaderWithItsToken(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	vault := a.library(admin, "Vault", "private")
	hidden := a.library(admin, "Hidden", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	epub := func(library, title string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), title+".epub")
		writeEPUB(t, path, emmaMetadata, nil, "Chapter one of "+title+".")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		status, up := a.upload(admin, library, title+".epub", content)
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		book := up["bookId"].(string)
		if status, out := a.putCover(admin, book, coverPNG(t)); status != 200 {
			t.Fatalf("cover: %d %v", status, out)
		}
		return book
	}
	emma, secret := epub(vault, "Emma"), epub(hidden, "Secret")
	const nobody = "00000000-0000-0000-0000-000000000000"
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	token := made["token"].(string)
	if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{emma}}); status != 200 || out["queued"] != 1.0 {
		t.Fatalf("send: %d %v", status, out)
	}

	called := map[string]bool{}
	call := func(id, method, path string, body any, want int) []byte {
		t.Helper()
		called[id] = true
		status, raw, _ := a.kobo(token, method, path, "", body)
		if status != want {
			t.Errorf("%s %s: %d, want %d: %s", method, path, status, want, raw)
		}
		return raw
	}
	exactly := func(id, method, path string, body any, want string) {
		t.Helper()
		if raw := call(id, method, path, body, 200); strings.TrimSpace(string(raw)) != want {
			t.Errorf("%s %s: %s, want %s", method, path, raw, want)
		}
	}

	var init struct{ Resources map[string]string }
	if err := json.Unmarshal(call("koboInitialization", http.MethodGet, "/v1/initialization", nil, 200), &init); err != nil {
		t.Fatal(err)
	}
	for key, path := range map[string]string{
		"tags": "/v1/library/tags", "user_wishlist": "/v1/user/wishlist", "device_auth": "/v1/auth/device",
		"device_refresh": "/v1/auth/refresh", "post_analytics_event": "/v1/analytics/event", "reading_state": "/v1/library/{Ids}/state",
	} {
		if got := init.Resources[key]; !strings.HasSuffix(got, "/kobo/"+token+path) {
			t.Errorf("%s is %q", key, got)
		}
	}
	if got := init.Resources["user_recommendations"]; got != "https://storeapi.kobo.com/v1/user/recommendations" {
		t.Errorf("user_recommendations is %q", got)
	}

	var items []koboItem
	if err := json.Unmarshal(call("koboSync", http.MethodGet, "/v1/library/sync", nil, 200), &items); err != nil || len(items) != 1 || items[0].NewEntitlement == nil {
		t.Fatalf("sync: %v %s", err, mustJSON(items))
	}
	var metadata []struct {
		EntitlementID string `json:"EntitlementId"`
		CoverImageID  string `json:"CoverImageId"`
		Title         string
	}
	if err := json.Unmarshal(call("koboMetadata", http.MethodGet, "/v1/library/"+emma+"/metadata", nil, 200), &metadata); err != nil ||
		len(metadata) != 1 || metadata[0].EntitlementID != emma || metadata[0].CoverImageID != emma {
		t.Errorf("metadata: %v %+v", err, metadata)
	}

	type readingState struct {
		EntitlementID   string `json:"EntitlementId"`
		StatusInfo      struct{ Status string }
		CurrentBookmark struct {
			ProgressPercent *float64
		}
	}
	state := func() readingState {
		t.Helper()
		var states []readingState
		if err := json.Unmarshal(call("koboReadingState", http.MethodGet, "/v1/library/"+emma+"/state", nil, 200), &states); err != nil || len(states) != 1 {
			t.Fatalf("state: %v %+v", err, states)
		}
		return states[0]
	}
	if st := state(); st.EntitlementID != emma || st.StatusInfo.Status != "ReadyToRead" || st.CurrentBookmark.ProgressPercent != nil {
		t.Errorf("the state of a book not begun: %+v", st)
	}
	put := func(bookmark map[string]any, status string) string {
		t.Helper()
		return string(call("koboPutReadingState", http.MethodPut, "/v1/library/"+emma+"/state", map[string]any{
			"ReadingStates": []map[string]any{{"EntitlementId": emma, "CurrentBookmark": bookmark, "StatusInfo": map[string]any{"Status": status}}},
		}, 200))
	}
	if raw := put(map[string]any{"ProgressPercent": 42}, "Reading"); !strings.Contains(raw, `"CurrentBookmarkResult":{"Result":"Success"}`) {
		t.Errorf("put state: %s", raw)
	}
	if st := state(); st.StatusInfo.Status != "Reading" || st.CurrentBookmark.ProgressPercent == nil || *st.CurrentBookmark.ProgressPercent != 42 {
		t.Errorf("the state read back: %+v", st)
	}
	if raw := put(map[string]any{}, "Reading"); !strings.Contains(raw, `"CurrentBookmarkResult":{"Result":"Ignored"}`) {
		t.Errorf("a state without a place: %s", raw)
	}
	put(map[string]any{"ProgressPercent": 99}, "Finished")
	if st := state(); st.StatusInfo.Status != "Finished" || *st.CurrentBookmark.ProgressPercent != 100 {
		t.Errorf("a finished book: %+v", st)
	}

	cover := func(id, path, size string) {
		t.Helper()
		called[id] = true
		resp, err := http.Get(a.url + httpapi.APIPrefix + "/kobo/" + token + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix(body, []byte{0xff, 0xd8}) ||
			!strings.HasSuffix(resp.Header.Get("ETag"), "-"+size+`"`) {
			t.Errorf("%s: %d %s %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("ETag"))
		}
	}
	cover("koboCover", "/"+emma+"/150/200/false/image.jpg", "small")
	cover("koboCover", "/"+emma+"/1072/1448/false/image.jpg", "large")
	cover("koboCoverQuality", "/"+emma+"/150/200/80/true/image.jpg", "small")
	cover("koboCoverQuality", "/"+emma+"/1264/1680/80/false/image.jpg", "large")

	// What belongs to a library the reader does not see answers as what
	// does not exist.
	for _, book := range []string{secret, nobody} {
		call("koboMetadata", http.MethodGet, "/v1/library/"+book+"/metadata", nil, 404)
		call("koboReadingState", http.MethodGet, "/v1/library/"+book+"/state", nil, 404)
		call("koboCover", http.MethodGet, "/"+book+"/150/200/false/image.jpg", nil, 404)
		call("koboCoverQuality", http.MethodGet, "/"+book+"/150/200/80/false/image.jpg", nil, 404)
	}

	exactly("koboTags", http.MethodGet, "/v1/library/tags", nil, "[]")
	exactly("koboWishlist", http.MethodGet, "/v1/user/wishlist", nil, "[]")
	exactly("koboRecommendations", http.MethodGet, "/v1/user/recommendations", nil, "[]")
	exactly("koboProfile", http.MethodGet, "/v1/user/profile", nil, "{}")
	exactly("koboBenefits", http.MethodGet, "/v1/user/loyalty/benefits", nil, "{}")
	exactly("koboDeals", http.MethodGet, "/v1/deals", nil, "{}")
	exactly("koboAnalyticsTests", http.MethodPost, "/v1/analytics/gettests", map[string]any{}, "{}")
	exactly("koboAnalyticsEvent", http.MethodPost, "/v1/analytics/event", map[string]any{"Events": []any{map[string]any{"EventType": "OpenContent"}}}, "{}")
	const none = `{"Items":[],"TotalPageCount":0,"PageIndex":0,"PageSize":0}`
	exactly("koboFeatured", http.MethodGet, "/v1/products/featured", nil, none)
	exactly("koboFeaturedSlash", http.MethodGet, "/v1/products/featured/", nil, none)
	exactly("koboFeaturedList", http.MethodGet, "/v1/products/featured/some-list", nil, none)
	exactly("koboNextRead", http.MethodGet, "/v1/products/"+emma+"/nextread", nil, none)
	exactly("koboAffiliate", http.MethodGet, "/v1/affiliate", nil, "{}")
	for id, path := range map[string]string{"koboAuthDevice": "/v1/auth/device", "koboAuthRefresh": "/v1/auth/refresh"} {
		var tokens struct{ AccessToken, RefreshToken, TokenType, TrackingID, UserKey string }
		if err := json.Unmarshal(call(id, http.MethodPost, path, map[string]any{"UserKey": "kobo-user-key"}, 200), &tokens); err != nil ||
			tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.AccessToken == tokens.RefreshToken ||
			tokens.TokenType != "Bearer" || tokens.TrackingID == "" || tokens.UserKey != "kobo-user-key" {
			t.Errorf("%s: %v %+v", path, err, tokens)
		}
	}

	called["koboDownload"] = true
	resp, err := http.Get(items[0].NewEntitlement.BookMetadata.DownloadUrls[0].Url)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/epub+zip" || !bytes.HasPrefix(body, []byte("PK")) {
		t.Errorf("download: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
	called["koboDownloadKepub"] = true
	resp, err = http.Get(items[0].NewEntitlement.BookMetadata.DownloadUrls[0].Url + "/kepub")
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || !bytes.HasPrefix(body, []byte("PK")) || !strings.Contains(resp.Header.Get("Content-Disposition"), ".kepub.epub") {
		t.Errorf("KEPUB download: %d %s", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
	resp.Body.Close()
	call("koboArchive", http.MethodDelete, "/v1/library/"+emma, nil, 204)
	if _, d, _ := a.call(reader, http.MethodGet, "/books/"+emma+"/deliveries", nil); len(d["deliveries"].([]any)) != 0 {
		t.Errorf("archived, still sent: %v", d)
	}

	for _, rt := range a.server.Table() {
		if strings.HasPrefix(rt.Path, "/kobo/") && !called[rt.ID] {
			t.Errorf("%s %s is not called here", rt.Method, rt.Path)
		}
	}
}

// A state as a Kobo reader sends it, with the fields the firmware adds
// (values invented).
func koboStateBody(book string, percent float64, spent int, modified string) map[string]any {
	return map[string]any{"ReadingStates": []map[string]any{{
		"EntitlementId": book, "Created": modified, "LastModified": modified, "PriorityTimestamp": modified,
		"StatusInfo": map[string]any{"LastModified": modified, "Status": "Reading", "TimesStartedReading": 1},
		"Statistics": map[string]any{"LastModified": modified, "SpentReadingMinutes": spent, "RemainingTimeMinutes": 300},
		"CurrentBookmark": map[string]any{
			"LastModified": modified, "ProgressPercent": percent, "ContentSourceProgressPercent": percent / 2,
			"Location": map[string]any{"Value": "kobo.3.1", "Type": "KoboSpan", "Source": "OEBPS/chapter3.xhtml"},
		},
	}}}
}

func TestAKoboReadersReadingTimeIsCountedOnceInTheStatistics(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	lib := a.library(admin, "Shelf", "shared")
	path := filepath.Join(t.TempDir(), "Emma.epub")
	writeEPUB(t, path, emmaMetadata, nil, "Chapter one of Emma.")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	status, up := a.upload(admin, lib, "Emma.epub", content)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	emma := up["bookId"].(string)
	if _, err := a.pool.Exec(context.Background(), `UPDATE books SET page_count = 400 WHERE id = $1`, emma); err != nil {
		t.Fatal(err)
	}
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	token := made["token"].(string)
	if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{emma}}); status != 200 {
		t.Fatalf("send: %d %v", status, out)
	}
	if status, raw, _ := a.kobo(token, http.MethodGet, "/v1/library/sync", "", nil); status != 200 {
		t.Fatalf("sync: %d %s", status, raw)
	}
	put := func(percent float64, spent int, at time.Time) {
		t.Helper()
		status, raw, _ := a.kobo(token, http.MethodPut, "/v1/library/"+emma+"/state", "", koboStateBody(emma, percent, spent, at.UTC().Format("2006-01-02T15:04:05Z")))
		if status != 200 {
			t.Fatalf("put state: %d %s", status, raw)
		}
	}
	type sessionRow struct {
		Seconds    float64
		From, To   float64
		EndedHours float64
	}
	sessions := func() []sessionRow {
		t.Helper()
		rows, err := a.pool.Query(context.Background(), `SELECT extract(epoch FROM ended_at - started_at)::float8, from_fraction, to_fraction,
			extract(epoch FROM now() - ended_at)::float8 / 3600 FROM reading_sessions WHERE user_id = $1 AND client_id LIKE 'kobo:%' ORDER BY started_at`, readerID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []sessionRow
		for rows.Next() {
			var r sessionRow
			if err := rows.Scan(&r.Seconds, &r.From, &r.To, &r.EndedHours); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}

	now := time.Now()
	put(10, 25, now.Add(-2*time.Hour))
	put(10, 25, now.Add(-2*time.Hour)) // sent again: the same minutes
	got := sessions()
	if len(got) != 1 || got[0].Seconds != 25*60 || got[0].From != 0 || got[0].To != 0.1 || got[0].EndedHours < 1.9 || got[0].EndedHours > 2.1 {
		t.Fatalf("after the first report: %+v", got)
	}
	put(30, 40, now.Add(-time.Hour))
	got = sessions()
	if len(got) != 2 || got[1].Seconds != 15*60 || got[1].From != 0.1 || got[1].To != 0.3 {
		t.Fatalf("after the second: %+v", got)
	}
	put(30, 40, now.Add(-time.Hour))
	if got = sessions(); len(got) != 2 {
		t.Fatalf("a repeated report made a session: %+v", got)
	}
	put(35, 5, now) // the reader was reset: its count starts over, no time to add
	put(35, 8, now)
	if got = sessions(); len(got) != 4 || got[2].Seconds != 3*60 || got[3].Seconds > 5 {
		t.Fatalf("after a reset: %+v", got)
	}

	status, stats, _ := a.call(reader, http.MethodGet, "/me/stats?days=7&tz=UTC", nil)
	if status != 200 {
		t.Fatalf("stats: %d %v", status, stats)
	}
	if stats["totalReadingMinutes"].(float64) != 43 {
		t.Errorf("reading minutes %v, want 43", stats["totalReadingMinutes"])
	}
	if pages := stats["totalPages"].(float64); pages < 139 || pages > 141 {
		t.Errorf("pages %v, want 140 (35%% of 400)", pages)
	}

	// An analytics event is taken and counts for nothing.
	if status, _, _ := a.kobo(token, http.MethodPost, "/v1/analytics/event", "", map[string]any{
		"Events": []any{map[string]any{"EventType": "LeaveContent", "Metrics": map[string]any{"SecondsRead": 600}}}}); status != 200 {
		t.Errorf("analytics event: %d", status)
	}
	if got = sessions(); len(got) != 4 {
		t.Errorf("an analytics event made a session: %+v", got)
	}
}
