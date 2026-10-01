-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (username, email, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: LockSetup :exec
-- Serialises first-run setup: two requests arriving together must not both
-- find the table empty.
SELECT pg_advisory_xact_lock(7311501);

-- name: ListUsers :many
SELECT * FROM users ORDER BY lower(username);

-- name: ListSessionUse :many
-- When each account that has sessions last used one, and how many are live.
SELECT user_id, max(last_seen_at)::timestamptz AS last_seen_at,
       count(*) FILTER (WHERE expires_at > sqlc.arg(now)::timestamptz) AS live
FROM sessions
GROUP BY user_id;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: LockActiveAdmins :many
-- Locked, so that two administrators demoting each other at once cannot
-- both find another one left.
SELECT id FROM users
WHERE role = 'admin' AND disabled_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: UpdateUser :one
UPDATE users
SET role = $2, email = $3, disabled_at = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;
