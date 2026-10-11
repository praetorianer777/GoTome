-- name: CreateDevice :one
INSERT INTO devices (user_id, name, token_hash, sync_key_hash)
VALUES ($1, $2, $3, $4)
RETURNING id, name, created_at, last_used_at;

-- name: ListUserDevices :many
SELECT id, name, created_at, last_used_at
FROM devices
WHERE user_id = $1
ORDER BY created_at;

-- name: DeleteUserDevice :execrows
DELETE FROM devices WHERE id = $1 AND user_id = $2;

-- name: GetDeviceUser :one
-- The account a device token signs in, under its user name. A disabled
-- account's devices sign nobody in.
SELECT u.*, d.id AS device_id, d.last_used_at AS device_last_used_at
FROM devices d
JOIN users u ON u.id = d.user_id
WHERE d.token_hash = $1
  AND lower(u.username) = lower(sqlc.arg(username)::text)
  AND u.disabled_at IS NULL;

-- name: TouchDevice :exec
UPDATE devices SET last_used_at = sqlc.arg(now)::timestamptz WHERE id = $1;
