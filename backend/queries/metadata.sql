-- name: GetProviderRecord :one
SELECT status, body
FROM provider_records
WHERE provider = $1 AND request_key = $2 AND expires_at > now();

-- name: PutProviderRecord :exec
INSERT INTO provider_records (provider, request_key, url, status, body, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, request_key) DO UPDATE
SET url = EXCLUDED.url, status = EXCLUDED.status, body = EXCLUDED.body,
    fetched_at = now(), expires_at = EXCLUDED.expires_at;


-- name: RecordMatch :one
-- A match found again keeps what became of it: a dismissed one is not
-- brought back, an applied one not made pending. inserted is false for a
-- match that was there already.
INSERT INTO metadata_matches (book_id, provider, record_id, score, record, state)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (book_id, provider, record_id) DO UPDATE
SET score = EXCLUDED.score, record = EXCLUDED.record, updated_at = now(),
    state = CASE WHEN metadata_matches.state = 'pending' THEN EXCLUDED.state ELSE metadata_matches.state END
RETURNING (xmax = 0)::boolean AS inserted;

-- name: ListReviewBooks :many
-- The books with matches waiting, those waiting longest first, of the
-- libraries the viewer may see.
SELECT b.id
FROM metadata_matches m
JOIN books b ON b.id = m.book_id
WHERE m.state = 'pending'
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean))
GROUP BY b.id
ORDER BY min(m.created_at), b.id
LIMIT sqlc.arg(max_books);

-- name: CountReviewBooks :one
SELECT count(DISTINCT b.id)
FROM metadata_matches m
JOIN books b ON b.id = m.book_id
WHERE m.state = 'pending'
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: ListPendingMatches :many
SELECT * FROM metadata_matches
WHERE book_id = ANY(sqlc.arg(book_ids)::uuid[]) AND state = 'pending'
ORDER BY book_id, score DESC, id;

-- name: GetVisibleMatch :one
SELECT m.*
FROM metadata_matches m
JOIN books b ON b.id = m.book_id
WHERE m.id = sqlc.arg(id)
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids(sqlc.arg(viewer)::uuid, sqlc.arg(sees_all)::boolean));

-- name: SetMatchState :exec
UPDATE metadata_matches SET state = $2, updated_at = now() WHERE id = $1;

-- name: DismissOtherMatches :exec
-- Once one match of a book is taken, the others it waited with are done.
UPDATE metadata_matches SET state = 'dismissed', updated_at = now()
WHERE book_id = $1 AND id <> $2 AND state = 'pending';
