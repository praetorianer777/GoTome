-- name: ListAnnotations :many
-- The viewer's own annotations of books they still see, newest first; of
-- one book, matching words of the passage, the note, the title or an
-- author, or all. The cursor is the created_at and id of the last row.
SELECT a.*, b.title AS book_title,
       coalesce((SELECT array_agg(au.name ORDER BY c.position, au.id)
                 FROM book_contributors c JOIN authors au ON au.id = c.author_id
                 WHERE c.book_id = b.id AND c.role = 'author'), '{}')::text[] AS book_authors
FROM annotations a
JOIN books b ON b.id = a.book_id
WHERE a.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND (sqlc.narg(book_id)::uuid IS NULL OR a.book_id = sqlc.narg(book_id)::uuid)
  AND (sqlc.narg(words)::text IS NULL OR NOT EXISTS (
        SELECT 1 FROM unnest(string_to_array(sqlc.narg(words)::text, ' ')) AS w
        WHERE w <> '' AND NOT (
              a.passage ILIKE '%' || w || '%' OR coalesce(a.note, '') ILIKE '%' || w || '%'
              OR b.title ILIKE '%' || w || '%'
              OR EXISTS (SELECT 1 FROM book_contributors c JOIN authors au ON au.id = c.author_id
                         WHERE c.book_id = b.id AND au.name ILIKE '%' || w || '%'))))
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (a.created_at, a.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows);

-- name: GetAnnotation :one
SELECT a.*
FROM annotations a
JOIN books b ON b.id = a.book_id
WHERE a.id = sqlc.arg(id) AND a.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
FOR UPDATE OF a;

-- name: CountBookAnnotations :one
SELECT count(*) FROM annotations WHERE user_id = $1 AND book_id = $2;

-- name: CreateAnnotation :one
INSERT INTO annotations (user_id, book_id, file_id, locator_kind, locator, locator_end, page, rects, passage, note,
                         color, chapter, fraction, client_id, client_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ChangeAnnotation :one
UPDATE annotations
SET note = sqlc.arg(note), color = sqlc.arg(color), edited_here = true, updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteAnnotation :exec
DELETE FROM annotations WHERE id = $1;

-- name: SyncAnnotation :exec
-- A device's annotation: new, or the same key again. What a person changed
-- here (note and colour) is kept, the rest follows the device.
INSERT INTO annotations AS a (user_id, book_id, file_id, locator_kind, locator, locator_end, page, rects, passage, note,
                              color, chapter, fraction, client_id, client_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (user_id, book_id, client_id, client_key) DO UPDATE SET
    file_id = EXCLUDED.file_id, locator = EXCLUDED.locator, locator_end = EXCLUDED.locator_end,
    page = EXCLUDED.page, rects = EXCLUDED.rects, passage = EXCLUDED.passage, chapter = EXCLUDED.chapter,
    fraction = EXCLUDED.fraction,
    note = CASE WHEN a.edited_here THEN a.note ELSE EXCLUDED.note END,
    color = CASE WHEN a.edited_here THEN a.color ELSE EXCLUDED.color END,
    updated_at = CASE WHEN (a.passage, a.locator, a.locator_end, a.note, a.color) IS NOT DISTINCT FROM
                           (EXCLUDED.passage, EXCLUDED.locator, EXCLUDED.locator_end, EXCLUDED.note, EXCLUDED.color)
                      THEN a.updated_at ELSE EXCLUDED.updated_at END;

-- name: DropAnnotationsNotSent :execrows
-- What a device no longer has: the annotations it sent before and left
-- out of the set it sent now.
DELETE FROM annotations
WHERE user_id = $1 AND book_id = $2 AND client_id = $3 AND NOT (client_key = ANY($4::text[]));
