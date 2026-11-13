-- name: UpsertFileWords :exec
-- The counts of the file's text as just chunked, for the chunked_at the
-- file has in this transaction.
INSERT INTO file_words (file_id, version, chunked_at, words, counts)
SELECT f.id, sqlc.arg(version)::int, f.chunked_at, sqlc.arg(words)::int, sqlc.arg(counts)::bytea
FROM book_files f
WHERE f.id = sqlc.arg(file_id)::uuid AND f.chunked_at IS NOT NULL
ON CONFLICT (file_id) DO UPDATE
SET version = excluded.version, chunked_at = excluded.chunked_at,
    words = excluded.words, counts = excluded.counts, counted_at = now();

-- name: ListFilesToCount :many
-- The primary text files that are chunked and whose words are not counted
-- (or were counted for another chunking or version), in ID order from after.
SELECT f.id
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id
LEFT JOIN file_words w ON w.file_id = f.id
WHERE f.chunked_at IS NOT NULL AND f.missing_at IS NULL AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND (w.file_id IS NULL OR w.chunked_at <> f.chunked_at OR w.version <> sqlc.arg(version)::int)
  AND f.id > sqlc.arg(after)::uuid
ORDER BY f.id
LIMIT sqlc.arg(page)::int;
