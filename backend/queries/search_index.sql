-- name: GetIndexVersion :one
SELECT version FROM index_versions WHERE name = $1;

-- name: SetIndexVersion :exec
INSERT INTO index_versions (name, version) VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET version = EXCLUDED.version, updated_at = now();

-- name: ClearChunked :many
-- Marks the primary text files of the visible books, of a library or of
-- one book when named, as still to be chunked. Their chunks stay, and are
-- found, until the new ones replace them.
UPDATE book_files f SET chunked_at = NULL
FROM books b
WHERE f.id = b.primary_text_file_id
  AND b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
  AND (sqlc.narg(book_id)::uuid IS NULL OR b.id = sqlc.narg(book_id)::uuid)
  AND f.missing_at IS NULL AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND NOT b.placeholder
RETURNING f.id;

-- name: ClearAllChunked :exec
UPDATE book_files SET chunked_at = NULL WHERE chunked_at IS NOT NULL;

-- name: CountChunked :one
-- How many of the visible books' primary text files, of a library when
-- named, there are, and how many of them are chunked.
SELECT count(*) AS files, count(f.chunked_at) AS chunked
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id
WHERE b.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
  AND f.missing_at IS NULL AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND NOT b.placeholder;

