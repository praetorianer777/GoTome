-- name: PlaceholderFacts :one
-- What a placeholder is matched by: its title key, its ISBNs and its
-- authors' person keys.
SELECT b.title_key,
       COALESCE((SELECT array_agg(i.value) FROM book_identifiers i
                 WHERE i.book_id = b.id AND i.type = 'isbn'), '{}')::text[] AS isbns,
       COALESCE((SELECT array_agg(DISTINCT a.person_key) FROM book_contributors c JOIN authors a ON a.id = c.author_id
                 WHERE c.book_id = b.id AND c.role = 'author' AND a.person_key <> ''), '{}')::text[] AS person_keys
FROM books b
WHERE b.id = $1 AND b.placeholder AND b.deleted_at IS NULL;

-- name: ListPlaceholdersOfBook :many
-- The placeholders, in any library, that the book may be: one that shares
-- an ISBN with it, or has its title and an author of it.
SELECT p.id
FROM books b
JOIN books p ON p.placeholder AND p.deleted_at IS NULL AND p.id <> b.id
WHERE b.id = $1 AND NOT b.placeholder AND b.deleted_at IS NULL
  AND (
    EXISTS (SELECT 1 FROM book_identifiers bi
            JOIN book_identifiers pi ON pi.type = 'isbn' AND pi.value = bi.value
            WHERE bi.book_id = b.id AND bi.type = 'isbn' AND pi.book_id = p.id)
    OR (p.title_key = b.title_key
        AND EXISTS (SELECT 1 FROM book_contributors pc JOIN authors pa ON pa.id = pc.author_id
                    JOIN authors ba ON ba.person_key = pa.person_key AND ba.person_key <> ''
                    JOIN book_contributors bc ON bc.author_id = ba.id AND bc.role = 'author'
                    WHERE pc.book_id = p.id AND pc.role = 'author' AND bc.book_id = b.id))
  )
ORDER BY p.created_at, p.id;

-- name: ListPlaceholderHolders :many
-- Everyone with something of their own on the book: their status, place,
-- note or a collection it is on.
SELECT u.id, u.role
FROM users u
WHERE u.id IN (
    SELECT x.user_id FROM user_books x WHERE x.book_id = sqlc.arg(book_id)
    UNION SELECT x.user_id FROM reading_progress x WHERE x.book_id = sqlc.arg(book_id)
    UNION SELECT x.user_id FROM book_notes x WHERE x.book_id = sqlc.arg(book_id)
    UNION SELECT x.user_id FROM book_reviews x WHERE x.book_id = sqlc.arg(book_id)
    UNION SELECT c.owner_id FROM collection_items i JOIN collections c ON c.id = i.collection_id WHERE i.book_id = sqlc.arg(book_id))
ORDER BY u.id;

-- name: MaybeImportBooks :many
-- The books of libraries the viewer sees, not placeholders, that an
-- imported row may be though its title is not theirs: one an author of the
-- row wrote, with a title that is similar. The most similar first.
SELECT b.id
FROM authors a
JOIN book_contributors c ON c.author_id = a.id AND c.role = 'author'
JOIN books b ON b.id = c.book_id
WHERE a.person_key = ANY(sqlc.arg(person_keys)::text[])
  AND b.deleted_at IS NULL AND NOT b.placeholder
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND similarity(b.title_key, sqlc.arg(title_key)::text) >= sqlc.arg(min_similarity)::real
GROUP BY b.id, b.title_key, b.created_at
ORDER BY max(similarity(b.title_key, sqlc.arg(title_key)::text)) DESC, b.created_at, b.id
LIMIT 5;

-- name: IsBookHolder :one
SELECT EXISTS (SELECT 1 FROM user_books x WHERE x.book_id = sqlc.arg(book_id) AND x.user_id = sqlc.arg(user_id))
    OR EXISTS (SELECT 1 FROM reading_progress x WHERE x.book_id = sqlc.arg(book_id) AND x.user_id = sqlc.arg(user_id))
    OR EXISTS (SELECT 1 FROM book_notes x WHERE x.book_id = sqlc.arg(book_id) AND x.user_id = sqlc.arg(user_id))
    OR EXISTS (SELECT 1 FROM book_reviews x WHERE x.book_id = sqlc.arg(book_id) AND x.user_id = sqlc.arg(user_id))
    OR EXISTS (SELECT 1 FROM collection_items i JOIN collections c ON c.id = i.collection_id
               WHERE i.book_id = sqlc.arg(book_id) AND c.owner_id = sqlc.arg(user_id));

-- name: GetPlaceholderJoinAction :one
-- An action of a placeholder join that the person made themselves.
SELECT a.id FROM curation_actions a JOIN curation_runs r ON r.id = a.run_id
WHERE a.id = sqlc.arg(id) AND r.kind = 'placeholders' AND r.started_by = sqlc.arg(user_id)::uuid;

-- name: LatestPlaceholderJoinAction :one
-- The last action of a placeholder join the person made of that book.
SELECT a.id FROM curation_actions a JOIN curation_runs r ON r.id = a.run_id
WHERE r.kind = 'placeholders' AND r.started_by = sqlc.arg(user_id)::uuid AND sqlc.arg(from_book)::uuid = ANY(a.books)
ORDER BY a.created_at DESC, a.id DESC
LIMIT 1;
