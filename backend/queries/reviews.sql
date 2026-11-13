-- name: GetShareReviews :one
SELECT share_reviews FROM users WHERE id = $1;

-- name: SetShareReviews :exec
UPDATE users SET share_reviews = $2, updated_at = now() WHERE id = $1;

-- name: ReviewableBook :one
-- Whether the viewer may see the book.
SELECT EXISTS (
    SELECT 1 FROM books b
    WHERE b.id = sqlc.arg(book_id) AND b.deleted_at IS NULL
      AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))));

-- name: GetBookReview :one
SELECT body, private, created_at, updated_at FROM book_reviews WHERE user_id = $1 AND book_id = $2;

-- name: PutBookReview :exec
INSERT INTO book_reviews (user_id, book_id, body, private) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, book_id) DO UPDATE SET body = EXCLUDED.body, private = EXCLUDED.private, updated_at = now();

-- name: DeleteBookReview :exec
DELETE FROM book_reviews WHERE user_id = $1 AND book_id = $2;

-- name: SharedStars :one
-- The mean of the stars people share on a book and how many gave them: those
-- who share, whose account is on, and who did not keep a review of the book
-- private. Nothing for a book the viewer may not see.
SELECT count(*)::int AS stars, coalesce(avg(ub.rating), 0)::float8 AS average
FROM user_books ub
JOIN users u ON u.id = ub.user_id
WHERE ub.book_id = sqlc.arg(book_id) AND ub.rating IS NOT NULL
  AND u.share_reviews AND u.disabled_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM book_reviews r WHERE r.user_id = ub.user_id AND r.book_id = ub.book_id AND r.private)
  AND ub.book_id IN (
      SELECT b.id FROM books b
      WHERE b.deleted_at IS NULL
        AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))));

-- name: ListSharedReviews :many
-- The reviews others share on a book the viewer may see, the latest first.
SELECT u.username, r.body, ub.rating, r.created_at, r.updated_at
FROM book_reviews r
JOIN users u ON u.id = r.user_id
LEFT JOIN user_books ub ON ub.user_id = r.user_id AND ub.book_id = r.book_id
WHERE r.book_id = sqlc.arg(book_id) AND r.user_id <> sqlc.arg(viewer)::uuid
  AND NOT r.private AND u.share_reviews AND u.disabled_at IS NULL
  AND r.book_id IN (
      SELECT b.id FROM books b
      WHERE b.deleted_at IS NULL
        AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))))
ORDER BY r.updated_at DESC, r.user_id
LIMIT 100;

-- name: ListPersonReviews :many
-- The reviews a person shares on books the viewer may see, the latest
-- first. A person who shares nothing, or has no such account, gives no rows.
SELECT b.id AS book_id, b.title, b.author_sort, r.body, ub.rating, r.created_at, r.updated_at
FROM users u
JOIN book_reviews r ON r.user_id = u.id
JOIN books b ON b.id = r.book_id
LEFT JOIN user_books ub ON ub.user_id = r.user_id AND ub.book_id = r.book_id
WHERE u.username = sqlc.arg(username)::citext
  AND NOT r.private AND u.share_reviews AND u.disabled_at IS NULL
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)) AND (b.age_rating IS NULL OR b.age_rating <= (SELECT age_limit(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean)))
ORDER BY r.updated_at DESC, r.book_id
LIMIT 200;
