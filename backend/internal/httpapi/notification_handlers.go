package httpapi

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// keepAlive is how often an idle stream sends a comment, so that a proxy
// does not take it for dead.
const keepAlive = 25 * time.Second

// notification is something that happened that the caller should hear of.
type notification struct {
	ID uuid.UUID `json:"id"`
	// Kind says what happened; the web app words it from data.
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
	// Link is the app's path to go to for more; left out when there is none.
	Link      string     `json:"link,omitempty"`
	BookID    *uuid.UUID `json:"bookId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	// ReadAt is when the caller marked it read; left out while it is unread.
	ReadAt *time.Time `json:"readAt,omitempty"`
}

type notificationList struct {
	Notifications []notification `json:"notifications"`
	// Unread is how many of the caller's notifications are unread.
	Unread int `json:"unread"`
	// NextCursor asks for older ones; left out on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

type notificationsQuery struct {
	Cursor string `query:"cursor" doc:"The nextCursor of the page before."`
	Limit  int    `query:"limit" doc:"How many notifications a page holds, at most 100; 20 when left out."`
}

func encodeNotificationCursor(c *notify.Cursor) string {
	if c == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(c.At.Format(time.RFC3339Nano) + "|" + c.ID.String()))
}

func decodeNotificationCursor(s string) (*notify.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	at, id, _ := strings.Cut(string(raw), "|")
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, err
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	return &notify.Cursor{At: t, ID: u}, nil
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) error {
	var q notificationsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	var after *notify.Cursor
	if q.Cursor != "" {
		var err error
		if after, err = decodeNotificationCursor(q.Cursor); err != nil {
			return ErrValidation(map[string]string{"cursor": "This cursor was not handed out here; start again from the first page."})
		}
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	list, next, err := s.Notifications.List(r.Context(), scope, after, cmp.Or(q.Limit, 20))
	if err != nil {
		return err
	}
	unread, err := s.Notifications.Unread(r.Context(), scope)
	if err != nil {
		return err
	}
	out := notificationList{Notifications: make([]notification, len(list)), Unread: unread, NextCursor: encodeNotificationCursor(next)}
	for i, n := range list {
		data := map[string]any{}
		_ = json.Unmarshal(n.Data, &data)
		out.Notifications[i] = notification{
			ID: n.ID, Kind: n.Kind, Data: data, Link: n.Link, BookID: n.BookID, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// notificationsRead marks the caller's notifications read: those named, or
// all of them.
type notificationsRead struct {
	IDs []uuid.UUID `json:"ids,omitempty"`
	All bool        `json:"all,omitempty"`
}

type unreadCount struct {
	Unread int `json:"unread"`
}

func (s *Server) markNotificationsRead(w http.ResponseWriter, r *http.Request) error {
	var req notificationsRead
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.All == (len(req.IDs) > 0) {
		return ErrValidation(map[string]string{"ids": "Name the notifications to mark read, or say all, not both."})
	}
	ids := req.IDs
	if req.All {
		ids = nil
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	if err := s.Notifications.MarkRead(r.Context(), scope, ids); err != nil {
		return err
	}
	unread, err := s.Notifications.Unread(r.Context(), scope)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, unreadCount{Unread: unread})
	return nil
}

// streamNotifications sends, as server-sent events, the caller's unread
// count when the stream opens and again whenever something changes for
// them; the list itself is asked for with listNotifications. It runs until
// the client goes, without the server's write deadline.
func (s *Server) streamNotifications(w http.ResponseWriter, r *http.Request) error {
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	changes, stop := s.NotifyHub.Subscribe(user.ID)
	defer stop()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	// nginx and others would otherwise hold the events back.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func() error {
		n, err := s.Notifications.Unread(r.Context(), scope)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: unread\ndata: {\"unread\":%d}\n\n", n); err != nil {
			return err
		}
		return rc.Flush()
	}
	// Once the stream has begun, a failure can only end it; the client
	// opens a new one.
	if send() != nil {
		return nil
	}
	tick := time.NewTicker(keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-changes:
			if send() != nil {
				return nil
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil || rc.Flush() != nil {
				return nil
			}
		}
	}
}
