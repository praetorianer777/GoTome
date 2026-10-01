-- name: GetUserBook :one
SELECT * FROM user_books WHERE user_id = $1 AND book_id = $2;

-- name: LockVisibleBookIDs :many
-- The books among these the viewer may see, locked against a change of
-- their state in parallel.
SELECT b.id
FROM books b
WHERE b.id = ANY(sqlc.arg(ids)::uuid[])
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY b.id
FOR SHARE;

-- name: PutUserBook :exec
INSERT INTO user_books (user_id, book_id, status, rating, started_on, finished_on)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, book_id) DO UPDATE
SET status = EXCLUDED.status, rating = EXCLUDED.rating, started_on = EXCLUDED.started_on,
    finished_on = EXCLUDED.finished_on, updated_at = now();
