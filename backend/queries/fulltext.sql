-- name: AddOutboxDocs :exec
INSERT INTO fulltext_outbox (docs) VALUES (sqlc.arg(docs)::jsonb);

-- name: ListOutbox :many
-- The oldest changes the full-text index is still to get.
SELECT id, deletes, docs FROM fulltext_outbox ORDER BY id LIMIT $1;

-- name: DeleteOutbox :exec
DELETE FROM fulltext_outbox WHERE id <= $1;

-- name: SetFileIndexed :exec
UPDATE book_files SET indexed_at = now() WHERE id = $1;

-- name: ClearAllIndexed :exec
UPDATE book_files SET indexed_at = NULL WHERE indexed_at IS NOT NULL;

-- name: ListFilesToIndex :many
-- The primary text files that are chunked and whose chunks the full-text
-- index is still to get, in ID order from after.
SELECT f.id
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id
WHERE f.chunked_at IS NOT NULL AND f.indexed_at IS NULL
  AND f.missing_at IS NULL AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND f.id > sqlc.arg(after)::uuid
ORDER BY f.id
LIMIT sqlc.arg(page)::int;
