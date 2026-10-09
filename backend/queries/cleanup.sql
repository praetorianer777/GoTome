-- name: ListCleanupCredits :many
-- The author credits of the books the viewer sees whose author is one of
-- the people (person_keys) or names (name_keys) a clean-up is about, with
-- the key of the book's series.
SELECT c.book_id, a.name, a.name_key, a.person_key, COALESCE(s.name_key, '')::text AS series_key
FROM book_contributors c
JOIN authors a ON a.id = c.author_id
JOIN books b ON b.id = c.book_id
LEFT JOIN series s ON s.id = b.series_id
WHERE c.role = 'author' AND b.deleted_at IS NULL
  AND (a.person_key = ANY(sqlc.arg(person_keys)::text[]) OR a.name_key = ANY(sqlc.arg(name_keys)::text[]))
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY b.sort_title, b.id, c.position;

-- name: DismissCleanup :exec
INSERT INTO cleanup_dismissals (kind, subject, value, dismissed_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (kind, subject) DO UPDATE
SET value = EXCLUDED.value, dismissed_by = EXCLUDED.dismissed_by, dismissed_at = now();
