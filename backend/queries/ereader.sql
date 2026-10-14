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
