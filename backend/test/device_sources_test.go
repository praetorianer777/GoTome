//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestADeviceIsStockedFromTheShelvesItFollows(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	vault := a.library(admin, "Vault", "private")
	if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("membership: %d", status)
	}
	upload := func(name string) string {
		t.Helper()
		status, up := a.upload(admin, vault, name+".pdf", []byte("%PDF-1.4 "+name))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		return up["bookId"].(string)
	}
	one, two, three := upload("One"), upload("Two"), upload("Three")

	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	status, col, _ := a.call(reader, http.MethodPost, "/collections", map[string]any{"name": "Next", "visibility": "private"})
	if status >= 300 {
		t.Fatalf("collection: %d %v", status, col)
	}
	next := col["id"].(string)
	add := func(book string) {
		t.Helper()
		if status, out, _ := a.call(reader, http.MethodPost, "/collections/"+next+"/books", map[string]any{"books": []string{book}}); status >= 300 {
			t.Fatalf("add: %d %v", status, out)
		}
	}
	add(one)
	follow := func(sources ...map[string]any) {
		t.Helper()
		if status, out, _ := a.call(reader, http.MethodPut, "/devices/"+device+"/sources", map[string]any{"sources": sources}); status != 200 {
			t.Fatalf("follow: %d %v", status, out)
		}
	}
	sync := func() {
		t.Helper()
		if err := a.server.DeviceSources.SyncUser(context.Background(), uuid.MustParse(readerID)); err != nil {
			t.Fatal(err)
		}
	}
	onDevice := func() map[string][]any {
		t.Helper()
		status, out, _ := a.call(reader, http.MethodGet, "/devices/"+device+"/books", nil)
		if status != 200 {
			t.Fatalf("books: %d %v", status, out)
		}
		got := map[string][]any{}
		for _, b := range out["books"].([]any) {
			b := b.(map[string]any)
			got[b["book"].(map[string]any)["id"].(string)] = b["origins"].([]any)
		}
		return got
	}
	has := func(got map[string][]any, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("the device holds %d books, want %d: %v", len(got), len(want), got)
		}
		for _, w := range want {
			if _, ok := got[w]; !ok {
				t.Fatalf("the device lacks %s: %v", w, got)
			}
		}
	}

	if status, _, _ := a.call(reader, http.MethodPut, "/devices/"+device+"/sources", map[string]any{"sources": []map[string]any{{"kind": "collection", "shelf": uuid.NewString()}}}); status != 422 {
		t.Errorf("following a shelf that does not exist: %d", status)
	}
	follow(map[string]any{"kind": "collection", "shelf": next})
	sync()
	got := onDevice()
	has(got, one)
	if o := got[one][0].(map[string]any); o["kind"] != "collection" || o["name"] != "Next" {
		t.Errorf("origin: %v", got[one])
	}

	add(two)
	sync()
	has(onDevice(), one, two)

	if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{one}}); status != 200 {
		t.Fatalf("send by hand: %d %v", status, out)
	}
	for _, b := range []string{one, two} {
		if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+next+"/books/"+b, nil); status != 204 {
			t.Fatalf("remove: %d", status)
		}
	}
	sync()
	got = onDevice()
	has(got, one)
	if len(got[one]) != 0 {
		t.Errorf("a book sent by hand has no origin once the shelf lets go: %v", got[one])
	}

	smart := func() string {
		status, shelf, _ := a.call(reader, http.MethodPost, "/smart-shelves", map[string]any{
			"name": "Fives", "visibility": "private", "filter": `{"field":"rating","op":"in","values":["5"]}`})
		if status >= 300 {
			t.Fatalf("smart shelf: %d %v", status, shelf)
		}
		return shelf["id"].(string)
	}()
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+three+"/reading", map[string]any{"rating": 5, "status": "reading"}); status != 200 {
		t.Fatalf("rating: %d %v", status, out)
	}
	follow(map[string]any{"kind": "smart", "shelf": smart}, map[string]any{"kind": "reading"})
	sync()
	got = onDevice()
	has(got, one, three)
	if len(got[three]) != 2 {
		t.Errorf("a book on two sources names both: %v", got[three])
	}

	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+three+"/reading", map[string]any{"rating": 0}); status != 200 {
		t.Fatalf("unrate: %d %v", status, out)
	}
	sync()
	got = onDevice()
	has(got, one, three)
	if len(got[three]) != 1 || got[three][0].(map[string]any)["kind"] != "reading" {
		t.Errorf("the other source still holds it: %v", got[three])
	}

	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("remove member: %d", status)
	}
	sync()
	var rows int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE device_id = $1 AND book_id = $2`, device, three).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("a book of a library the owner lost stays on the device: %d %v", rows, err)
	}
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE device_id = $1`, device).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("only the book sent by hand is left: %d %v", rows, err)
	}

	follow()
	var sources []any
	_, list, _ := a.call(reader, http.MethodGet, "/devices/"+device+"/sources", nil)
	if sources = list["sources"].([]any); len(sources) != 0 {
		t.Errorf("sources after unfollowing: %v", sources)
	}
}
