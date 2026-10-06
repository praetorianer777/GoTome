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

-- name: DailyActivity :many
-- What a person read and listened to per day of their time zone since a
-- time, of books they may still see. Pages are the share of the book a
-- session covered times its page count: its main text file's, or the
-- book's; estimated says the file's was worked out from its text.
SELECT (s.started_at AT TIME ZONE sqlc.arg(tz)::text)::date AS day,
       s.medium,
       sum(extract(epoch FROM s.ended_at - s.started_at))::bigint AS seconds,
       sum(CASE WHEN s.medium = 'ebook'
                THEN greatest(s.to_fraction - s.from_fraction, 0) * COALESCE(f.page_count, b.page_count, 0)
                ELSE 0 END)::double precision AS pages,
       bool_or(s.medium = 'ebook' AND f.page_count IS NOT NULL AND f.pages_estimated) AS estimated
FROM reading_sessions s
JOIN books b ON b.id = s.book_id
LEFT JOIN book_files f ON f.id = b.primary_text_file_id
WHERE s.user_id = sqlc.arg(user_id)
  AND s.started_at >= sqlc.arg(since)
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(user_id)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: ActivityTotals :many
-- The same, over all time, per medium.
SELECT s.medium,
       sum(extract(epoch FROM s.ended_at - s.started_at))::bigint AS seconds,
       sum(CASE WHEN s.medium = 'ebook'
                THEN greatest(s.to_fraction - s.from_fraction, 0) * COALESCE(f.page_count, b.page_count, 0)
                ELSE 0 END)::double precision AS pages,
       bool_or(s.medium = 'ebook' AND f.page_count IS NOT NULL AND f.pages_estimated) AS estimated
FROM reading_sessions s
JOIN books b ON b.id = s.book_id
LEFT JOIN book_files f ON f.id = b.primary_text_file_id
WHERE s.user_id = sqlc.arg(user_id)
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(user_id)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY s.medium
ORDER BY s.medium;

-- name: FinishesByYear :many
-- Every time a book was read to its end counts, so a book read twice in a
-- year counts twice there; books is how many different ones.
SELECT extract(year FROM rf.finished_at AT TIME ZONE sqlc.arg(tz)::text)::int AS year,
       count(*) AS finishes,
       count(DISTINCT rf.book_id) AS books
FROM reading_finishes rf
JOIN books b ON b.id = rf.book_id
WHERE rf.user_id = sqlc.arg(user_id)
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(user_id)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY 1
ORDER BY 1 DESC;

-- name: ReadingHistory :many
-- When a person began and finished books, the latest first.
SELECT h.book_id, b.title, h.event, h.day
FROM (
    SELECT ub.book_id, 'started' AS event, ub.started_on AS day
    FROM user_books ub
    WHERE ub.user_id = sqlc.arg(user_id) AND ub.started_on IS NOT NULL
  UNION ALL
    SELECT rf.book_id, 'finished', (rf.finished_at AT TIME ZONE sqlc.arg(tz)::text)::date
    FROM reading_finishes rf
    WHERE rf.user_id = sqlc.arg(user_id)
) h
JOIN books b ON b.id = h.book_id
WHERE b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(user_id)::uuid, sqlc.arg(sees_all)::boolean))
ORDER BY h.day DESC, h.event DESC, b.title
LIMIT sqlc.arg(max_rows);
