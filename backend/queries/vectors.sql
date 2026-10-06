-- name: ListBooksToEmbed :many
-- A page of books after the given one, in ID order, and whether each one's
-- vectors are missing, of another model or made from something that has
-- changed since. The content's source is the primary text file as it was
-- chunked (its ID, when, and how it is sampled); a book without one has no
-- content vector, and content_gone says one is still stored.
SELECT b.id AS book_id,
       f.id AS text_file_id,
       h.content_hash::bytea AS content_hash,
       (mv.book_id IS NULL OR mv.model <> sqlc.arg(model)::text
        OR mv.model_version <> sqlc.arg(model_version)::text
        OR mv.source_hash <> h.metadata_hash)::boolean AS metadata_stale,
       (h.content_hash IS NOT NULL
        AND (cv.book_id IS NULL OR cv.model <> sqlc.arg(model)::text
             OR cv.model_version <> sqlc.arg(model_version)::text
             OR cv.source_hash <> h.content_hash))::boolean AS content_stale,
       (h.content_hash IS NULL AND cv.book_id IS NOT NULL)::boolean AS content_gone
FROM books b
LEFT JOIN book_files f ON f.id = b.primary_text_file_id AND f.chunked_at IS NOT NULL
CROSS JOIN LATERAL (
    SELECT sha256(convert_to(book_metadata_text(b.id), 'UTF8')) AS metadata_hash,
           CASE WHEN f.id IS NOT NULL THEN
               sha256(convert_to(f.id::text || ' ' || f.chunked_at::text || ' ' || sqlc.arg(recipe)::text, 'UTF8'))
           END::bytea AS content_hash
) h
LEFT JOIN book_vectors mv ON mv.book_id = b.id AND mv.kind = 'metadata'
LEFT JOIN book_vectors cv ON cv.book_id = b.id AND cv.kind = 'content'
WHERE b.deleted_at IS NULL AND NOT b.placeholder AND b.id > sqlc.arg(after)::uuid
ORDER BY b.id
LIMIT sqlc.arg(page)::int;

-- name: GetBookMetadataText :one
SELECT coalesce(book_metadata_text(sqlc.arg(book_id)::uuid), '')::text;

-- name: ListChunkPositions :many
SELECT position FROM book_chunks WHERE file_id = $1 ORDER BY position;

-- name: ListChunkBodies :many
SELECT coalesce(body_en, body_de, body_xx)::text AS body
FROM book_chunks
WHERE file_id = sqlc.arg(file_id) AND position = ANY(sqlc.arg(positions)::int[])
ORDER BY position;

-- name: PutBookVector :exec
-- Nothing is stored for a book deleted since it was read.
INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
SELECT b.id, sqlc.arg(kind)::text, sqlc.arg(model)::text, sqlc.arg(model_version)::text,
       sqlc.arg(source_hash)::bytea, CAST(sqlc.arg(embedding)::text AS vector)
FROM books b WHERE b.id = sqlc.arg(book_id)::uuid AND b.deleted_at IS NULL
ON CONFLICT (book_id, kind) DO UPDATE
SET model = excluded.model, model_version = excluded.model_version,
    source_hash = excluded.source_hash, embedding = excluded.embedding, embedded_at = now();

-- name: DeleteBookVector :exec
DELETE FROM book_vectors WHERE book_id = $1 AND kind = $2;

-- name: DeleteVectorsOfGoneBooks :execrows
-- Deleted, merged and placeholder books have none.
DELETE FROM book_vectors v USING books b
WHERE b.id = v.book_id AND (b.deleted_at IS NOT NULL OR b.placeholder);
