package notify

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

func TestFoldingEventsOfALibraryLinksToThatLibrary(t *testing.T) {
	lib := uuid.New()
	event := func(kind string, library *uuid.UUID, data string) sqlc.NotificationEvent {
		return sqlc.NotificationEvent{Kind: kind, LibraryID: library, Data: []byte(data), Link: "/books/one"}
	}
	for _, c := range []struct {
		name  string
		group []sqlc.NotificationEvent
		link  string
		count float64
	}{
		{"books of a library", []sqlc.NotificationEvent{
			event(KindBooksAdded, &lib, `{"library":"Shelf","count":3}`), event(KindBooksAdded, &lib, `{"library":"Shelf","count":4}`),
		}, "/?library=" + lib.String(), 7},
		{"books of no library", []sqlc.NotificationEvent{
			event(KindBooksAdded, nil, `{"count":1}`), event(KindBooksAdded, nil, `{"count":1}`),
		}, "/", 2},
		{"a kind with a page of its own", []sqlc.NotificationEvent{
			event(KindWishFulfilled, &lib, `{}`), event(KindWishFulfilled, &lib, `{}`), event(KindWishFulfilled, &lib, `{}`),
		}, "/wishlist", 3},
	} {
		n, err := fold(c.group)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var data struct{ Count float64 }
		if err := json.Unmarshal(mustJSON(t, n.Data), &data); err != nil || data.Count != c.count {
			t.Errorf("%s: data %v, %v", c.name, n.Data, err)
		}
		if n.Link != c.link {
			t.Errorf("%s: link %q, want %q", c.name, n.Link, c.link)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
