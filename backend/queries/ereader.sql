-- name: ListFilesToMeasure :many
-- EPUBs on disk not measured yet, with where they lie.
SELECT f.id, f.rel_path, f.sha256, l.root_path
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.format = 'epub' AND f.ereader_findings IS NULL
  AND f.trashed_at IS NULL AND f.missing_at IS NULL
ORDER BY f.id
LIMIT $1;

-- name: SetFileMeasures :exec
-- The measures of a file as it is now: one whose bytes changed since it
-- was read keeps none, and is measured again.
UPDATE book_files
SET epub_measures = sqlc.narg(measures)::jsonb, ereader_findings = sqlc.arg(findings)::text[]
WHERE id = sqlc.arg(id) AND sha256 IS NOT DISTINCT FROM sqlc.narg(sha256)::bytea;

-- name: GetFileToFix :one
-- A file of a library the viewer sees, with what fixing it needs.
SELECT f.id, f.book_id, f.library_id, f.format, f.rel_path, f.sha256, f.drm,
       f.missing_at, f.trashed_at, f.ereader_findings, l.root_path, l.writable
FROM book_files f
JOIN libraries l ON l.id = f.library_id
WHERE f.id = sqlc.arg(id)
  AND f.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: ListFilesToFix :many
-- The book's EPUBs on disk with a finding among those given.
SELECT f.id
FROM book_files f
WHERE f.book_id = sqlc.arg(book_id) AND f.format = 'epub'
  AND f.trashed_at IS NULL AND f.missing_at IS NULL
  AND f.ereader_findings && sqlc.arg(findings)::text[]
ORDER BY f.id;

-- name: RelPathTaken :one
SELECT EXISTS (SELECT 1 FROM book_files WHERE library_id = $1 AND rel_path = $2);

-- name: AddTrashedOriginal :one
-- The file as it was before a fix, as a file of its own in the trash under
-- another path, from which it can be restored beside the fixed one.
INSERT INTO book_files (
    book_id, library_id, kind, format, rel_path, size_bytes, modified_at, sha256, original_sha256,
    content_sha256, page_count, pages_estimated, has_text, drm, extract_state, uploaded_by,
    epub_measures, ereader_findings, trashed_at, trashed_by)
SELECT s.book_id, s.library_id, s.kind, s.format, sqlc.arg(rel_path), s.size_bytes, s.modified_at, s.sha256, s.original_sha256,
       s.content_sha256, s.page_count, s.pages_estimated, s.has_text, s.drm, s.extract_state, s.uploaded_by,
       s.epub_measures, s.ereader_findings, now(), sqlc.arg(by)::uuid
FROM book_files s
WHERE s.id = sqlc.arg(id)
RETURNING book_files.id;

-- name: SetFileFixed :exec
-- The file as a fix wrote it. original_sha256 stays what arrived; the
-- content is read again.
UPDATE book_files
SET sha256      = sqlc.arg(sha256),
    size_bytes  = sqlc.arg(size_bytes),
    modified_at = sqlc.arg(modified_at),
    updated_at  = now()
WHERE id = sqlc.arg(id);

-- name: ProgressOfFileByFraction :exec
-- Places read in a file whose documents a fix changed say only how far.
UPDATE reading_progress
SET locator = 'fraction:' || fraction::text
WHERE file_id = $1 AND locator NOT LIKE 'fraction:%';

-- name: SendBookAgain :exec
-- The devices that had the book get it again.
UPDATE deliveries SET queued_at = now(), delivered_at = NULL
WHERE book_id = $1 AND delivered_at IS NOT NULL;
