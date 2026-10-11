-- name: QueueDeliveries :execrows
-- Puts the books among ids the viewer may see, and that have an ebook on
-- disk, in the queue of the user's device; one delivered before is queued
-- again, one queued already stays as it is.
INSERT INTO deliveries (device_id, book_id)
SELECT d.id, b.id
FROM devices d
CROSS JOIN unnest(sqlc.arg(ids)::uuid[]) AS n(id)
JOIN books b ON b.id = n.id
WHERE d.id = sqlc.arg(device_id) AND d.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND EXISTS (SELECT 1 FROM book_files f
              WHERE f.book_id = b.id AND f.kind = 'ebook' AND f.trashed_at IS NULL AND f.missing_at IS NULL)
ON CONFLICT (device_id, book_id) DO UPDATE
SET queued_at = now(), delivered_at = NULL
WHERE deliveries.delivered_at IS NOT NULL;

-- name: OwnsDevice :one
SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1 AND user_id = $2);

-- name: ListBookDeliveries :many
-- The book's deliveries to the user's devices, the latest first.
SELECT dl.id, dl.device_id, d.name AS device_name, dl.queued_at, dl.delivered_at
FROM deliveries dl
JOIN devices d ON d.id = dl.device_id
WHERE dl.book_id = $1 AND d.user_id = $2
ORDER BY dl.queued_at DESC;

-- name: TakeBackDelivery :execrows
-- Takes a book out of the queue of one of the user's devices before the
-- device has it.
DELETE FROM deliveries dl
USING devices d
WHERE dl.id = $1 AND d.id = dl.device_id AND d.user_id = $2 AND dl.delivered_at IS NULL;

-- name: ListQueuedBooks :many
-- The books queued for the device that its user still sees, the first sent
-- first.
SELECT dl.book_id
FROM deliveries dl
JOIN books b ON b.id = dl.book_id
WHERE dl.device_id = sqlc.arg(device_id) AND dl.delivered_at IS NULL
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY dl.queued_at
LIMIT 200;

-- name: MarkDelivered :execrows
-- The device has downloaded the book: it leaves the queue, once.
UPDATE deliveries SET delivered_at = now()
WHERE device_id = $1 AND book_id = $2 AND delivered_at IS NULL;
