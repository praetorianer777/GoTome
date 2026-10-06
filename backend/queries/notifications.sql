-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, data, link, book_id, library_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListNotifications :many
-- The viewer's notifications, newest first, after the cursor; one about a
-- book or a library is left out while that library may not be seen.
SELECT n.id, n.kind, n.data, n.link, n.book_id, n.created_at, n.read_at
FROM notifications n
LEFT JOIN books b ON b.id = n.book_id
WHERE n.user_id = sqlc.arg(viewer)::uuid
  AND (n.book_id IS NULL OR (b.deleted_at IS NULL
       AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))))
  AND (n.library_id IS NULL
       OR n.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (n.created_at, n.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT sqlc.arg(max_rows);

-- name: CountUnreadNotifications :one
SELECT count(*)::int
FROM notifications n
LEFT JOIN books b ON b.id = n.book_id
WHERE n.user_id = sqlc.arg(viewer)::uuid AND n.read_at IS NULL
  AND (n.book_id IS NULL OR (b.deleted_at IS NULL
       AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))))
  AND (n.library_id IS NULL
       OR n.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)));

-- name: MarkNotificationsRead :exec
-- Marks the viewer's notifications among ids, or all of them when ids is
-- NULL, as read.
UPDATE notifications SET read_at = now()
WHERE user_id = sqlc.arg(viewer)::uuid AND read_at IS NULL
  AND (sqlc.narg(ids)::uuid[] IS NULL OR id = ANY(sqlc.narg(ids)::uuid[]));

-- name: QueueNotificationEvent :execrows
-- One event for each person who is to hear of it: active, of a role the
-- kind is for, among the recipients when they are given, subscribed to the
-- kind (or it is on by default), and seeing every library the event names.
INSERT INTO notification_events (user_id, kind, library_id, book_id, data, link)
SELECT u.id, sqlc.arg(kind)::text, sqlc.narg(library_id)::uuid, sqlc.narg(book_id)::uuid,
       sqlc.arg(data)::jsonb, sqlc.arg(link)::text
FROM users u
LEFT JOIN notification_subscriptions s
       ON s.user_id = u.id AND s.kind = sqlc.arg(kind)::text AND s.channel = 'app'
WHERE u.disabled_at IS NULL
  AND u.role = ANY(sqlc.arg(roles)::text[])
  AND (sqlc.narg(recipients)::uuid[] IS NULL OR u.id = ANY(sqlc.narg(recipients)::uuid[]))
  AND coalesce(s.enabled, sqlc.arg(default_on)::boolean)
  AND NOT EXISTS (
      SELECT unnest(sqlc.arg(libraries)::uuid[])
      EXCEPT
      SELECT visible_library_ids(u.id, u.role = ANY(sqlc.arg(sees_all_roles)::text[])));

-- name: NotificationEventSpan :one
-- When the oldest and the newest waiting event were queued.
SELECT coalesce(min(created_at), now())::timestamptz AS oldest, coalesce(max(created_at), now())::timestamptz AS newest,
       count(*)::int AS events
FROM notification_events;

-- name: TakeNotificationEvents :many
-- The events queued until then, oldest first, gone from the queue.
DELETE FROM notification_events
WHERE created_at <= sqlc.arg(until)::timestamptz
RETURNING id, user_id, kind, library_id, book_id, data, link, created_at;

-- name: ListNotificationSubscriptions :many
SELECT kind, enabled FROM notification_subscriptions WHERE user_id = $1 AND channel = 'app';

-- name: PutNotificationSubscription :exec
INSERT INTO notification_subscriptions (user_id, kind, channel, enabled)
VALUES ($1, $2, 'app', $3)
ON CONFLICT (user_id, kind, channel) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: ListWishers :many
-- Who has the book on their wishlist.
SELECT user_id FROM user_books WHERE book_id = $1 AND status = 'wishlist';

-- name: GetBookTitleAndLibrary :one
SELECT title, library_id FROM books WHERE id = $1;

-- name: ListBookLibraries :many
SELECT DISTINCT library_id FROM books WHERE id = ANY(sqlc.arg(ids)::uuid[]);
