-- name: ListBooksForGenres :many
-- Books after the given ID that a pass of the recipe has not looked at, or
-- that changed or were embedded again since it did, in ID order.
SELECT b.id
FROM books b
LEFT JOIN book_genre_checks c ON c.book_id = b.id
WHERE b.id > sqlc.arg(after)::uuid AND b.deleted_at IS NULL AND NOT b.placeholder
  AND (c.book_id IS NULL OR c.recipe <> sqlc.arg(recipe)::text OR c.checked_at < b.updated_at
       OR EXISTS (SELECT 1 FROM book_vectors v
                  WHERE v.book_id = b.id AND v.model = sqlc.arg(model)::text AND v.embedded_at > c.checked_at))
ORDER BY b.id
LIMIT sqlc.arg(page)::int;

-- name: ListGenreEvidence :many
-- What tells the genres of the books: their tags, and the genres a person
-- accepted or rejected.
SELECT b.id,
       ARRAY(SELECT t.name FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE bt.book_id = b.id ORDER BY t.name_key)::text[] AS tags,
       ARRAY(SELECT g.genre FROM book_genres g WHERE g.book_id = b.id AND g.state = 'accepted' ORDER BY g.genre)::text[] AS accepted,
       ARRAY(SELECT g.genre FROM book_genres g WHERE g.book_id = b.id AND g.state = 'rejected' ORDER BY g.genre)::text[] AS rejected
FROM books b
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: GetGenreVectors :many
-- A book's vectors of the model, and its library.
SELECT v.kind, v.embedding::text AS embedding, b.library_id
FROM book_vectors v
JOIN books b ON b.id = v.book_id
WHERE v.book_id = sqlc.arg(book_id)::uuid
  AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text;

-- name: ListGenreNeighbours :many
-- The books of the library nearest the vectors given, blended as similar
-- books are: the book itself, its duplicates and its relations left out.
-- It scans the vectors of one library, as the book page's similar books
-- scan those of the visible ones.
WITH near AS (
    SELECT v.book_id,
           max(1 - (v.embedding <=> sqlc.narg(content)::text::vector)) FILTER (WHERE v.kind = 'content') AS content,
           max(1 - (v.embedding <=> sqlc.narg(metadata)::text::vector)) FILTER (WHERE v.kind = 'metadata') AS metadata
    FROM book_vectors v
    JOIN books o ON o.id = v.book_id
    WHERE v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text
      AND o.library_id = sqlc.arg(library_id)::uuid AND v.book_id <> sqlc.arg(book_id)::uuid
      AND ((v.kind = 'content' AND sqlc.narg(content)::text IS NOT NULL)
        OR (v.kind = 'metadata' AND sqlc.narg(metadata)::text IS NOT NULL))
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
  AND NOT EXISTS (
      SELECT 1 FROM duplicate_pairs p
      WHERE (p.book_a, p.book_b) IN ((n.book_id, sqlc.arg(book_id)::uuid), (sqlc.arg(book_id)::uuid, n.book_id)))
  AND NOT EXISTS (
      SELECT 1 FROM book_relations r
      WHERE (r.book_a, r.book_b) IN ((n.book_id, sqlc.arg(book_id)::uuid), (sqlc.arg(book_id)::uuid, n.book_id)))
ORDER BY score DESC, n.book_id
LIMIT sqlc.arg(max)::int;

-- name: DropStaleGenres :exec
-- The book's undecided genres that are no longer found; what a person
-- decided stays.
DELETE FROM book_genres
WHERE book_id = sqlc.arg(book_id)::uuid AND state = 'suggested' AND NOT (genre = ANY(sqlc.arg(keep)::text[]));

-- name: PutTagGenre :exec
-- A genre of the book's tags, over an undecided suggestion of it.
INSERT INTO book_genres (book_id, genre, source)
VALUES (sqlc.arg(book_id)::uuid, sqlc.arg(genre)::text, 'tags')
ON CONFLICT (book_id, genre) DO UPDATE
SET source = 'tags', method = NULL, score = NULL, updated_at = now()
WHERE book_genres.state = 'suggested' AND book_genres.source <> 'tags';

-- name: PutAutomaticGenre :exec
-- A genre suggested by the book's vectors, unless a person decided it or
-- the tags give it.
INSERT INTO book_genres (book_id, genre, source, method, score)
VALUES (sqlc.arg(book_id)::uuid, sqlc.arg(genre)::text, 'automatic', sqlc.arg(method)::text, sqlc.arg(score)::real)
ON CONFLICT (book_id, genre) DO UPDATE
SET method = EXCLUDED.method, score = EXCLUDED.score, updated_at = now()
WHERE book_genres.state = 'suggested' AND book_genres.source = 'automatic';

-- name: MarkGenresChecked :exec
INSERT INTO book_genre_checks (book_id, recipe, checked_at)
VALUES (sqlc.arg(book_id)::uuid, sqlc.arg(recipe)::text, now())
ON CONFLICT (book_id) DO UPDATE SET recipe = EXCLUDED.recipe, checked_at = EXCLUDED.checked_at;

-- name: ListGenreSuggestions :many
-- Automatic suggestions nobody has decided, of books the viewer sees, of
-- one genre and one library when named: the best first, after the cursor.
SELECT g.book_id, g.genre, g.method, g.score::float8 AS score
FROM book_genres g
JOIN books b ON b.id = g.book_id
WHERE g.source = 'automatic' AND g.state = 'suggested'
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
  AND (sqlc.narg(genre)::text IS NULL OR g.genre = sqlc.narg(genre)::text)
  AND (sqlc.narg(after_score)::float8 IS NULL
       OR (g.score::float8, g.book_id, g.genre) < (sqlc.narg(after_score)::float8, sqlc.arg(after_book)::uuid, sqlc.arg(after_genre)::text))
ORDER BY g.score DESC, g.book_id DESC, g.genre DESC
LIMIT sqlc.arg(max)::int;

-- name: CountGenreSuggestions :many
-- How many automatic suggestions wait per genre, of the books the viewer
-- sees, of one library when named.
SELECT g.genre, count(*)::int AS waiting
FROM book_genres g
JOIN books b ON b.id = g.book_id
WHERE g.source = 'automatic' AND g.state = 'suggested'
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
  AND (sqlc.narg(library_id)::uuid IS NULL OR b.library_id = sqlc.narg(library_id)::uuid)
GROUP BY g.genre;

-- name: DecideGenre :execrows
-- A person accepts or rejects a genre a book they see has.
UPDATE book_genres g
SET state = sqlc.arg(state)::text, decided_by = sqlc.arg(viewer)::uuid, updated_at = now()
FROM books b
WHERE g.book_id = sqlc.arg(book_id)::uuid AND g.genre = sqlc.arg(genre)::text
  AND b.id = g.book_id AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: ListGenreCheckBooks :many
-- Every book a genre-check may count, with whether it has each vector of
-- the model; the vectors themselves are read only for the sample.
SELECT b.id, b.library_id,
       EXISTS (SELECT 1 FROM book_vectors v WHERE v.book_id = b.id AND v.kind = 'content'
                 AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text) AS has_content,
       EXISTS (SELECT 1 FROM book_vectors v WHERE v.book_id = b.id AND v.kind = 'metadata'
                 AND v.model = sqlc.arg(model)::text AND v.model_version = sqlc.arg(model_version)::text) AS has_metadata
FROM books b
WHERE b.deleted_at IS NULL AND NOT b.placeholder;

-- name: ListAllBookTags :many
-- Every tag of every book a genre-check counts, a row each.
SELECT bt.book_id, t.name
FROM book_tags bt
JOIN tags t ON t.id = bt.tag_id
JOIN books b ON b.id = bt.book_id
WHERE b.deleted_at IS NULL AND NOT b.placeholder;

-- name: GetUntaggedMetadataText :one
-- What the book's metadata vector is made from, without its tags.
SELECT coalesce(book_metadata_text(sqlc.arg(book_id)::uuid, false), '')::text;
