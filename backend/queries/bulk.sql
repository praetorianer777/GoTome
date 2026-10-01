-- name: CreateBulkChange :one
INSERT INTO bulk_changes (created_by, sees_all, action, change)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: AddBulkChangeBooks :exec
INSERT INTO bulk_change_books (bulk_change_id, position, book_id)
SELECT sqlc.arg(bulk_change_id), b.ord::integer, b.id
FROM unnest(sqlc.arg(book_ids)::uuid[]) WITH ORDINALITY AS b (id, ord);

-- name: GetBulkChange :one
SELECT * FROM bulk_changes WHERE id = $1;

-- name: NextBulkChangeBooks :many
-- The books whose turn has not come yet, in the order they were chosen.
SELECT book_id
FROM bulk_change_books
WHERE bulk_change_id = $1 AND outcome IS NULL
ORDER BY position
LIMIT $2;

-- name: SetBulkChangeOutcome :exec
UPDATE bulk_change_books
SET outcome = $3, message = $4, skipped = $5
WHERE bulk_change_id = $1 AND book_id = $2;

-- name: FinishBulkChange :exec
UPDATE bulk_changes SET finished_at = now() WHERE id = $1 AND finished_at IS NULL;

-- name: ListBulkChangeBooks :many
SELECT cb.book_id, cb.outcome, cb.message, cb.skipped, b.title
FROM bulk_change_books cb
JOIN books b ON b.id = cb.book_id
WHERE cb.bulk_change_id = $1
ORDER BY cb.position;
