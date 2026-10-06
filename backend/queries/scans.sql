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

-- name: FinishScan :one
UPDATE library_scans
SET state = $2, error = $3, finished_at = now()
WHERE id = $1
RETURNING books_added;

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
WHERE s.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
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

-- name: ListPendingExtractions :many
-- Files of the library that are there, have been hashed, and that nothing
-- has been read out of yet, of the formats there is a reader for.
SELECT id FROM book_files
WHERE library_id = $1
  AND extract_state = 'pending'
  AND missing_at IS NULL AND trashed_at IS NULL
  AND sha256 IS NOT NULL
  AND format = ANY(sqlc.arg(formats)::text[])
ORDER BY id;

-- name: GetFileForExtraction :one
SELECT f.id, f.book_id, f.library_id, f.format, f.rel_path, f.sha256, f.missing_at, f.trashed_at, l.root_path
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.id = $1;

-- name: SetFileExtracted :execrows
-- Only while the file is still the one that was read: a scan that found it
-- changed in the meantime has asked for it to be read again.
UPDATE book_files
SET content_sha256  = sqlc.arg(content_sha256),
    drm             = sqlc.arg(drm),
    has_text        = sqlc.arg(has_text),
    page_count      = sqlc.arg(page_count),
    pages_estimated = sqlc.arg(pages_estimated),
    duration_ms     = sqlc.narg(duration_ms),
    track_number    = sqlc.narg(track_number),
    disc_number     = sqlc.narg(disc_number),
    extract_state   = 'done',
    extract_error   = NULL,
    updated_at      = now()
WHERE id = $1 AND sha256 = sqlc.arg(sha256);

-- name: SetFileExtractFailed :exec
UPDATE book_files
SET extract_state = 'failed', extract_error = sqlc.arg(error), updated_at = now()
WHERE id = $1 AND sha256 = sqlc.arg(sha256);


-- name: DeleteFileChapters :exec
DELETE FROM audio_chapters WHERE file_id = $1;

-- name: AddFileChapter :exec
INSERT INTO audio_chapters (file_id, position, title, start_ms, end_ms)
VALUES ($1, $2, $3, $4, $5);

-- name: ListBookParts :many
-- The parts of a book's audiobook, which are its audio files with a place.
SELECT id, rel_path, part_index, track_number, disc_number
FROM book_files
WHERE book_id = $1 AND kind = 'audio' AND part_index IS NOT NULL AND trashed_at IS NULL;

-- name: CountPendingFiles :many
-- Per library the viewer may see, how many files found are still to be read.
SELECT library_id, count(*)::int AS pending
FROM book_files
WHERE extract_state = 'pending'
  AND missing_at IS NULL AND trashed_at IS NULL
  AND library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY library_id;

-- name: ResetExtraction :many
-- Files to be read again, as if they were new; only those a reader exists
-- for, that are on disk and not in the trash.
UPDATE book_files
SET extract_state = 'pending', extract_error = NULL, updated_at = now()
WHERE id = ANY(sqlc.arg(ids)::uuid[])
  AND format = ANY(sqlc.arg(formats)::text[])
  AND missing_at IS NULL AND trashed_at IS NULL
RETURNING id;

-- name: ListLibraryFilesToReread :many
SELECT id FROM book_files
WHERE library_id = $1 AND missing_at IS NULL AND trashed_at IS NULL
  AND (NOT sqlc.arg(failed_only)::boolean OR extract_state = 'failed');

-- name: CountFailedFiles :many
-- Per library the viewer may see, how many files could not be read.
SELECT library_id, count(*)::int AS failed
FROM book_files
WHERE extract_state = 'failed'
  AND missing_at IS NULL AND trashed_at IS NULL
  AND library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY library_id;

-- name: ListFilesToWriteBack :many
-- The files of a book its metadata is written into: EPUBs that were read,
-- are there, are not encrypted, and lie in a library GOtome may change.
SELECT f.id
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.book_id = $1
  AND f.format = 'epub'
  AND f.extract_state = 'done'
  AND f.sha256 IS NOT NULL
  AND f.missing_at IS NULL
  AND f.trashed_at IS NULL
  AND NOT f.drm
  AND l.writable
ORDER BY f.id;

-- name: GetFileForWriteBack :one
SELECT f.id, f.book_id, f.format, f.rel_path, f.size_bytes, f.sha256, f.content_sha256,
       f.extract_state, f.drm, f.missing_at, f.trashed_at, l.root_path, l.writable
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.id = $1;

-- name: LockFileHash :one
SELECT sha256 FROM book_files WHERE id = $1 FOR UPDATE;

-- name: SetFileWritten :exec
-- The file as GOtome wrote it. original_sha256 stays what arrived, and
-- content_sha256 what the content is, which writing metadata does not change.
UPDATE book_files
SET sha256      = sqlc.arg(sha256),
    size_bytes  = sqlc.arg(size_bytes),
    modified_at = sqlc.arg(modified_at),
    updated_at  = now()
WHERE id = sqlc.arg(id);
