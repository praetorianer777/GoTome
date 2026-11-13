-- name: GetKindleAddress :one
SELECT kindle_address FROM users WHERE id = $1;

-- name: SetKindleAddress :exec
UPDATE users SET kindle_address = sqlc.narg(address), updated_at = now() WHERE id = sqlc.arg(id);

-- name: ListKindleFiles :many
-- The books among ids that the viewer may see, each with its first EPUB
-- that is on disk.
SELECT DISTINCT ON (b.id) b.id AS book_id, b.title, f.id AS file_id, f.size_bytes
FROM books b
JOIN book_files f ON f.book_id = b.id AND f.format = 'epub'
  AND f.trashed_at IS NULL AND f.missing_at IS NULL
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]) AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ORDER BY b.id, f.created_at, f.id;

-- name: QueueKindleSend :one
-- Nothing when the book already waits to be sent for the person.
INSERT INTO kindle_sends (user_id, book_id, file_id, title, address)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, book_id) WHERE state IN ('queued', 'sending') DO NOTHING
RETURNING id;

-- name: ClaimKindleSend :one
-- Takes a queued send for the job, once: a second run finds it taken.
UPDATE kindle_sends SET state = 'sending'
WHERE id = $1 AND state = 'queued'
RETURNING id, user_id, book_id, file_id, title, address;

-- name: GetKindleSendState :one
SELECT state FROM kindle_sends WHERE id = $1;

-- name: RequeueKindleSend :exec
UPDATE kindle_sends SET state = 'queued' WHERE id = $1 AND state = 'sending';

-- name: FinishKindleSend :one
-- Records the outcome of a send that was being sent, once.
UPDATE kindle_sends SET state = sqlc.arg(state), message = sqlc.arg(message), finished_at = now()
WHERE id = sqlc.arg(id) AND state IN ('queued', 'sending')
RETURNING id, user_id, book_id, title;

-- name: ListBookKindleSends :many
-- The person's latest sends of the book.
SELECT id, state, message, address, created_at, finished_at
FROM kindle_sends
WHERE book_id = $1 AND user_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 5;

-- name: GetUserRole :one
SELECT role FROM users WHERE id = $1 AND disabled_at IS NULL;
