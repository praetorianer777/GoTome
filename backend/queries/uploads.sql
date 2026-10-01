-- name: LockLibraryFiles :exec
-- Uploads and scans both add files to a library. Whoever holds this lock
-- until the end of their transaction is the only one choosing new paths in
-- it, and sees every path the other has taken.
SELECT pg_advisory_xact_lock(hashtextextended('book_files:' || sqlc.arg(library_id)::uuid::text, 0));

-- name: ListTakenPaths :many
SELECT rel_path
FROM book_files
WHERE library_id = $1 AND rel_path = ANY (sqlc.arg(paths)::text[]);

-- name: FindVisibleFileByHash :one
-- A file with exactly these bytes in a library the viewer may see, the
-- oldest first. Files that are gone or in the trash do not count.
SELECT b.id AS book_id, b.library_id, b.title
FROM book_files f
JOIN books b ON b.id = f.book_id
WHERE f.sha256 = sqlc.arg(sha256)
  AND f.missing_at IS NULL AND f.trashed_at IS NULL AND b.deleted_at IS NULL
  AND f.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY f.created_at, f.id
LIMIT 1;

-- name: ListFolderFiles :many
-- The files below one folder of a library, for grouping a new file with them.
SELECT id, book_id, rel_path, part_index
FROM book_files
WHERE library_id = $1 AND trashed_at IS NULL
  AND starts_with(rel_path, sqlc.arg(folder)::text || '/');

-- name: GetBookTitle :one
SELECT title FROM books WHERE id = $1;
