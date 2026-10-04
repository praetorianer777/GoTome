//go:build integration

package test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// events opens the caller's notification stream and hands each event's
// data on the channel until the test ends.
func (a *app) events(c *http.Client) <-chan string {
	a.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url+httpapi.APIPrefix+"/notifications/stream", nil)
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		a.t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	out := make(chan string, 16)
	go func() {
		defer resp.Body.Close()
		lines := bufio.NewScanner(resp.Body)
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				out <- data
			}
		}
	}()
	return out
}

func next(t *testing.T, events <-chan string) string {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event within five seconds")
		return ""
	}
}

func TestNotificationsReachTheirOwnerAtOnceAndStayUnreadUntilRead(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	emma, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Emma"})
	if err != nil {
		t.Fatal(err)
	}

	mine, others := a.events(editor), a.events(reader)
	if e := next(t, mine); e != `{"unread":0}` {
		t.Fatalf("the stream opens with %s", e)
	}
	next(t, others)

	// A bulk change that finishes tells whoever asked, in the stream, once.
	bulk := a.runBulk(editor, map[string]any{"books": []string{emma.String()}, "action": "writeBack"})
	if e := next(t, mine); e != `{"unread":1}` {
		t.Errorf("after the bulk change: %s", e)
	}
	if _, err := a.server.Bulk.Run(context.Background(), uuid.MustParse(bulk["id"].(string))); err != nil {
		t.Fatal(err)
	}
	_, list, _ := a.call(editor, http.MethodGet, "/notifications", nil)
	items := list["notifications"].([]any)
	if len(items) != 1 || list["unread"].(float64) != 1 {
		t.Fatalf("the editor's notifications: %v", list)
	}
	n := items[0].(map[string]any)
	if n["kind"] != "bulk.finished" || n["link"] != "/bulk/"+bulk["id"].(string) || n["readAt"] != nil ||
		n["data"].(map[string]any)["action"] != "writeBack" {
		t.Errorf("the notification: %v", n)
	}
	select {
	case e := <-others:
		t.Errorf("the reader's stream heard of the editor's notification: %s", e)
	case <-time.After(300 * time.Millisecond):
	}
	if _, list, _ := a.call(reader, http.MethodGet, "/notifications", nil); len(list["notifications"].([]any)) != 0 || list["unread"].(float64) != 0 {
		t.Errorf("the reader sees %v", list)
	}

	// Pages, newest first.
	for i := range 3 {
		if err := db.InTx(context.Background(), a.pool, func(tx pgx.Tx) error {
			_, err := notify.CreateTx(context.Background(), tx, uuid.MustParse(editorID), notify.New{Kind: notify.KindBulkFinished, Data: map[string]int{"n": i}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, first, _ := a.call(editor, http.MethodGet, "/notifications?limit=2", nil)
	_, second, _ := a.call(editor, http.MethodGet, "/notifications?limit=2&cursor="+first["nextCursor"].(string), nil)
	page := func(l map[string]any) (out []any) {
		for _, n := range l["notifications"].([]any) {
			out = append(out, n.(map[string]any)["data"].(map[string]any)["n"])
		}
		return out
	}
	if p1, p2 := page(first), page(second); len(p1) != 2 || p1[0] != 2.0 || p1[1] != 1.0 || len(p2) != 2 || p2[0] != 0.0 || second["nextCursor"] != nil {
		t.Errorf("pages: %v %v (%v)", p1, p2, second["nextCursor"])
	}
	if first["unread"].(float64) != 4 {
		t.Errorf("unread: %v", first["unread"])
	}

	// Marking read is told to every window, and lasts.
	for range 3 {
		next(t, mine)
	}
	id := n["id"].(string)
	if status, out, _ := a.call(editor, http.MethodPost, "/notifications/read", map[string]any{"ids": []string{id}}); status != 200 || out["unread"].(float64) != 3 {
		t.Errorf("mark one: %d %v", status, out)
	}
	if e := next(t, mine); e != `{"unread":3}` {
		t.Errorf("after marking one read: %s", e)
	}
	// Someone else's notification is not theirs to mark.
	a.call(reader, http.MethodPost, "/notifications/read", map[string]any{"all": true})
	if _, list, _ := a.call(editor, http.MethodGet, "/notifications", nil); list["unread"].(float64) != 3 {
		t.Errorf("the reader marked the editor's: %v", list["unread"])
	}
	if status, out, _ := a.call(editor, http.MethodPost, "/notifications/read", map[string]any{"all": true}); status != 200 || out["unread"].(float64) != 0 {
		t.Errorf("mark all: %d %v", status, out)
	}
	if e := next(t, mine); e != `{"unread":0}` {
		t.Errorf("after marking all read: %s", e)
	}
	if again := a.events(editor); next(t, again) != `{"unread":0}` {
		t.Error("a new stream does not open with the count kept")
	}
	for name, body := range map[string]map[string]any{"neither": {}, "both": {"all": true, "ids": []string{id}}} {
		if status, _, _ := a.call(editor, http.MethodPost, "/notifications/read", body); status != 422 {
			t.Errorf("%s: %d", name, status)
		}
	}
}
