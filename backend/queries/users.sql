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
