// Package webhooks tells other services of what happens in GOtome (#418).
// An administrator registers an address per kind of event
// (notify.EventKinds); each event is sent once to every hook that wants it,
// as a small JSON of IDs and counts, signed with the hook's secret, by a
// River job that is retried with a growing wait. The request goes through
// outbound.NewClient, so only public addresses are reached unless the
// administrator marked the hook trusted.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/secret"
)

// KindTest is the event "send test" makes; no real event has this kind.
const KindTest = "webhook.test"

// Limits.
const (
	MaxHooks       = 20
	MaxNameLen     = 80
	MaxURLLen      = 2000
	MinSecretLen   = 16
	MaxSecretLen   = 200
	MaxAttempts    = 8
	KeepPerHook    = 200
	KeepDays       = 30
	requestTimeout = 15 * time.Second
	maxErrorLen    = 300
)

// Header names of a delivery.
const (
	HeaderEvent     = "X-GOtome-Event"
	HeaderDelivery  = "X-GOtome-Delivery"
	HeaderTimestamp = "X-GOtome-Timestamp"
	HeaderSignature = "X-GOtome-Signature"
)

// Errors a caller can tell apart.
var (
	ErrNotFound = errors.New("no such webhook")
	ErrTooMany  = errors.New("too many webhooks")
	ErrBadInput = errors.New("bad webhook")
)

// FieldError names the field of a webhook that is wrong.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }
func (e *FieldError) Unwrap() error { return ErrBadInput }

// Hook is a registered address. The secret is never part of it.
type Hook struct {
	ID        uuid.UUID
	Name      string
	URL       string
	Kinds     []string
	Trusted   bool
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input is what an administrator sets. A nil Secret on an update keeps the
// one there is.
type Input struct {
	Name    string
	URL     string
	Kinds   []string
	Secret  *string
	Trusted bool
	Enabled bool
}

// Delivery is one send of one event to one hook.
type Delivery struct {
	ID           uuid.UUID
	Kind         string
	Payload      json.RawMessage
	Status       string
	Attempts     int
	LastError    string
	ResponseCode *int
	CreatedAt    time.Time
	DeliveredAt  *time.Time
}

// Queue is the job queue deliveries are enqueued with.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Service keeps the hooks and sends the events.
type Service struct {
	Pool  *pgxpool.Pool
	Box   *secret.Box
	Queue Queue
	Log   *slog.Logger
	// PublicURL is the address links in a payload are made absolute with;
	// empty leaves them relative.
	PublicURL string
	now       func() time.Time
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func hookOf(row sqlc.Webhook) Hook {
	return Hook{ID: row.ID, Name: row.Name, URL: row.Url, Kinds: row.Kinds, Trusted: row.Trusted,
		Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

// KnownKind reports whether hooks may ask for the kind.
func KnownKind(kind string) bool {
	return slices.ContainsFunc(notify.EventKinds, func(k notify.EventKind) bool { return k.Kind == kind })
}

func clean(in Input, creating bool) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	if in.Name == "" || len([]rune(in.Name)) > MaxNameLen {
		return in, &FieldError{"name", fmt.Sprintf("Give the webhook a name of up to %d characters.", MaxNameLen)}
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(in.URL) > MaxURLLen {
		return in, &FieldError{"url", "Give an http or https address without a user name."}
	}
	seen := map[string]bool{}
	kinds := []string{}
	for _, k := range in.Kinds {
		if !KnownKind(k) {
			return in, &FieldError{"kinds", fmt.Sprintf("%q is not a kind of event.", k)}
		}
		if !seen[k] {
			seen[k] = true
			kinds = append(kinds, k)
		}
	}
	in.Kinds = kinds
	if in.Secret == nil && creating {
		return in, &FieldError{"secret", fmt.Sprintf("Give a secret of at least %d characters.", MinSecretLen)}
	}
	if in.Secret != nil && (len(*in.Secret) < MinSecretLen || len(*in.Secret) > MaxSecretLen) {
		return in, &FieldError{"secret", fmt.Sprintf("Give a secret of %d to %d characters.", MinSecretLen, MaxSecretLen)}
	}
	return in, nil
}

func (s *Service) seal(id uuid.UUID, value string) ([]byte, error) {
	return s.Box.Seal("webhook:"+id.String(), []byte(value))
}

// List returns the hooks, oldest first.
func (s *Service) List(ctx context.Context) ([]Hook, error) {
	rows, err := sqlc.New(s.Pool).ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Hook, len(rows))
	for i, r := range rows {
		out[i] = hookOf(r)
	}
	return out, nil
}

// Get returns one hook.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Hook, error) {
	row, err := sqlc.New(s.Pool).GetWebhook(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrNotFound
	}
	return hookOf(row), err
}

// Create registers a hook.
func (s *Service) Create(ctx context.Context, by uuid.UUID, in Input) (Hook, error) {
	in, err := clean(in, true)
	if err != nil {
		return Hook{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Hook{}, err
	}
	sealed, err := s.seal(id, *in.Secret)
	if err != nil {
		return Hook{}, err
	}
	var out Hook
	err = db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('webhooks'))"); err != nil {
			return err
		}
		n, err := q.CountWebhooks(ctx)
		if err != nil {
			return err
		}
		if n >= MaxHooks {
			return ErrTooMany
		}
		row, err := q.CreateWebhook(ctx, sqlc.CreateWebhookParams{
			ID: id, Name: in.Name, Url: in.URL, Kinds: in.Kinds, SecretSealed: sealed,
			Trusted: in.Trusted, Enabled: in.Enabled, CreatedBy: &by,
		})
		out = hookOf(row)
		return err
	})
	return out, err
}

// Update changes a hook; a nil secret keeps the old one.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (Hook, error) {
	in, err := clean(in, false)
	if err != nil {
		return Hook{}, err
	}
	var sealed []byte
	if in.Secret != nil {
		if sealed, err = s.seal(id, *in.Secret); err != nil {
			return Hook{}, err
		}
	}
	row, err := sqlc.New(s.Pool).UpdateWebhook(ctx, sqlc.UpdateWebhookParams{
		ID: id, Name: in.Name, Url: in.URL, Kinds: in.Kinds, Trusted: in.Trusted, Enabled: in.Enabled, SecretSealed: sealed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrNotFound
	}
	return hookOf(row), err
}

// Delete removes a hook and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := sqlc.New(s.Pool).DeleteWebhook(ctx, id)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// Deliveries returns the newest deliveries of a hook.
func (s *Service) Deliveries(ctx context.Context, id uuid.UUID, limit int) ([]Delivery, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := sqlc.New(s.Pool).ListWebhookDeliveries(ctx, sqlc.ListWebhookDeliveriesParams{HookID: id, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]Delivery, len(rows))
	for i, r := range rows {
		out[i] = deliveryOf(r)
	}
	return out, nil
}

// LastDelivery is how the latest delivery of a hook went.
type LastDelivery struct {
	Status       string
	At           time.Time
	ResponseCode *int
}

func code(c *int32) *int {
	if c == nil {
		return nil
	}
	n := int(*c)
	return &n
}

// Last returns the latest delivery of each hook, by hook.
func (s *Service) Last(ctx context.Context) (map[uuid.UUID]LastDelivery, error) {
	rows, err := sqlc.New(s.Pool).WebhookLastDeliveries(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]LastDelivery{}
	for _, r := range rows {
		out[r.HookID] = LastDelivery{Status: r.Status, At: r.CreatedAt, ResponseCode: code(r.ResponseCode)}
	}
	return out, nil
}

func deliveryOf(r sqlc.WebhookDelivery) Delivery {
	d := Delivery{ID: r.ID, Kind: r.Kind, Payload: r.Payload, Status: r.Status, Attempts: int(r.Attempts),
		CreatedAt: r.CreatedAt, DeliveredAt: r.DeliveredAt}
	if r.LastError != nil {
		d.LastError = *r.LastError
	}
	d.ResponseCode = code(r.ResponseCode)
	return d
}

// Payload is the body of a delivery: IDs, counts and a link, never a title,
// a name or any text of a book.
type Payload struct {
	Kind      string         `json:"kind"`
	Time      time.Time      `json:"time"`
	Libraries []uuid.UUID    `json:"libraryIds"`
	BookID    *uuid.UUID     `json:"bookId,omitempty"`
	Link      string         `json:"link,omitempty"`
	Data      map[string]any `json:"data"`
}

// dataKeys are the keys of an event's data that go out. Everything else an
// event holds (titles, authors, series, feed names, error texts) stays in
// GOtome.
var dataKeys = []string{"library", "file", "count", "component", "version", "current", "security", "date", "precision"}

func (s *Service) payloadOf(ev notify.Event) Payload {
	p := Payload{Kind: ev.Kind, Time: s.clock().UTC().Truncate(time.Second), Libraries: ev.Libraries,
		BookID: ev.BookID, Data: map[string]any{}}
	if p.Libraries == nil {
		p.Libraries = []uuid.UUID{}
	}
	for _, k := range dataKeys {
		switch v := ev.Data[k].(type) {
		case nil:
		case string:
			if len(v) <= 64 {
				p.Data[k] = v
			}
		case bool, int, int64, float64:
			p.Data[k] = v
		case uuid.UUID:
			p.Data[k] = v.String()
		}
	}
	p.Link = s.absolute(ev.Link)
	return p
}

func (s *Service) absolute(link string) string {
	if link == "" || s.PublicURL == "" || !strings.HasPrefix(link, "/") {
		return link
	}
	return strings.TrimRight(s.PublicURL, "/") + link
}

// SendTx queues the event for every enabled hook that wants its kind, in
// the transaction of what happened: once per event, however many people
// hear of it. It satisfies notify.Hooks.
func (s *Service) SendTx(ctx context.Context, tx pgx.Tx, ev notify.Event) error {
	if s == nil {
		return nil
	}
	q := sqlc.New(tx)
	ids, err := q.ListWebhooksForKind(ctx, ev.Kind)
	if err != nil || len(ids) == 0 {
		return err
	}
	body, err := json.Marshal(s.payloadOf(ev))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.enqueueTx(ctx, tx, id, ev.Kind, body, time.Time{}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) enqueueTx(ctx context.Context, tx pgx.Tx, hook uuid.UUID, kind string, body []byte, at time.Time) error {
	d, err := sqlc.New(tx).CreateWebhookDelivery(ctx, sqlc.CreateWebhookDeliveryParams{HookID: hook, Kind: kind, Payload: body})
	if err != nil {
		return err
	}
	_, err = s.Queue.InsertTx(ctx, tx, DeliverArgs{DeliveryID: d.ID}, jobs.InsertOpts{
		Queue: jobs.QueueNotify, MaxAttempts: MaxAttempts, ScheduledAt: at,
	})
	return err
}

// SendTest queues a test event to one hook, enabled or not.
func (s *Service) SendTest(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	body, err := json.Marshal(s.payloadOf(notify.Event{Kind: KindTest, Link: "/admin/webhooks"}))
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		return s.enqueueTx(ctx, tx, id, KindTest, body, time.Time{})
	})
}

// Sign is the signature of a delivery: the hex HMAC-SHA256, under the
// hook's secret, of the timestamp, a full stop and the body. The timestamp
// is in the signed string so that a captured request cannot be sent again
// later: a receiver refuses one that is too old.
func Sign(secret []byte, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(timestamp))
	m.Write([]byte("."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func (s *Service) httpClient(h sqlc.Webhook) *http.Client {
	opts := outbound.Options{Timeout: requestTimeout}
	if h.Trusted {
		opts.Trusted = []string{h.Url}
	}
	c := outbound.NewClient(opts)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// errGone is a delivery whose hook no longer wants it.
var errGone = errors.New("the webhook is gone or off")

// Deliver makes one attempt. A nil error is a 2xx answer; any other is
// recorded on the delivery and returned for the queue to retry, and final
// says it was the last attempt.
func (s *Service) Deliver(ctx context.Context, id uuid.UUID, final bool) error {
	q := sqlc.New(s.Pool)
	d, err := q.GetWebhookDelivery(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	h, err := q.GetWebhook(ctx, d.HookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	code, sendErr := s.post(ctx, h, d)
	status, msg := "delivered", (*string)(nil)
	var codeArg *int32
	if code > 0 {
		c := int32(code)
		codeArg = &c
	}
	switch {
	case sendErr == nil:
	case errors.Is(sendErr, errGone):
		status, msg = "failed", ptr(sendErr.Error())
		final = true
	default:
		status = "pending"
		if final {
			status = "failed"
		}
		msg = ptr(truncate(sendErr.Error()))
	}
	if err := q.RecordWebhookAttempt(ctx, sqlc.RecordWebhookAttemptParams{
		ID: id, Status: status, LastError: msg, ResponseCode: codeArg,
	}); err != nil {
		return err
	}
	if errors.Is(sendErr, errGone) {
		return nil
	}
	return sendErr
}

func ptr[T any](v T) *T { return &v }

func truncate(s string) string {
	if len(s) > maxErrorLen {
		return s[:maxErrorLen]
	}
	return s
}

func (s *Service) post(ctx context.Context, h sqlc.Webhook, d sqlc.WebhookDelivery) (int, error) {
	if !h.Enabled && d.Kind != KindTest {
		return 0, errGone
	}
	key, err := s.Box.Open("webhook:"+h.ID.String(), h.SecretSealed)
	if err != nil {
		return 0, errors.New("the secret cannot be opened")
	}
	ts := strconv.FormatInt(s.clock().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Url, bytes.NewReader(d.Payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GOtome-Webhook")
	req.Header.Set(HeaderEvent, d.Kind)
	req.Header.Set(HeaderDelivery, d.ID.String())
	req.Header.Set(HeaderTimestamp, ts)
	req.Header.Set(HeaderSignature, Sign(key, ts, d.Payload))
	resp, err := s.httpClient(h).Do(req)
	if err != nil {
		return 0, scrub(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// scrub keeps the address out of an error: it may hold a token.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Trim removes old deliveries.
func (s *Service) Trim(ctx context.Context) error {
	_, err := sqlc.New(s.Pool).TrimWebhookDeliveries(ctx, sqlc.TrimWebhookDeliveriesParams{
		Before: s.clock().AddDate(0, 0, -KeepDays), Keep: KeepPerHook,
	})
	return err
}

const (
	deliverKind = "webhooks.deliver"
	trimKind    = "webhooks.trim"
)

// DeliverArgs is the job that makes one delivery.
type DeliverArgs struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

func (DeliverArgs) Kind() string { return deliverKind }

// DeliverWorker runs DeliverArgs.
type DeliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	Service *Service
}

func (w *DeliverWorker) Work(ctx context.Context, job *river.Job[DeliverArgs]) error {
	return w.Service.Deliver(ctx, job.Args.DeliveryID, job.Attempt >= job.MaxAttempts)
}

// NextRetry waits 30 seconds after the first failure and four times as long
// after each next: 30 s, 2 min, 8 min, 32 min, 2 h, 9 h, 34 h.
func (w *DeliverWorker) NextRetry(job *river.Job[DeliverArgs]) time.Time {
	return time.Now().Add(Backoff(job.Attempt))
}

// Backoff is the wait after the attempt-th failed attempt.
func Backoff(attempt int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 4
	}
	return d
}

// TrimArgs is the job that trims the log.
type TrimArgs struct{}

func (TrimArgs) Kind() string { return trimKind }

// TrimWorker runs TrimArgs.
type TrimWorker struct {
	river.WorkerDefaults[TrimArgs]
	Service *Service
}

func (w *TrimWorker) Work(ctx context.Context, _ *river.Job[TrimArgs]) error {
	return w.Service.Trim(ctx)
}

// TrimPeriodic is the trim, every interval.
func TrimPeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, false, TrimArgs{}, jobs.QueueDefault)
}
