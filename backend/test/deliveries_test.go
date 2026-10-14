//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

func TestABookSentToADeviceArrivesOnceAndOnlyWhileSeen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	vault := a.library(admin, "Vault", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	content := []byte("%PDF-1.4 the waves")
	status, up := a.upload(admin, vault, "Waves.pdf", content)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	book := up["bookId"].(string)
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	deviceID := made["device"].(map[string]any)["id"].(string)
	device := deviceClient("reader", made["token"].(string))
	_, other, _ := a.call(stranger, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})

	send := func(c *http.Client, to string) (int, map[string]any) {
		t.Helper()
		status, out, _ := a.call(c, http.MethodPost, "/devices/"+to+"/books", map[string]any{"books": []string{book}})
		return status, out
	}
	if status, _ := send(reader, other["device"].(map[string]any)["id"].(string)); status != 404 {
		t.Errorf("sending to someone else's device: %d", status)
	}
	if status, out := send(stranger, other["device"].(map[string]any)["id"].(string)); status != 200 || out["queued"] != 0.0 {
		t.Errorf("sending a book one does not see: %d %v", status, out)
	}
	if status, out := send(reader, deviceID); status != 200 || out["queued"] != 1.0 {
		t.Fatalf("send: %d %v", status, out)
	}
	if _, out := send(reader, deviceID); out["queued"] != 0.0 {
		t.Errorf("sending it again while it waits: %v", out)
	}
	deliveries := func() []any {
		t.Helper()
		status, out, _ := a.call(reader, http.MethodGet, "/books/"+book+"/deliveries", nil)
		if status != 200 {
			t.Fatalf("deliveries: %d %v", status, out)
		}
		return out["deliveries"].([]any)
	}
	if d := deliveries(); len(d) != 1 || d[0].(map[string]any)["deviceName"] != "Tolino" || d[0].(map[string]any)["deliveredAt"] != nil {
		t.Errorf("waiting: %v", d)
	}

	// A device's own app reads the same queue.
	status, mine, _ := a.call(device, http.MethodGet, "/device", nil)
	if status != 200 || mine["username"] != "reader" || mine["device"].(map[string]any)["id"] != deviceID {
		t.Errorf("the device itself: %d %v", status, mine)
	}
	if status, out, _ := a.call(device, http.MethodGet, "/device/books", nil); status != 200 ||
		len(out["books"].([]any)) != 1 || out["books"].([]any)[0].(map[string]any)["id"] != book {
		t.Errorf("the device's queue: %d %v", status, out)
	}
	for _, path := range []string{"/device", "/device/books"} {
		if status, _, _ := a.call(reader, http.MethodGet, path, nil); status != 404 {
			t.Errorf("%s in a browser: %d", path, status)
		}
	}

	// A browser has no feed of its own.
	if status, _, _ := a.fetch(reader, httpapi.APIPrefix+"/opds/device"); status != 404 {
		t.Errorf("the device feed in a browser: %d", status)
	}
	queued := a.feed(device, httpapi.APIPrefix+"/opds/device")
	if len(queued.Entries) != 1 || queued.Entries[0].ID != "urn:uuid:"+book {
		t.Fatalf("the device's feed: %+v", queued)
	}
	var download string
	for _, l := range queued.Entries[0].Links {
		if l.Rel == "http://opds-spec.org/acquisition" {
			download = l.Href
		}
	}
	if download != httpapi.APIPrefix+"/opds/device/files/"+up["fileId"].(string) {
		t.Fatalf("the download: %q", download)
	}
	// A part of the file is no delivery; the whole of it is.
	req, _ := http.NewRequest(http.MethodGet, a.url+download, nil)
	req.Header.Set("Range", "bytes=0-3")
	if resp, err := device.Do(req); err != nil || resp.StatusCode != 206 {
		t.Fatalf("a part: %v %v", resp, err)
	}
	if got := a.feed(device, httpapi.APIPrefix+"/opds/device"); len(got.Entries) != 1 {
		t.Errorf("after a part: %+v", got)
	}
	if status, _, body := a.fetch(device, download); status != 200 || string(body) != string(content) {
		t.Fatalf("download: %d %q", status, body)
	}
	if got := a.feed(device, httpapi.APIPrefix+"/opds/device"); len(got.Entries) != 0 {
		t.Errorf("after the download: %+v", got)
	}
	if status, out, _ := a.call(device, http.MethodGet, "/device/books", nil); status != 200 || len(out["books"].([]any)) != 0 {
		t.Errorf("the device's queue after the download: %d %v", status, out)
	}
	if d := deliveries(); len(d) != 1 || d[0].(map[string]any)["deliveredAt"] == nil {
		t.Errorf("delivered: %v", d)
	}
	if status, _ := send(reader, deviceID); status != 200 {
		t.Fatalf("sending it again: %d", status)
	}
	id := deliveries()[0].(map[string]any)["id"].(string)
	if status, _, _ := a.call(stranger, http.MethodDelete, "/deliveries/"+id, nil); status != 404 {
		t.Errorf("someone else taking it back: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/deliveries/"+id, nil); status != 204 {
		t.Errorf("taking it back: %d", status)
	}
	send(reader, deviceID)

	// What the device takes itself, and takes off again, its book's page says.
	if status, _, _ := a.call(device, http.MethodDelete, "/device/books/"+book, nil); status != 204 {
		t.Fatalf("taken off: %d", status)
	}
	if d := deliveries(); len(d) != 0 {
		t.Errorf("after taking it off: %v", d)
	}
	if status, _, _ := a.call(device, http.MethodPut, "/device/books/"+book, nil); status != 204 {
		t.Fatalf("put on: %d", status)
	}
	if d := deliveries(); len(d) != 1 || d[0].(map[string]any)["deliveredAt"] == nil {
		t.Errorf("after putting it on: %v", d)
	}
	if status, _, _ := a.call(reader, http.MethodPut, "/device/books/"+book, nil); status != 404 {
		t.Errorf("putting it on from a browser: %d", status)
	}
	a.call(device, http.MethodDelete, "/device/books/"+book, nil)
	send(reader, deviceID)

	// Out of the library, the book leaves the device's feed.
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("removing the member: %d", status)
	}
	if got := a.feed(device, httpapi.APIPrefix+"/opds/device"); len(got.Entries) != 0 {
		t.Errorf("a book no longer seen: %+v", got)
	}
	if status, _, _ := a.fetch(device, download); status != 404 {
		t.Errorf("its download: %d", status)
	}
	if status, out, _ := a.call(device, http.MethodGet, "/device/books", nil); status != 200 || len(out["books"].([]any)) != 0 {
		t.Errorf("the device's queue of a book no longer seen: %d %v", status, out)
	}
	if status, _, _ := a.call(device, http.MethodPut, "/device/books/"+book, nil); status != 404 {
		t.Errorf("putting a book no longer seen on the device: %d", status)
	}
}

func TestADevicesPageListsItsBooksTheLastSentFirst(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	vault := a.library(admin, "Vault", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	var books []string
	for _, name := range []string{"One", "Two", "Three"} {
		status, up := a.upload(admin, vault, name+".pdf", []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		books = append(books, up["bookId"].(string))
	}
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	deviceID := made["device"].(map[string]any)["id"].(string)
	device := deviceClient("reader", made["token"].(string))
	for _, b := range books {
		if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+deviceID+"/books", map[string]any{"books": []string{b}}); status != 200 || out["queued"] != 1.0 {
			t.Fatalf("send: %d %v", status, out)
		}
	}
	if status, _, _ := a.call(device, http.MethodPut, "/device/books/"+books[0], nil); status != 204 {
		t.Fatalf("put on: %d", status)
	}

	var listed []map[string]any
	cursor := ""
	for range 4 {
		path := "/devices/" + deviceID + "/books?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		status, out, _ := a.call(reader, http.MethodGet, path, nil)
		if status != 200 || out["device"].(map[string]any)["name"] != "Tolino" {
			t.Fatalf("the device's books: %d %v", status, out)
		}
		for _, b := range out["books"].([]any) {
			listed = append(listed, b.(map[string]any))
		}
		next, _ := out["nextCursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(listed) != 3 {
		t.Fatalf("listed: %v", listed)
	}
	for i, want := range []string{books[2], books[1], books[0]} {
		if got := listed[i]["book"].(map[string]any)["id"]; got != want {
			t.Errorf("book %d is %v, not %s", i, got, want)
		}
	}
	if b := listed[0]; b["state"] != "waiting" || b["removable"] != true || b["deliveredAt"] != nil {
		t.Errorf("a book waiting: %v", b)
	}
	// The device keeps what it downloaded, so GOtome does not offer to
	// take it off.
	held := listed[2]
	if held["state"] != "delivered" || held["removable"] != false || held["deliveredAt"] == nil {
		t.Errorf("a book on the device: %v", held)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/deliveries/"+held["deliveryId"].(string), nil); status != 404 {
		t.Errorf("taking a downloaded book off: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/deliveries/"+listed[0]["deliveryId"].(string), nil); status != 204 {
		t.Errorf("taking a waiting book back: %d", status)
	}

	if status, out, _ := a.call(stranger, http.MethodGet, "/devices/"+deviceID+"/books", nil); status != 404 {
		t.Errorf("someone else's device: %d %v", status, out)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/devices/"+deviceID+"/books?cursor=nonsense", nil); status != 422 {
		t.Errorf("a cursor made up: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("removing the member: %d", status)
	}
	if status, out, _ := a.call(reader, http.MethodGet, "/devices/"+deviceID+"/books", nil); status != 200 || len(out["books"].([]any)) != 0 {
		t.Errorf("books no longer seen: %d %v", status, out)
	}
}
