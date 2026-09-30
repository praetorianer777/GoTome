-- name: QueueScan :one
-- Returns no row when the library already has a scan waiting or running.
INSERT INTO library_scans (library_id, requested_by)
VALUES ($1, $2)
ON CONFLICT (library_id) WHERE state IN ('queued', 'running') DO NOTHING
RETURNING *;

-- name: LockActiveScan :one
-- Locked, so that two people asking at once do not both decide that the scan
-- needs a job.
SELECT * FROM library_scans
WHERE library_id = $1 AND state IN ('queued', 'running')
FOR UPDATE;

-- name: SetScanJob :exec
UPDATE library_scans SET job_id = $2 WHERE id = $1;

-- name: StartScan :one
-- The worker takes the library's waiting scan, or the one an earlier run left
-- unfinished. A job without either still has a scan to show for its work.
INSERT INTO library_scans (library_id, state, started_at, job_id)
VALUES ($1, 'running', now(), sqlc.arg(job_id))
ON CONFLICT (library_id) WHERE state IN ('queued', 'running')
DO UPDATE SET state = 'running', started_at = COALESCE(library_scans.started_at, now())
RETURNING *;

-- name: RecordScanProgress :exec
-- What one run found is added to what earlier runs of the same scan found;
-- only the walk's count is of the whole library each time.
UPDATE library_scans
SET files_seen     = sqlc.arg(files_seen),
    files_added    = files_added + sqlc.arg(files_added),
    files_changed  = files_changed + sqlc.arg(files_changed),
    files_moved    = files_moved + sqlc.arg(files_moved),
    files_restored = files_restored + sqlc.arg(files_restored),
    files_missing  = files_missing + sqlc.arg(files_missing),
    files_skipped  = sqlc.arg(files_skipped),
    books_added    = books_added + sqlc.arg(books_added)
WHERE id = $1;

-- name: FinishScan :exec
UPDATE library_scans
SET state = $2, error = $3, finished_at = now()
WHERE id = $1;

-- name: PruneScans :exec
-- A library scanned four times a day would otherwise grow a row each time.
DELETE FROM library_scans s
WHERE s.library_id = $1
  AND s.id NOT IN (
      SELECT k.id FROM library_scans k
      WHERE k.library_id = $1
      ORDER BY k.requested_at DESC, k.id DESC
      LIMIT sqlc.arg(keep)::int
  );

-- name: LatestVisibleScans :many
-- The newest scan of each library the viewer may see.
SELECT DISTINCT ON (s.library_id) s.*
FROM library_scans s
WHERE s.library_id IN (SELECT visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY s.library_id, s.requested_at DESC, s.id DESC;

-- name: ListLibraryIDs :many
SELECT id FROM libraries ORDER BY id;

-- name: ListFilesForScan :many
-- Every file the library knows, trashed ones included: their paths are taken.
SELECT id, book_id, kind, format, rel_path, size_bytes, modified_at, sha256,
       part_index, missing_at, trashed_at
FROM book_files
WHERE library_id = $1;

-- name: UpdateFileContent :exec
-- A file whose bytes changed is a file nothing has been extracted from yet.
UPDATE book_files
SET size_bytes      = sqlc.arg(size_bytes),
    modified_at     = sqlc.arg(modified_at),
    original_sha256 = CASE WHEN sha256 IS DISTINCT FROM sqlc.arg(sha256)::bytea THEN sqlc.arg(sha256)::bytea ELSE original_sha256 END,
    content_sha256  = CASE WHEN sha256 IS DISTINCT FROM sqlc.arg(sha256)::bytea THEN NULL ELSE content_sha256 END,
    extract_state   = CASE WHEN sha256 IS DISTINCT FROM sqlc.arg(sha256)::bytea THEN 'pending' ELSE extract_state END,
    extract_error   = CASE WHEN sha256 IS DISTINCT FROM sqlc.arg(sha256)::bytea THEN NULL ELSE extract_error END,
    sha256          = sqlc.arg(sha256)::bytea,
    missing_at      = NULL,
    updated_at      = now()
WHERE id = $1;

-- name: MoveFile :exec
UPDATE book_files
SET rel_path = $2, size_bytes = $3, modified_at = $4, missing_at = NULL, updated_at = now()
WHERE id = $1;

-- name: RestoreFile :exec
UPDATE book_files
SET missing_at = NULL, updated_at = now()
WHERE id = $1;

-- name: MarkFilesMissing :execrows
UPDATE book_files
SET missing_at = now(), updated_at = now()
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND missing_at IS NULL;

-- name: SetFilePartIndex :exec
UPDATE book_files
SET part_index = $2, updated_at = now()
WHERE id = $1;
