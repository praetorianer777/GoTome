-- name: CreateAPIKey :one
-- Refuses (no row) once the user holds the most keys they may.
INSERT INTO api_keys (user_id, name, key_hash, key_prefix, scopes, expires_at)
SELECT sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(key_hash), sqlc.arg(key_prefix), sqlc.arg(scopes)::text[], sqlc.narg(expires_at)::timestamptz
WHERE (SELECT count(*) FROM api_keys WHERE user_id = sqlc.arg(user_id)) < sqlc.arg(max_keys)::int
RETURNING id, name, key_prefix, scopes, created_at, last_used_at, expires_at;

-- name: ListUserAPIKeys :many
SELECT id, name, key_prefix, scopes, created_at, last_used_at, expires_at
FROM api_keys
WHERE user_id = $1
ORDER BY created_at;

-- name: DeleteUserAPIKey :execrows
DELETE FROM api_keys WHERE id = $1 AND user_id = $2;

-- name: GetAPIKeyUser :one
-- The account a key signs in, unless the key has expired or the account is
-- disabled.
SELECT u.*, k.id AS key_id, k.scopes AS key_scopes, k.last_used_at AS key_last_used_at
FROM api_keys k
JOIN users u ON u.id = k.user_id
WHERE k.key_hash = $1
  AND (k.expires_at IS NULL OR k.expires_at > sqlc.arg(now)::timestamptz)
  AND u.disabled_at IS NULL;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = sqlc.arg(now)::timestamptz WHERE id = $1;
