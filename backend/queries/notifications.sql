-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, data, link, book_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: ListNotifications :many
-- The viewer's notifications, newest first, after the cursor; one about a
-- book is left out while its library may not be seen.
SELECT n.id, n.kind, n.data, n.link, n.book_id, n.created_at, n.read_at
FROM notifications n
LEFT JOIN books b ON b.id = n.book_id
WHERE n.user_id = sqlc.arg(viewer)::uuid
  AND (n.book_id IS NULL OR (b.deleted_at IS NULL
       AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))))
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (n.created_at, n.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT sqlc.arg(max_rows);

-- name: CountUnreadNotifications :one
SELECT count(*)::int
FROM notifications n
LEFT JOIN books b ON b.id = n.book_id
WHERE n.user_id = sqlc.arg(viewer)::uuid AND n.read_at IS NULL
  AND (n.book_id IS NULL OR (b.deleted_at IS NULL
       AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))));

-- name: MarkNotificationsRead :exec
-- Marks the viewer's notifications among ids, or all of them when ids is
-- NULL, as read.
UPDATE notifications SET read_at = now()
WHERE user_id = sqlc.arg(viewer)::uuid AND read_at IS NULL
  AND (sqlc.narg(ids)::uuid[] IS NULL OR id = ANY(sqlc.narg(ids)::uuid[]));
