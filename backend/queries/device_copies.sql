-- name: GetFileToCopy :one
-- A file with what a copy for a device is made from.
SELECT f.id, f.format, f.rel_path, f.sha256, f.drm, f.missing_at, f.trashed_at,
       f.ereader_findings, l.root_path
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.id = $1;

-- name: ListFilesToCopy :many
-- One EPUB on disk with e-reader findings for each hash among the books'
-- files that has no copy, and no note that there is none to make, of this
-- version.
SELECT DISTINCT ON (f.sha256) f.id, f.sha256
FROM book_files f
WHERE f.book_id = ANY(sqlc.arg(book_ids)::uuid[]) AND f.format = 'epub' AND NOT f.drm
  AND f.trashed_at IS NULL AND f.missing_at IS NULL AND f.sha256 IS NOT NULL
  AND cardinality(f.ereader_findings) > 0
  AND NOT EXISTS (SELECT 1 FROM device_copies c
                  WHERE c.source_sha256 = f.sha256 AND c.fix_version = sqlc.arg(fix_version)::smallint
                    AND c.state IN ('ready', 'unfixable'))
ORDER BY f.sha256, f.id;

-- name: GetDeviceCopy :one
SELECT state, sha256, size_bytes, koreader_md5, last_used_at
FROM device_copies
WHERE source_sha256 = $1 AND fix_version = $2;

-- name: GetReadyCopyOfFile :one
-- The size of the copy of a file, if one is ready.
SELECT c.sha256, c.size_bytes
FROM book_files f
JOIN device_copies c ON c.source_sha256 = f.sha256 AND c.fix_version = $2 AND c.state = 'ready'
WHERE f.id = $1;

-- name: SetDeviceCopy :exec
INSERT INTO device_copies (source_sha256, fix_version, state, sha256, size_bytes, koreader_md5)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source_sha256, fix_version) DO UPDATE
SET state = EXCLUDED.state, sha256 = EXCLUDED.sha256, size_bytes = EXCLUDED.size_bytes,
    koreader_md5 = EXCLUDED.koreader_md5, made_at = now(), last_used_at = now();

-- name: TouchDeviceCopy :exec
UPDATE device_copies SET last_used_at = now()
WHERE source_sha256 = $1 AND fix_version = $2 AND last_used_at < now() - interval '1 hour';

-- name: EvictDeviceCopy :exec
UPDATE device_copies SET state = 'evicted'
WHERE source_sha256 = $1 AND fix_version = $2 AND state = 'ready';

-- name: ListCopiesToEvict :many
-- The ready copies unused for longer than max_age, then the least recently
-- used of the rest until what remains is within max_bytes.
SELECT source_sha256, fix_version
FROM (
    SELECT source_sha256, fix_version, last_used_at,
           sum(size_bytes) OVER (ORDER BY last_used_at DESC, source_sha256) AS running
    FROM device_copies
    WHERE state = 'ready'
) c
WHERE c.last_used_at < now() - sqlc.arg(max_age_days)::int * interval '1 day' OR c.running > sqlc.arg(max_bytes)::bigint;

-- name: DeleteStaleCopyNotes :execrows
-- The notes of copies that were not made, once old, and the hashes of
-- evicted copies whose source no file has any more.
DELETE FROM device_copies c
WHERE (c.state = 'unfixable' AND c.made_at < now() - sqlc.arg(max_age_days)::int * interval '1 day')
   OR (c.state = 'evicted' AND NOT EXISTS (SELECT 1 FROM book_files f WHERE f.sha256 = c.source_sha256));

-- name: ListLiveCopies :many
SELECT source_sha256, fix_version FROM device_copies WHERE state = 'ready';

-- name: RecordDeviceSend :exec
INSERT INTO device_sends (device_id, book_id, fixed) VALUES ($1, $2, $3)
ON CONFLICT (device_id, book_id) DO UPDATE SET fixed = EXCLUDED.fixed, sent_at = now();
