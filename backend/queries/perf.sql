-- name: PerfFigures :one
-- What `gotome perf-check` says the library holds: the viewer's books and
-- their files, the authors and series they name, and, from the planner's
-- estimate so that no scan of the chunks is made to count them, the chunks
-- and the books with vectors of the model.
WITH vb AS (
    SELECT b.id, b.series_id
    FROM books b
    WHERE b.deleted_at IS NULL AND NOT b.placeholder
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
)
SELECT (SELECT count(*) FROM vb)::bigint AS books,
       (SELECT count(*) FROM book_files f JOIN vb ON vb.id = f.book_id WHERE f.trashed_at IS NULL)::bigint AS files,
       (SELECT count(DISTINCT c.author_id) FROM book_contributors c JOIN vb ON vb.id = c.book_id WHERE c.role = 'author')::bigint AS authors,
       (SELECT count(DISTINCT series_id) FROM vb)::bigint AS series,
       (SELECT GREATEST(reltuples, 0) FROM pg_catalog.pg_class WHERE oid = 'book_chunks'::regclass)::bigint AS chunks,
       (SELECT count(DISTINCT v.book_id) FROM book_vectors v JOIN vb ON vb.id = v.book_id WHERE v.model = sqlc.arg(model)::text)::bigint AS vectors;

-- name: PerfAuthor :one
-- The author with the most visible books under one spelling, leaving out
-- what tools write in place of an author.
SELECT a.name::text AS name
FROM book_contributors c
JOIN authors a ON a.id = c.author_id
JOIN books b ON b.id = c.book_id
WHERE c.role = 'author' AND b.deleted_at IS NULL AND NOT b.placeholder
  AND a.name_key <> ALL(sqlc.arg(placeholders)::text[])
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY a.id
ORDER BY count(*) DESC, a.id
LIMIT 1;

-- name: PerfSeries :one
SELECT s.name_key::text AS name_key
FROM books b
JOIN series s ON s.id = b.series_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY s.id
ORDER BY count(*) DESC, s.id
LIMIT 1;

-- name: PerfTag :one
SELECT t.name::text AS name
FROM book_tags bt
JOIN tags t ON t.id = bt.tag_id
JOIN books b ON b.id = bt.book_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY t.id
ORDER BY count(*) DESC, t.id
LIMIT 1;

-- name: PerfLanguage :one
SELECT lower(split_part(replace(b.language, '_', '-'), '-', 1))::text AS language
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder AND b.language IS NOT NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY 1
ORDER BY count(*) DESC, 1
LIMIT 1;

-- name: PerfLibrary :one
SELECT b.library_id::text AS id
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY b.library_id
ORDER BY count(*) DESC, b.library_id
LIMIT 1;

-- name: PerfBook :one
SELECT b.id::text AS id
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY b.id
LIMIT 1;

-- name: PerfBookWithVectors :one
-- A book with both vectors of the model, for the books like it.
SELECT b.id::text AS id
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND EXISTS (SELECT 1 FROM book_vectors v WHERE v.book_id = b.id AND v.kind = 'content' AND v.model = sqlc.arg(model)::text)
  AND EXISTS (SELECT 1 FROM book_vectors v WHERE v.book_id = b.id AND v.kind = 'metadata' AND v.model = sqlc.arg(model)::text)
ORDER BY b.id
LIMIT 1;

-- name: PerfCollection :one
SELECT c.id::text AS id
FROM collections c
WHERE c.owner_id = sqlc.arg(viewer)::uuid OR c.visibility = 'shared'
ORDER BY (SELECT count(*) FROM collection_items i WHERE i.collection_id = c.id) DESC, c.id
LIMIT 1;

-- name: PerfSmartShelf :one
SELECT s.id::text AS id
FROM smart_shelves s
WHERE s.owner_id = sqlc.arg(viewer)::uuid OR s.visibility = 'shared'
ORDER BY s.id
LIMIT 1;

-- name: PerfTitleWords :many
-- The commonest words of four letters or more in the titles, and whether
-- the vocabulary of the texts holds them, for a quick search and, where no
-- text can be read, a full-text one.
SELECT t.word::text AS word, EXISTS (SELECT 1 FROM search_words s WHERE s.word = t.word) AS in_texts
FROM (
    SELECT w AS word, count(*) AS n
    FROM books b, unnest(string_to_array(b.title_key, ' ')) w
    WHERE b.deleted_at IS NULL AND NOT b.placeholder AND length(w) >= 4
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
    GROUP BY w
    ORDER BY n DESC, w
    LIMIT 40
) t
ORDER BY t.n DESC, t.word;

-- name: PerfSampleTextFiles :many
-- Files of books' primary texts that have chunks, spread by a hash of the
-- book, to take a common word and phrase from.
SELECT f.id
FROM books b
JOIN book_files f ON f.id = b.primary_text_file_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder AND f.chunked_at IS NOT NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY md5(b.id::text)
LIMIT sqlc.arg(n)::int;

-- name: ReadServerFile :one
-- A file of the database server's machine, which only a superuser may read:
-- the memory its container uses.
SELECT pg_read_file(sqlc.arg(path)::text)::text AS content;
