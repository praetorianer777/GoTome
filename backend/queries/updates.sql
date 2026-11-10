-- name: GetUpdateCheck :one
SELECT * FROM update_checks WHERE id;

-- name: EnsureUpdateCheck :exec
INSERT INTO update_checks (id) VALUES (true) ON CONFLICT DO NOTHING;

-- name: LockUpdateCheck :one
-- The row, held while an answer is recorded.
SELECT * FROM update_checks WHERE id FOR UPDATE;

-- name: RecordUpdateAnswer :exec
UPDATE update_checks SET latest_version = sqlc.arg(latest_version)::text, url = sqlc.arg(url)::text,
    published_at = sqlc.arg(published_at)::timestamptz, security = sqlc.arg(security)::boolean,
    checked_at = sqlc.arg(now)::timestamptz, attempted_at = sqlc.arg(now)::timestamptz, error = NULL
WHERE id;

-- name: RecordUpdateFailure :exec
UPDATE update_checks SET attempted_at = sqlc.arg(now)::timestamptz, error = sqlc.arg(error)::text WHERE id;

-- name: MarkUpdateNotified :exec
UPDATE update_checks SET notified_version = sqlc.arg(version)::text WHERE id;
