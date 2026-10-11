-- name: ListFilesToHash :many
-- Ebook files on disk whose KOReader MD5 is not worked out yet, with where
-- they lie.
SELECT f.id, f.rel_path, l.root_path
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.koreader_md5 IS NULL AND f.kind = 'ebook'
  AND f.trashed_at IS NULL AND f.missing_at IS NULL
ORDER BY f.id
LIMIT $1;

-- name: SetKoreaderMD5 :exec
UPDATE book_files SET koreader_md5 = $2 WHERE id = $1;

-- name: GetKoreaderDocument :one
-- The file KOReader names by the MD5, of a book the viewer sees; of
-- several copies, the newest.
SELECT f.id, f.book_id, f.format, f.page_count
FROM book_files f
JOIN books b ON b.id = f.book_id
WHERE f.koreader_md5 = sqlc.arg(md5)::text AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY f.id DESC
LIMIT 1;
