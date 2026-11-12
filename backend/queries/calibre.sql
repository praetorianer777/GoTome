-- name: CreateCalibreImport :one
INSERT INTO calibre_imports (user_id, library_id, sees_all, summary)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeleteStaleCalibreImports :exec
-- Previews nobody went on with.
DELETE FROM calibre_imports
WHERE user_id = $1 AND started_at IS NULL AND created_at < now() - interval '1 day';

-- name: AddCalibreImportBooks :copyfrom
INSERT INTO calibre_import_books (import_id, calibre_id, title, found, missing)
VALUES ($1, $2, $3, $4, $5);

-- name: GetCalibreImport :one
SELECT * FROM calibre_imports WHERE id = $1 AND user_id = $2;

-- name: GetCalibreImportAny :one
SELECT * FROM calibre_imports WHERE id = $1;

-- name: ListCalibreImportBooks :many
SELECT * FROM calibre_import_books WHERE import_id = $1 ORDER BY calibre_id;

-- name: StartCalibreImport :execrows
UPDATE calibre_imports SET started_at = now(), ratings = $2, columns = $3
WHERE id = $1 AND started_at IS NULL;

-- name: NextCalibreImportBooks :many
SELECT * FROM calibre_import_books
WHERE import_id = $1 AND outcome IS NULL
ORDER BY calibre_id
LIMIT $2;

-- name: SetCalibreImportBookOutcome :exec
UPDATE calibre_import_books SET outcome = $3, message = $4, book_id = $5
WHERE import_id = $1 AND calibre_id = $2;

-- name: FinishCalibreImport :execrows
UPDATE calibre_imports SET finished_at = now() WHERE id = $1 AND finished_at IS NULL;

-- name: ListFilesByRelPath :many
-- The live files of a library at these paths.
SELECT id, book_id, rel_path, missing_at
FROM book_files
WHERE library_id = $1 AND rel_path = ANY (sqlc.arg(paths)::text[]) AND trashed_at IS NULL;

-- name: BookDescribedDigest :one
-- What a book is described as, for telling whether an import changed it.
SELECT md5(concat_ws('|',
    b.title, coalesce(b.subtitle, ''), coalesce(b.description, ''), coalesce(b.language, ''),
    coalesce(b.published_on::text, ''), coalesce(b.published_precision, ''),
    coalesce(b.publisher_id::text, ''), coalesce(b.series_id::text, ''), coalesce(b.series_index::text, ''),
    coalesce(b.cover_key, ''), b.field_sources::text,
    coalesce((SELECT string_agg(c.author_id::text || c.role, ',' ORDER BY c.position, c.author_id)
              FROM book_contributors c WHERE c.book_id = b.id), ''),
    coalesce((SELECT string_agg(t.tag_id::text, ',' ORDER BY t.tag_id)
              FROM book_tags t WHERE t.book_id = b.id), ''),
    coalesce((SELECT string_agg(i.type || ':' || i.value, ',' ORDER BY i.type, i.value)
              FROM book_identifiers i WHERE i.book_id = b.id AND i.file_id IS NULL), '')
))::text AS digest
FROM books b WHERE b.id = $1;
