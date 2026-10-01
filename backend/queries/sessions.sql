-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, user_agent, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetSessionUser :one
-- The user a live session belongs to. A disabled user has no live sessions.
SELECT u.*, s.last_seen_at AS session_last_seen_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > sqlc.arg(now)::timestamptz
  AND u.disabled_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = sqlc.arg(now)::timestamptz, expires_at = $2
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now)::timestamptz;

-- name: ListUserSessions :many
SELECT id, token_hash, user_agent, created_at, last_seen_at, expires_at
FROM sessions
WHERE user_id = $1 AND expires_at > sqlc.arg(now)::timestamptz
ORDER BY last_seen_at DESC;

-- name: DeleteUserSession :execrows
DELETE FROM sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteUserSessions :exec
-- Every session of the user but the one kept, which may be none.
DELETE FROM sessions
WHERE user_id = $1 AND token_hash IS DISTINCT FROM sqlc.narg(keep)::bytea;
