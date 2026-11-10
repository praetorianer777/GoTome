//go:build integration

package test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/kindle"
	"github.com/praetorianer777/gotome/backend/internal/kindle/smtptest"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

func (a *app) workKindle() {
	a.t.Helper()
	workers := jobs.NewWorkers()
	river.AddWorker(workers, &kindle.Worker{Service: a.server.Kindle})
	runner, err := jobs.New(a.pool, jobs.Config{Logger: slog.New(slog.DiscardHandler), Workers: workers})
	if err != nil {
		a.t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(func() { _ = runner.Stop(context.Background()) })
}

func (a *app) useMailServer(admin *http.Client, srv *smtptest.Server, extra map[string]any) {
	a.t.Helper()
	values := map[string]any{
		settings.MailHost: srv.Host, settings.MailPort: strconv.Itoa(srv.Port), settings.MailSender: "library@example.org",
		settings.MailUser: "mailer", settings.MailPassword: "hunter2-mail",
	}
	for k, v := range extra {
		values[k] = v
	}
	if status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": values}); status != 200 {
		a.t.Fatalf("settings: %d %v", status, body)
	}
	a.server.Kindle.TLS = srv.TLS
}

func (a *app) kindleSends(c *http.Client, book string) []map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/books/"+book+"/kindle", nil)
	if status != 200 {
		a.t.Fatalf("sends: %d %v", status, out)
	}
	var sends []map[string]any
	for _, s := range out["sends"].([]any) {
		sends = append(sends, s.(map[string]any))
	}
	return sends
}

func (a *app) waitKindle(c *http.Client, book, state string) map[string]any {
	a.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if sends := a.kindleSends(c, book); len(sends) > 0 && sends[0]["state"] == state {
			return sends[0]
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("the send did not reach %s: %v", state, a.kindleSends(c, book))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func notificationKinds(a *app, c *http.Client) []string {
	_, list, _ := a.call(c, http.MethodGet, "/notifications", nil)
	var kinds []string
	for _, n := range list["notifications"].([]any) {
		kinds = append(kinds, n.(map[string]any)["kind"].(string))
	}
	return kinds
}

func TestABookGoesToAKindleOnceAndOnlyWhileSeen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	shared := a.library(admin, "Shared", "shared")
	vault := a.library(admin, "Vault", "private")
	book := []byte("PK\x03\x04 an epub of sorts, long enough to need more than one line of base64 text to carry it over")
	_, up := a.upload(admin, shared, "Emma.epub", book)
	_, secret := a.upload(admin, vault, "Secret.epub", []byte("PK\x03\x04 a secret"))
	_, pdf := a.upload(admin, shared, "Pride.pdf", []byte("%PDF-1.4 a pdf of sorts"))
	id := up["bookId"].(string)

	send := func(c *http.Client, ids ...string) (int, map[string]any) {
		status, out, _ := a.call(c, http.MethodPost, "/kindle/sends", map[string]any{"books": ids})
		return status, out
	}
	if status, out := send(reader, id); status != 409 {
		t.Fatalf("before a mail server is set up: %d %v", status, out)
	}
	if _, st, _ := a.call(reader, http.MethodGet, "/me/kindle", nil); st["available"] != false || st["format"] != "epub" || st["maxBytes"] != float64(50<<20) {
		t.Errorf("status: %v", st)
	}

	srv := smtptest.New(t, smtptest.Options{User: "mailer", Password: "hunter2-mail"})
	a.useMailServer(admin, srv, nil)
	_, st, _ := a.call(reader, http.MethodGet, "/me/kindle", nil)
	if st["available"] != true || st["sender"] != "library@example.org" || st["address"] != "" {
		t.Fatalf("status: %v", st)
	}
	if _, body, _ := a.call(admin, http.MethodGet, "/settings", nil); strings.Contains(fmt.Sprint(body), "hunter2") {
		t.Error("the mail password is sent back")
	}
	if status, out := send(reader, id); status != 409 {
		t.Fatalf("without a Kindle address: %d %v", status, out)
	}
	for _, bad := range []string{"someone@example.org", "x@kindle.com.example.org", "a@kindle.com, b@example.org"} {
		if status, _, _ := a.call(reader, http.MethodPut, "/me/kindle", map[string]any{"address": bad}); status != 422 {
			t.Errorf("address %q: %d", bad, status)
		}
	}
	if status, st, _ := a.call(reader, http.MethodPut, "/me/kindle", map[string]any{"address": " Reader_9@Kindle.com "}); status != 200 || st["address"] != "reader_9@kindle.com" {
		t.Fatalf("set address: %d %v", status, st)
	}
	if _, st, _ := a.call(stranger, http.MethodGet, "/me/kindle", nil); st["address"] != "" {
		t.Errorf("someone else's address: %v", st)
	}
	_ = readerID

	status, out := send(reader, id, id, secret["bookId"].(string), pdf["bookId"].(string), uuid.NewString())
	if status != 200 || out["queued"] != 1.0 || len(out["skipped"].([]any)) != 3 {
		t.Fatalf("send: %d %v", status, out)
	}
	reasons := map[string]string{}
	for _, s := range out["skipped"].([]any) {
		m := s.(map[string]any)
		reasons[m["bookId"].(string)] = m["reason"].(string)
	}
	if reasons[secret["bookId"].(string)] != "unavailable" || reasons[pdf["bookId"].(string)] != "unavailable" {
		t.Errorf("reasons: %v", reasons)
	}
	if _, out := send(reader, id); out["queued"] != 0.0 || out["skipped"].([]any)[0].(map[string]any)["reason"] != "pending" {
		t.Errorf("sending it again while it waits: %v", out)
	}
	if got := a.kindleSends(stranger, id); len(got) != 0 {
		t.Errorf("someone else's sends: %v", got)
	}
	if got := a.kindleSends(reader, id); len(got) != 1 || got[0]["state"] != "queued" {
		t.Fatalf("queued: %v", got)
	}
	if len(srv.Mails()) != 0 {
		t.Fatal("sent before the job ran")
	}

	a.workKindle()
	a.waitKindle(reader, id, "sent")
	mails := srv.Mails()
	if len(mails) != 1 {
		t.Fatalf("%d mails", len(mails))
	}
	m := mails[0]
	if !m.Encrypted || m.AuthUser != "mailer" || m.From != "library@example.org" || len(m.To) != 1 || m.To[0] != "reader_9@kindle.com" {
		t.Errorf("envelope: %+v", m)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(m.Data))
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	mr := multipart.NewReader(parsed.Body, params["boundary"])
	var got []byte
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FileName() == "Emma.epub" {
			raw, _ := io.ReadAll(p)
			got, _ = base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(string(raw)))
		}
	}
	if string(got) != string(book) {
		t.Errorf("attachment: %q", got)
	}
	if kinds := notificationKinds(a, reader); len(kinds) != 1 || kinds[0] != "kindle.sent" {
		t.Errorf("notifications: %v", kinds)
	}
	if strings.Contains(a.logs.String(), "hunter2") {
		t.Error("the mail password is in the log")
	}

	// A job that runs again sends nothing: this is what keeps a retry or a
	// restart from sending a book twice.
	var sendID uuid.UUID
	if err := a.pool.QueryRow(context.Background(), `SELECT id FROM kindle_sends`).Scan(&sendID); err != nil {
		t.Fatal(err)
	}
	for _, last := range []bool{false, true} {
		if err := a.server.Kindle.Run(context.Background(), sendID, last); err != nil {
			t.Fatal(err)
		}
	}
	if len(srv.Mails()) != 1 {
		t.Errorf("sent again: %d mails", len(srv.Mails()))
	}

	// Sent, it may be sent again; a book too large is refused up front.
	if _, out := send(reader, id); out["queued"] != 1.0 {
		t.Errorf("sending it again once it is sent: %v", out)
	}
	a.waitKindle(reader, id, "sent")
	if _, err := a.pool.Exec(context.Background(), `UPDATE book_files SET size_bytes = $1 WHERE id = $2`, kindle.MaxAttachment+1, up["fileId"]); err != nil {
		t.Fatal(err)
	}
	if _, out := send(reader, id); out["queued"] != 0.0 || out["skipped"].([]any)[0].(map[string]any)["reason"] != "tooLarge" {
		t.Errorf("a book over the limit: %v", out)
	}
}

func TestAFailedSendSaysWhyAndATakenOneIsNotSentAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := a.library(admin, "Shared", "shared")
	_, up := a.upload(admin, shared, "Emma.epub", []byte("PK\x03\x04 an epub of sorts"))
	id := up["bookId"].(string)
	srv := smtptest.New(t, smtptest.Options{User: "mailer", Password: "hunter2-mail", RejectRcpt: "550 5.7.1 sender not approved by the recipient"})
	a.useMailServer(admin, srv, nil)
	a.call(reader, http.MethodPut, "/me/kindle", map[string]any{"address": "reader@kindle.com"})
	a.call(reader, http.MethodPost, "/kindle/sends", map[string]any{"books": []string{id}})
	a.workKindle()
	failed := a.waitKindle(reader, id, "failed")
	if !strings.Contains(failed["message"].(string), "sender not approved") {
		t.Errorf("message: %v", failed["message"])
	}
	if kinds := notificationKinds(a, reader); len(kinds) != 1 || kinds[0] != "kindle.failed" {
		t.Errorf("notifications: %v", kinds)
	}

	// A send found "sending" was taken by a run that may have handed the
	// mail over before it stopped.
	good := smtptest.New(t, smtptest.Options{User: "mailer", Password: "hunter2-mail"})
	a.useMailServer(admin, good, nil)
	if _, out, _ := a.call(reader, http.MethodPost, "/kindle/sends", map[string]any{"books": []string{id}}); out["queued"] != 1.0 {
		t.Fatalf("queue: %v", out)
	}
	a.waitKindle(reader, id, "sent")
	var sendID uuid.UUID
	if err := a.pool.QueryRow(context.Background(), `SELECT id FROM kindle_sends WHERE state = 'sent'`).Scan(&sendID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(context.Background(), `UPDATE kindle_sends SET state = 'sending', finished_at = NULL WHERE id = $1`, sendID); err != nil {
		t.Fatal(err)
	}
	before := len(good.Mails())
	if err := a.server.Kindle.Run(context.Background(), sendID, false); err != nil {
		t.Fatal(err)
	}
	if sends := a.kindleSends(reader, id); sends[0]["state"] != "failed" || len(good.Mails()) != before {
		t.Errorf("after a crash: %v, %d mails of %d", sends[0], len(good.Mails()), before)
	}
}

func TestATemporaryRefusalIsTriedAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := a.library(admin, "Shared", "shared")
	_, up := a.upload(admin, shared, "Emma.epub", []byte("PK\x03\x04 an epub of sorts"))
	id := up["bookId"].(string)
	srv := smtptest.New(t, smtptest.Options{User: "mailer", Password: "hunter2-mail", RejectData: "452 4.3.1 try later"})
	a.useMailServer(admin, srv, nil)
	a.call(reader, http.MethodPut, "/me/kindle", map[string]any{"address": "reader@kindle.com"})
	a.call(reader, http.MethodPost, "/kindle/sends", map[string]any{"books": []string{id}})
	var sendID uuid.UUID
	if err := a.pool.QueryRow(context.Background(), `SELECT id FROM kindle_sends`).Scan(&sendID); err != nil {
		t.Fatal(err)
	}
	if err := a.server.Kindle.Run(context.Background(), sendID, false); err == nil {
		t.Fatal("a refusal to wait was not an error to retry")
	}
	if sends := a.kindleSends(reader, id); sends[0]["state"] != "queued" {
		t.Fatalf("after the first try: %v", sends[0])
	}
	if err := a.server.Kindle.Run(context.Background(), sendID, true); err != nil {
		t.Fatal(err)
	}
	if sends := a.kindleSends(reader, id); sends[0]["state"] != "failed" || srv.Attempts() != 2 || len(srv.Mails()) != 0 {
		t.Errorf("after the last try: %v, %d attempts", sends[0], srv.Attempts())
	}
}
