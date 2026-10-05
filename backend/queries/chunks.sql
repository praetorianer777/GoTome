-- name: ListBookTextFiles :many
-- The files of a book its text may be read from, for choosing the primary one.
SELECT id, format, created_at
FROM book_files
WHERE book_id = $1 AND has_text AND NOT drm AND missing_at IS NULL AND trashed_at IS NULL;

-- name: GetPrimaryTextFile :one
SELECT primary_text_file_id FROM books WHERE id = $1;

-- name: SetPrimaryTextFile :exec
UPDATE books SET primary_text_file_id = $2 WHERE id = $1;

-- name: DeleteBookChunks :exec
-- A book's chunks, and the file's wherever they are: a file that moved to
-- another book takes its text along.
DELETE FROM book_chunks WHERE book_id = $1 OR file_id = sqlc.arg(file_id);

-- name: DeleteFileChunks :exec
DELETE FROM book_chunks WHERE file_id = $1;

-- name: InsertBookChunks :copyfrom
INSERT INTO book_chunks (
    book_id, library_id, file_id, position, chapter, page_from, page_to,
    char_offset, lang, body_en, body_de, body_xx
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: AddSearchWords :exec
-- Every word of the book's chunks that is not known yet. In order, so that
-- two books adding the same new words wait for each other instead of
-- deadlocking; words too long to be typed are left out.
INSERT INTO search_words (word)
SELECT DISTINCT w
FROM book_chunks c,
     unnest(tsvector_to_array(to_tsvector('simple', coalesce(c.body_en, c.body_de, c.body_xx)))) AS w
WHERE c.book_id = $1 AND length(w) BETWEEN 2 AND 40
ORDER BY w
ON CONFLICT (word) DO NOTHING;

-- name: SetFileChunked :exec
UPDATE book_files SET chunked_at = now() WHERE id = $1;

-- name: GetFileForChunking :one
SELECT f.id, f.book_id, f.library_id, f.format, f.sha256, f.rel_path,
       l.root_path, b.language, b.primary_text_file_id
FROM book_files f
JOIN libraries l ON l.id = f.library_id
JOIN books b ON b.id = f.book_id
WHERE f.id = $1 AND f.missing_at IS NULL AND f.trashed_at IS NULL;

-- name: ListUnchunkedTextFiles :many
-- The primary text files whose text is still to be cut into chunks.
SELECT f.id
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id
WHERE f.chunked_at IS NULL AND f.missing_at IS NULL AND f.trashed_at IS NULL
  AND b.deleted_at IS NULL AND NOT b.placeholder;
