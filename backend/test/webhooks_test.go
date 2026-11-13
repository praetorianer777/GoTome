//go:build integration

package test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/webhooks"
)

const hookSecret = "a-shared-secret-of-some-length"

type received struct {
	header http.Header
	body   string
}

// receiver answers the statuses in order, then 200, and keeps what it got.
func receiver(t *testing.T, statuses ...int) (*httptest.Server, func() []received) {
	t.Helper()
	var mu sync.Mutex
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		n := len(got)
		got = append(got, received{r.Header.Clone(), string(body)})
		if n < len(statuses) {
			w.WriteHeader(statuses[n])
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []received {
		mu.Lock()
		defer mu.Unlock()
		return append([]received(nil), got...)
	}
}

func (a *app) makeHook(admin *http.Client, url string, trusted bool, kinds ...string) string {
	a.t.Helper()
	if kinds == nil {
		kinds = []string{}
	}
	status, out, _ := a.call(admin, http.MethodPost, "/webhooks", map[string]any{
		"name": "ops", "url": url, "kinds": kinds, "secret": hookSecret, "trusted": trusted, "enabled": true,
	})
	if status != 201 {
		a.t.Fatalf("make webhook: %d %v", status, out)
	}
	return out["id"].(string)
}

func (a *app) deliveriesOf(admin *http.Client, hook string) []map[string]any {
	a.t.Helper()
	status, out, _ := a.call(admin, http.MethodGet, "/webhooks/"+hook+"/deliveries", nil)
	if status != 200 {
		a.t.Fatalf("deliveries: %d %v", status, out)
	}
	var list []map[string]any
	for _, d := range out["deliveries"].([]any) {
		list = append(list, d.(map[string]any))
	}
	return list
}

func (a *app) tellHooks(ev notify.Event) {
	a.t.Helper()
	events := &notify.Events{Pool: a.pool, Queue: a.server.Duplicates.Queue, Hooks: a.hooks}
	err := db.InTx(context.Background(), a.pool, func(tx pgx.Tx) error {
		return events.QueueTx(context.Background(), tx, ev)
	})
	if err != nil {
		a.t.Fatal(err)
	}
}

func TestAnEventIsSentSignedOnceToTheHooksThatWantIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	srv, got := receiver(t)
	wanted := a.makeHook(admin, srv.URL, true, "books.added")
	all := a.makeHook(admin, srv.URL, true)
	other := a.makeHook(admin, srv.URL, true, "scan.failed")

	a.tellHooks(notify.Event{Kind: notify.KindBooksAdded, Link: "/?library=x", Data: map[string]any{
		"count": 4, "title": "The Moonlit Harbour", "authors": []string{"A. Writer"},
	}})

	for hook, want := range map[string]int{wanted: 1, all: 1, other: 0} {
		if n := len(a.deliveriesOf(admin, hook)); n != want {
			t.Errorf("hook %s has %d deliveries, want %d", hook, n, want)
		}
	}
	d := a.deliveriesOf(admin, wanted)[0]
	if err := a.hooks.Deliver(context.Background(), uuid.MustParse(d["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	ts := r.header.Get(webhooks.HeaderTimestamp)
	if want := webhooks.Sign([]byte(hookSecret), ts, []byte(r.body)); r.header.Get(webhooks.HeaderSignature) != want {
		t.Errorf("signature %q, want %q", r.header.Get(webhooks.HeaderSignature), want)
	}
	if r.header.Get(webhooks.HeaderEvent) != "books.added" || r.header.Get(webhooks.HeaderDelivery) != d["id"] {
		t.Errorf("headers: %v", r.header)
	}
	for _, leak := range []string{"Moonlit", "Writer", hookSecret} {
		if strings.Contains(r.body, leak) {
			t.Errorf("the body holds %q: %s", leak, r.body)
		}
	}
	if !strings.Contains(r.body, `"count": 4`) || !strings.Contains(r.body, "https://books.example.org/?library=x") {
		t.Errorf("body = %s", r.body)
	}
	if after := a.deliveriesOf(admin, wanted)[0]; after["status"] != "delivered" || after["responseCode"] != float64(200) {
		t.Errorf("delivery after: %v", after)
	}
}

func TestADeliveryIsRetriedUntilTheReceiverAnswersThenGivenUp(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	srv, got := receiver(t, 500, 503)
	hook := a.makeHook(admin, srv.URL, true)
	a.tellHooks(notify.Event{Kind: notify.KindScanFailed})
	id := uuid.MustParse(a.deliveriesOf(admin, hook)[0]["id"].(string))
	ctx := context.Background()

	for i, state := range []string{"pending", "pending"} {
		if err := a.hooks.Deliver(ctx, id, false); err == nil {
			t.Fatalf("attempt %d: the receiver refused and no error came back", i+1)
		}
		d := a.deliveriesOf(admin, hook)[0]
		if d["status"] != state || d["attempts"] != float64(i+1) || !strings.Contains(d["lastError"].(string), "answered") {
			t.Errorf("after attempt %d: %v", i+1, d)
		}
	}
	if err := a.hooks.Deliver(ctx, id, false); err != nil {
		t.Fatalf("third attempt: %v", err)
	}
	if d := a.deliveriesOf(admin, hook)[0]; d["status"] != "delivered" || d["attempts"] != float64(3) {
		t.Errorf("after the third: %v", d)
	}
	reqs := got()
	if len(reqs) != 3 || reqs[0].body != reqs[2].body || reqs[0].header.Get(webhooks.HeaderDelivery) != reqs[2].header.Get(webhooks.HeaderDelivery) {
		t.Errorf("a retry must carry the same delivery and body: %d requests", len(reqs))
	}

	down, _ := receiver(t, 500)
	failing := a.makeHook(admin, down.URL, true)
	a.tellHooks(notify.Event{Kind: notify.KindScanFailed})
	last := uuid.MustParse(a.deliveriesOf(admin, failing)[0]["id"].(string))
	if err := a.hooks.Deliver(ctx, last, true); err == nil {
		t.Fatal("no error from the last attempt")
	}
	if d := a.deliveriesOf(admin, failing)[0]; d["status"] != "failed" {
		t.Errorf("after the last attempt: %v", d)
	}
}

func TestAnAddressInsideIsRefusedUnlessTheHookIsTrusted(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	srv, got := receiver(t)
	hook := a.makeHook(admin, srv.URL, false)
	a.tellHooks(notify.Event{Kind: notify.KindScanFailed})
	id := uuid.MustParse(a.deliveriesOf(admin, hook)[0]["id"].(string))

	if err := a.hooks.Deliver(context.Background(), id, false); err == nil {
		t.Fatal("a loopback address was reached without being trusted")
	}
	if len(got()) != 0 {
		t.Error("the receiver was reached")
	}
	if d := a.deliveriesOf(admin, hook)[0]; !strings.Contains(d["lastError"].(string), "public internet") || strings.Contains(d["lastError"].(string), srv.URL) {
		t.Errorf("the refusal: %v", d)
	}

	status, out, _ := a.call(admin, http.MethodPut, "/webhooks/"+hook, map[string]any{
		"name": "ops", "url": srv.URL, "kinds": []string{}, "trusted": true, "enabled": true,
	})
	if status != 200 || out["trusted"] != true {
		t.Fatalf("trust: %d %v", status, out)
	}
	if err := a.hooks.Deliver(context.Background(), id, false); err != nil {
		t.Fatalf("a trusted address: %v", err)
	}
	if len(got()) != 1 {
		t.Error("the trusted receiver was not reached")
	}
}

func TestAWebhookSecretIsNeverSentBackAndOnlyAdministratorsHandleHooks(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	hook := a.makeHook(admin, "https://example.org/hook", false, "books.added")

	for _, path := range []string{"/webhooks", "/webhooks/" + hook + "/deliveries"} {
		status, _, _ := a.call(editor, http.MethodGet, path, nil)
		if status != 403 {
			t.Errorf("an editor reads %s: %d", path, status)
		}
	}
	if status, _, _ := a.call(editor, http.MethodPost, "/webhooks/"+hook+"/test", nil); status != 403 {
		t.Errorf("an editor sends a test: %d", status)
	}
	status, out, _ := a.call(admin, http.MethodGet, "/webhooks", nil)
	if status != 200 {
		t.Fatalf("list: %d %v", status, out)
	}
	h := out["webhooks"].([]any)[0].(map[string]any)
	if _, has := h["secret"]; has || h["secretSet"] != true {
		t.Errorf("the hook says %v", h)
	}
	var sealed []byte
	if err := a.pool.QueryRow(context.Background(), `SELECT secret_sealed FROM webhooks`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), hookSecret) {
		t.Error("the secret is stored as it is")
	}

	short := map[string]any{"name": "x", "url": "https://example.org/", "kinds": []string{}, "secret": "short"}
	if status, _, _ := a.call(admin, http.MethodPost, "/webhooks", short); status != 422 {
		t.Errorf("a short secret: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/webhooks/"+hook+"/test", nil); status != 202 {
		t.Errorf("send test: %d", status)
	}
	if n := len(a.deliveriesOf(admin, hook)); n != 1 {
		t.Errorf("the test left %d deliveries", n)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/webhooks/"+hook, nil); status != 204 {
		t.Errorf("delete: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodGet, "/webhooks/"+hook+"/deliveries", nil); status != 404 {
		t.Errorf("deliveries of a deleted hook: %d", status)
	}
}

func TestTheDeliverWorkerRetriesUntilTheLastAttemptThenGivesUp(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	srv, got := receiver(t, 500, 500)
	hook := a.makeHook(admin, srv.URL, true)
	a.tellHooks(notify.Event{Kind: notify.KindScanFailed})
	id := uuid.MustParse(a.deliveriesOf(admin, hook)[0]["id"].(string))
	worker := &webhooks.DeliverWorker{Service: a.hooks}
	work := func(attempt int) error {
		return worker.Work(context.Background(), &river.Job[webhooks.DeliverArgs]{
			JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: webhooks.MaxAttempts},
			Args:   webhooks.DeliverArgs{DeliveryID: id},
		})
	}

	if err := work(1); err == nil {
		t.Fatal("a refused delivery returned no error for the queue to retry")
	}
	if d := a.deliveriesOf(admin, hook)[0]; d["status"] != "pending" {
		t.Errorf("after a failure with attempts left: %v", d)
	}
	if err := work(webhooks.MaxAttempts); err == nil {
		t.Fatal("the last attempt returned no error")
	}
	if d := a.deliveriesOf(admin, hook)[0]; d["status"] != "failed" || d["attempts"] != float64(2) {
		t.Errorf("after the last attempt: %v", d)
	}
	if err := work(3); err != nil {
		t.Fatalf("an answered delivery: %v", err)
	}
	if d := a.deliveriesOf(admin, hook)[0]; d["status"] != "delivered" {
		t.Errorf("after a 200: %v", d)
	}
	if n := len(got()); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
}

func TestTrimRemovesDeliveriesPastTheirDaysAndPastTheKeptCount(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	srv, _ := receiver(t)
	hook := uuid.MustParse(a.makeHook(admin, srv.URL, true))
	ctx := context.Background()
	add := func(status string, age string, n int) {
		_, err := a.pool.Exec(ctx, `INSERT INTO webhook_deliveries (hook_id, kind, payload, status, created_at)
			SELECT $1, 'scan.failed', '{}', $2, now() - $3::interval - (g || ' seconds')::interval FROM generate_series(1, $4) g`,
			hook, status, age, n)
		if err != nil {
			t.Fatal(err)
		}
	}
	add("delivered", "40 days", 3)
	add("delivered", "1 day", webhooks.KeepPerHook+5)
	add("pending", "2 days", 2)
	count := func(where string) (n int) {
		if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE hook_id = $1 AND `+where, hook).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := a.hooks.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count("created_at < now() - interval '30 days'"); n != 0 {
		t.Errorf("%d deliveries older than the keep days remain", n)
	}
	if n := count("status = 'delivered'"); n != webhooks.KeepPerHook {
		t.Errorf("%d finished deliveries remain, want %d", n, webhooks.KeepPerHook)
	}
	if n := count("status = 'pending'"); n != 2 {
		t.Errorf("%d pending deliveries remain, want both", n)
	}
}
