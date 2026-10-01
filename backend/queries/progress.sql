-- name: ListProgress :many
SELECT * FROM reading_progress WHERE user_id = $1 AND book_id = $2 ORDER BY medium;

-- name: LockProgress :one
SELECT * FROM reading_progress WHERE user_id = $1 AND book_id = $2 AND medium = $3 FOR UPDATE;

-- name: PutProgress :one
INSERT INTO reading_progress (user_id, book_id, medium, file_id, locator, fraction, chapter, page, position_ms, client_id, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, clock_timestamp())
ON CONFLICT (user_id, book_id, medium) DO UPDATE
SET file_id = EXCLUDED.file_id, locator = EXCLUDED.locator, fraction = EXCLUDED.fraction,
    chapter = EXCLUDED.chapter, page = EXCLUDED.page, position_ms = EXCLUDED.position_ms,
    client_id = EXCLUDED.client_id, updated_at = clock_timestamp()
RETURNING *;

-- name: ExtendSession :execrows
-- The client's session goes on if it wrote recently.
UPDATE reading_sessions
SET ended_at = now(), to_fraction = sqlc.arg(fraction)
WHERE id = (
    SELECT s.id FROM reading_sessions s
    WHERE s.user_id = sqlc.arg(user_id) AND s.book_id = sqlc.arg(book_id) AND s.medium = sqlc.arg(medium)
      AND s.client_id = sqlc.arg(client_id) AND s.ended_at > now() - make_interval(secs => sqlc.arg(gap_seconds)::double precision)
    ORDER BY s.ended_at DESC
    LIMIT 1
);

-- name: StartSession :exec
INSERT INTO reading_sessions (user_id, book_id, medium, client_id, from_fraction, to_fraction)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: AddFinish :exec
INSERT INTO reading_finishes (user_id, book_id, medium) VALUES ($1, $2, $3);

-- name: CountFinishes :one
SELECT count(*) FROM reading_finishes WHERE user_id = $1 AND book_id = $2;

-- name: FileOfBook :one
SELECT EXISTS (SELECT 1 FROM book_files WHERE id = $1 AND book_id = $2);
