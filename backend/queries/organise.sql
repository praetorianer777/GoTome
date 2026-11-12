-- name: GetOrganiseLibrary :one
SELECT id, name, root_path, mode, writable, organise_enabled, organise_pattern, unwritable_since
FROM libraries WHERE id = $1;

-- name: ListOrganiseFiles :many
-- The files of a library's books that can be moved: not trashed, of books
-- that are neither placeholders, deleted nor merged, with what the pattern
-- is made from.
SELECT f.id AS file_id, f.book_id, f.kind, f.format, f.rel_path, f.part_index, f.missing_at,
       b.title, b.series_index, b.language, b.published_on,
       COALESCE(s.name, '')::text AS series,
       COALESCE(a.name, '')::text AS author, COALESCE(a.sort_name, '')::text AS author_sort
FROM book_files f
JOIN books b ON b.id = f.book_id
LEFT JOIN series s ON s.id = b.series_id
LEFT JOIN LATERAL (
    SELECT au.name, au.sort_name
    FROM book_contributors c JOIN authors au ON au.id = c.author_id
    WHERE c.book_id = b.id AND c.role = 'author'
    ORDER BY c.position, au.name_key
    LIMIT 1
) a ON true
WHERE f.library_id = $1 AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND b.merged_into_id IS NULL AND NOT b.placeholder
ORDER BY f.book_id, f.kind, f.part_index NULLS FIRST, f.rel_path;

-- name: ListLibraryFilePaths :many
-- Every path a file's record holds in the library, trashed or missing
-- ones too: they stay taken.
SELECT id, rel_path FROM book_files WHERE library_id = $1;

-- name: ListOrganiseBookFiles :many
-- The files of the books, locked, as they are now.
SELECT id, book_id, rel_path, trashed_at, missing_at
FROM book_files
WHERE library_id = sqlc.arg(library_id) AND book_id = ANY(sqlc.arg(books)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: SetFilePath :execrows
UPDATE book_files SET rel_path = sqlc.arg(rel_path), updated_at = now()
WHERE id = sqlc.arg(id) AND rel_path = sqlc.arg(was) AND trashed_at IS NULL;

-- name: FilePathTaken :one
SELECT EXISTS (
    SELECT 1 FROM book_files
    WHERE library_id = sqlc.arg(library_id) AND lower(rel_path) = lower(sqlc.arg(rel_path)::text) AND id <> sqlc.arg(file_id));

-- name: ListOrganiseUndoFiles :many
SELECT f.id, f.library_id, f.rel_path, f.trashed_at, l.root_path, l.writable
FROM book_files f JOIN libraries l ON l.id = f.library_id
WHERE f.id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY f.id
FOR UPDATE OF f;

-- name: LibraryScanActive :one
SELECT EXISTS (SELECT 1 FROM library_scans WHERE library_id = $1 AND state IN ('queued', 'running'));

-- name: HasActiveOrganiseRun :one
SELECT EXISTS (
    SELECT 1 FROM curation_runs
    WHERE kind = 'organise' AND organise_library = $1 AND state IN ('planned', 'running'));

-- name: CreateOrganiseRun :one
INSERT INTO curation_runs (kind, rule, started_by, state, organise_library, organise_pattern)
VALUES ('organise', 'organise', sqlc.arg(started_by), 'planned', sqlc.arg(library_id), sqlc.arg(pattern))
RETURNING id;

-- name: GetOrganiseRun :one
SELECT id, state, organise_library, organise_pattern FROM curation_runs WHERE id = $1 AND kind = 'organise';

-- name: AddRunSkipped :exec
UPDATE curation_runs SET skipped = skipped + sqlc.arg(skipped)::int WHERE id = sqlc.arg(id);

-- name: ListFilesAtPaths :many
-- The files whose record holds one of the paths.
SELECT id, rel_path FROM book_files WHERE library_id = sqlc.arg(library_id) AND rel_path = ANY(sqlc.arg(paths)::text[]);
