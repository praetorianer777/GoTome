-- name: ListPlayQueue :many
-- The viewer's queue, the books they may still see, in their order.
SELECT q.book_id
FROM play_queue q
JOIN books b ON b.id = q.book_id
WHERE q.user_id = sqlc.arg(viewer)::uuid
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ORDER BY q.position, q.book_id;

-- name: ClearPlayQueue :exec
DELETE FROM play_queue WHERE user_id = $1;

-- name: AddToPlayQueue :exec
-- One book at a place, if the viewer may see it and it has audio to play.
INSERT INTO play_queue (user_id, book_id, position)
SELECT sqlc.arg(viewer)::uuid, b.id, sqlc.arg(position)::int
FROM books b
WHERE b.id = sqlc.arg(book)::uuid
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
  AND EXISTS (SELECT 1 FROM book_files f
              WHERE f.book_id = b.id AND f.kind = 'audio' AND f.trashed_at IS NULL AND f.missing_at IS NULL)
ON CONFLICT DO NOTHING;
