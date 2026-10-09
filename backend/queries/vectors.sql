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

-- name: ListSimilarBooks :many
-- The books nearest the given one among those the viewer sees, by the
-- cosine of their vectors of the model: an exact scan, which at two vectors
-- a book needs no index. Where two books both have a content and a metadata
-- vector the two are blended, the text weighing more, as it says more of
-- what a book is about than its description; otherwise the kind they share
-- decides. The book itself, books it is known to be a copy of (a duplicate
-- pair in any state) and books it is related to are left out.
WITH mine AS (
    SELECT v.kind, v.embedding
    FROM book_vectors v
    WHERE v.book_id = sqlc.arg(book_id)::uuid
      AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text
),
near AS (
    SELECT v.book_id,
           max(1 - (v.embedding <=> m.embedding)) FILTER (WHERE v.kind = 'content') AS content,
           max(1 - (v.embedding <=> m.embedding)) FILTER (WHERE v.kind = 'metadata') AS metadata
    FROM book_vectors v
    JOIN mine m ON m.kind = v.kind
    WHERE v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text
      AND v.book_id <> sqlc.arg(book_id)::uuid
    GROUP BY v.book_id
)
SELECT n.book_id,
       (CASE WHEN n.content IS NOT NULL AND n.metadata IS NOT NULL
             THEN 0.6 * n.content + 0.4 * n.metadata
             ELSE coalesce(n.content, n.metadata)
        END)::float8 AS score
FROM near n
JOIN books b ON b.id = n.book_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND NOT EXISTS (
      SELECT 1 FROM duplicate_pairs p
      WHERE (p.book_a, p.book_b) IN ((n.book_id, sqlc.arg(book_id)::uuid), (sqlc.arg(book_id)::uuid, n.book_id)))
  AND NOT EXISTS (
      SELECT 1 FROM book_relations r
      WHERE (r.book_a, r.book_b) IN ((n.book_id, sqlc.arg(book_id)::uuid), (sqlc.arg(book_id)::uuid, n.book_id)))
ORDER BY score DESC, n.book_id
LIMIT sqlc.arg(max)::int;

-- name: ListSimilarCheckSample :many
-- The books a similar-check asks about: with a content vector of the model,
-- by an author with at least min_books books in either order of the name,
-- in the order of a hash of their ID, so a library gives the same sample
-- each time.
WITH people AS (
    SELECT a.person_key, count(DISTINCT c.book_id) AS books
    FROM book_contributors c
    JOIN authors a ON a.id = c.author_id
    JOIN books b ON b.id = c.book_id
    WHERE c.role = 'author' AND b.deleted_at IS NULL AND NOT b.placeholder
    GROUP BY a.person_key
)
SELECT b.id
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND EXISTS (SELECT 1 FROM book_vectors v
              WHERE v.book_id = b.id AND v.kind = 'content'
                AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text)
  AND EXISTS (SELECT 1 FROM book_contributors c
              JOIN authors a ON a.id = c.author_id
              JOIN people p ON p.person_key = a.person_key
              WHERE c.book_id = b.id AND c.role = 'author' AND p.books >= sqlc.arg(min_books)::int)
ORDER BY md5(b.id::text)
LIMIT sqlc.arg(max)::int;

-- name: ListBookTraits :many
-- What a similar-check compares books by: the title, the series and the
-- authors, each by person_key.
SELECT b.id, b.title, b.series_id,
       COALESCE(array_agg(a.person_key ORDER BY c.position) FILTER (WHERE a.person_key IS NOT NULL), '{}')::text[] AS people
FROM books b
LEFT JOIN book_contributors c ON c.book_id = b.id AND c.role = 'author'
LEFT JOIN authors a ON a.id = c.author_id
WHERE b.id = ANY(sqlc.arg(ids)::uuid[])
GROUP BY b.id;

-- name: ListBooksNearQuery :many
-- The books whose vectors of the model are nearest a query's, among those
-- the viewer sees, of one library when named, as ListSimilarBooks blends
-- them: the best first, a page at a time.
WITH near AS (
    SELECT v.book_id,
           max(1 - (v.embedding <=> sqlc.arg(query)::text::vector)) FILTER (WHERE v.kind = 'content') AS content,
           max(1 - (v.embedding <=> sqlc.arg(query)::text::vector)) FILTER (WHERE v.kind = 'metadata') AS metadata
    FROM book_vectors v
    WHERE v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text
    GROUP BY v.book_id
)
SELECT n.book_id,
       (CASE WHEN n.content IS NOT NULL AND n.metadata IS NOT NULL
             THEN 0.6 * n.content + 0.4 * n.metadata
             ELSE coalesce(n.content, n.metadata)
        END)::float8 AS score
FROM near n
JOIN books b ON b.id = n.book_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
ORDER BY score DESC, n.book_id
LIMIT sqlc.arg(max)::int OFFSET sqlc.arg(skip)::int;

-- name: ListBookTitles :many
-- Books by their title and first author, for a check to print.
SELECT b.id, b.title,
       COALESCE((SELECT a.name FROM book_contributors c JOIN authors a ON a.id = c.author_id
                 WHERE c.book_id = b.id AND c.role = 'author' ORDER BY c.position LIMIT 1), '')::text AS author
FROM books b
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]);
