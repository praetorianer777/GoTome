// Package notify tells people what happened in the background. A
// notification is a row of its user's, written in the transaction of the
// change it is about, and announced through Postgres's NOTIFY, which is sent
// only when that transaction commits; the Hub hears it and wakes the
// streams open for that user.
package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// channel is the NOTIFY channel; its payload is the user's ID.
const channel = "gotome_notifications"

// Kinds of notification, as stored. The web app words each.
const (
	// KindBulkFinished is a bulk change that has gone through all its
	// books. Its data are the action and the count of each outcome.
	KindBulkFinished = "bulk.finished"
)

// MaxPage is the most notifications one page holds.
const MaxPage = 100

// New is a notification to make.
type New struct {
	Kind string
	// Data are the values the text is made from; it is stored as JSON.
	Data any
	// Link is the app's path to go to for more, or "".
	Link string
	// BookID is the book it is about; it is seen only while the book is.
	BookID *uuid.UUID
	// LibraryID is the library it is about; it is seen only while the
	// library is.
	LibraryID *uuid.UUID
}

// Notification is one notification as its user sees it.
type Notification struct {
	ID        uuid.UUID
	Kind      string
	Data      json.RawMessage
	Link      string
	BookID    *uuid.UUID
	CreatedAt time.Time
	ReadAt    *time.Time
}

// CreateTx makes a notification for the user in the transaction, which
// announces it when it commits.
func CreateTx(ctx context.Context, tx pgx.Tx, user uuid.UUID, n New) (uuid.UUID, error) {
	data, err := json.Marshal(n.Data)
	if err != nil {
		return uuid.Nil, err
	}
	if n.Data == nil {
		data = []byte("{}")
	}
	id, err := sqlc.New(tx).CreateNotification(ctx, sqlc.CreateNotificationParams{
		UserID: user, Kind: n.Kind, Data: data, Link: n.Link, BookID: n.BookID, LibraryID: n.LibraryID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, announce(ctx, tx, user)
}

func announce(ctx context.Context, tx pgx.Tx, user uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", channel, user.String())
	return err
}

// Service reads and marks a person's notifications.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Cursor is where a page ended: the last notification's time and ID.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// List returns the scope's user's notifications, newest first, after the
// cursor when one is given, and the cursor of the next page if there is one.
func (s *Service) List(ctx context.Context, scope library.Scope, after *Cursor, limit int) ([]Notification, *Cursor, error) {
	limit = min(max(limit, 1), MaxPage)
	p := sqlc.ListNotificationsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxRows: int32(limit + 1)}
	if after != nil {
		p.BeforeAt, p.BeforeID = &after.At, &after.ID
	}
	rows, err := sqlc.New(s.pool).ListNotifications(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Notification, 0, len(rows))
	for _, r := range rows {
		out = append(out, Notification{
			ID: r.ID, Kind: r.Kind, Data: r.Data, Link: r.Link, BookID: r.BookID, CreatedAt: r.CreatedAt, ReadAt: r.ReadAt,
		})
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	out = out[:limit]
	last := out[limit-1]
	return out, &Cursor{At: last.CreatedAt, ID: last.ID}, nil
}

// Unread returns how many of the scope's user's notifications are unread.
func (s *Service) Unread(ctx context.Context, scope library.Scope) (int, error) {
	n, err := sqlc.New(s.pool).CountUnreadNotifications(ctx, sqlc.CountUnreadNotificationsParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	return int(n), err
}

// MarkRead marks the scope's user's notifications among ids as read, or
// all of them when ids is nil, and announces it, so that the person's other
// windows count again.
func (s *Service) MarkRead(ctx context.Context, scope library.Scope, ids []uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := sqlc.New(tx).MarkNotificationsRead(ctx, sqlc.MarkNotificationsReadParams{Viewer: scope.Viewer, Ids: ids}); err != nil {
			return err
		}
		return announce(ctx, tx, scope.Viewer)
	})
}

// Hub listens for announcements and wakes the subscribers of their user.
type Hub struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	mu   sync.Mutex
	subs map[uuid.UUID]map[chan struct{}]struct{}
}

// NewHub returns a Hub; Run makes it listen.
func NewHub(pool *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{pool: pool, log: log, subs: map[uuid.UUID]map[chan struct{}]struct{}{}}
}

// Subscribe returns a channel that receives when something changed for the
// user, and the function that ends the subscription. Changes that come
// while the last one is not yet taken are told once.
func (h *Hub) Subscribe(user uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[user] == nil {
		h.subs[user] = map[chan struct{}]struct{}{}
	}
	h.subs[user][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[user], ch)
		if len(h.subs[user]) == 0 {
			delete(h.subs, user)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) wake(user *uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, chans := range h.subs {
		if user != nil && id != *user {
			continue
		}
		for ch := range chans {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// Run listens until ctx ends, connecting again after a failure.
func (h *Hub) Run(ctx context.Context) {
	for {
		err := h.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("listening for notifications failed; trying again", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A connection that listens is not handed out again.
	defer func() {
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	// What was announced while nobody listened is not told again; every
	// stream counts afresh instead.
	h.wake(nil)
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		user, err := uuid.Parse(n.Payload)
		if err != nil {
			h.log.Warn("a notification was announced for no user", "payload", n.Payload)
			continue
		}
		h.wake(&user)
	}
}
