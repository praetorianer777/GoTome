-- name: ListKoboLibrary :many
-- The books of the device's deliveries, sent and waiting, that its user still
-- sees and that have an EPUB on disk, with when their details last changed
-- and the user's place in them.
SELECT b.id, b.updated_at, p.updated_at AS progress_at, p.client_id AS progress_client
FROM deliveries dl
JOIN books b ON b.id = dl.book_id
LEFT JOIN reading_progress p ON p.book_id = b.id AND p.user_id = sqlc.arg(viewer)::uuid AND p.medium = 'ebook'
WHERE dl.device_id = sqlc.arg(device_id)
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND EXISTS (SELECT 1 FROM book_files f
              WHERE f.book_id = b.id AND f.format = 'epub'
                AND f.trashed_at IS NULL AND f.missing_at IS NULL)
ORDER BY dl.queued_at, b.id;

-- name: ListKoboBooks :many
SELECT book_id, synced_at, progress_at FROM kobo_books WHERE device_id = $1;

-- name: PutKoboBook :exec
INSERT INTO kobo_books (device_id, book_id, synced_at, progress_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (device_id, book_id) DO UPDATE
SET synced_at = EXCLUDED.synced_at, progress_at = EXCLUDED.progress_at;

-- name: DropKoboBook :exec
DELETE FROM kobo_books WHERE device_id = $1 AND book_id = $2;

-- name: ForgetKoboBooks :exec
-- A reader that syncs from the start, after a reset, is told everything again.
DELETE FROM kobo_books WHERE device_id = $1;

-- name: SetKoboProgressSeen :exec
-- The reader's own place is one it need not be told of.
UPDATE kobo_books SET progress_at = $3 WHERE device_id = $1 AND book_id = $2;
