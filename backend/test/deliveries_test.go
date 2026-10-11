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
}
