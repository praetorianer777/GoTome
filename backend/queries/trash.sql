-- name: GetFileToTrash :one
-- A file of a library the viewer sees, with what moving it needs.
SELECT f.id, f.book_id, f.library_id, f.rel_path, f.missing_at, f.trashed_at,
       l.root_path, l.writable
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.id = sqlc.arg(id)
  AND f.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: SetFileTrashed :exec
UPDATE book_files
SET trashed_at = now(), trashed_by = sqlc.arg(by), chunked_at = NULL, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: SetFileRestored :exec
UPDATE book_files SET trashed_at = NULL, trashed_by = NULL, updated_at = now() WHERE id = $1;

-- name: ListTrash :many
-- The trashed files of the libraries the viewer sees, of one when named,
-- the latest first.
SELECT f.id, f.rel_path, f.format, f.size_bytes, f.trashed_at::timestamptz AS trashed_at,
       f.book_id, b.title, l.id AS library_id, l.name AS library_name,
       u.username AS trashed_by
FROM book_files f
JOIN books b ON b.id = f.book_id
JOIN libraries l ON l.id = f.library_id
LEFT JOIN users u ON u.id = f.trashed_by
WHERE f.trashed_at IS NOT NULL
  AND f.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR f.library_id = sqlc.narg(library_id)::uuid)
ORDER BY f.trashed_at DESC, f.id DESC;

-- name: ListExpiredTrash :many
SELECT id FROM book_files WHERE trashed_at < sqlc.arg(before)::timestamptz ORDER BY trashed_at;

-- name: GetTrashedFile :one
SELECT f.id, f.book_id, f.rel_path, l.root_path
FROM book_files f JOIN libraries l ON l.id = f.library_id
WHERE f.id = $1 AND f.trashed_at IS NOT NULL
FOR UPDATE OF f;

-- name: DeleteTrashedFile :exec
DELETE FROM book_files WHERE id = $1 AND trashed_at IS NOT NULL;
