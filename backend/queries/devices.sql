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

-- name: GetSyncKeyUser :one
-- The account KOReader's progress sync signs in with a device token's MD5,
-- under its user name. A disabled account's devices sign nobody in.
SELECT u.*, d.id AS device_id
FROM devices d
JOIN users u ON u.id = d.user_id
WHERE d.sync_key_hash = $1
  AND lower(u.username) = lower(sqlc.arg(username)::text)
  AND u.disabled_at IS NULL;

-- name: GetActiveUserIDByName :one
SELECT id FROM users WHERE lower(username) = lower(sqlc.arg(username)::text) AND disabled_at IS NULL;

-- name: CountWaitingDeviceRequests :one
SELECT count(*)::int FROM device_requests
WHERE user_id = $1 AND state = 'pending' AND expires_at > now();

-- name: DropOldDeviceRequests :exec
-- Requests a day past their end, which nobody can use any more.
DELETE FROM device_requests WHERE expires_at < now() - interval '1 day';

-- name: CreateDeviceRequest :one
INSERT INTO device_requests (user_id, device_name, code, secret_hash, address, expires_at)
VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => sqlc.arg(seconds)::int))
RETURNING id, expires_at;

-- name: ListWaitingDeviceRequests :many
SELECT id, device_name, code, address, created_at, expires_at FROM device_requests
WHERE user_id = $1 AND state = 'pending' AND expires_at > now()
ORDER BY created_at;

-- name: DecideDeviceRequest :execrows
-- Accepts or declines a request of the user's that still waits.
UPDATE device_requests SET state = sqlc.arg(state)
WHERE id = $1 AND user_id = $2 AND state = 'pending' AND expires_at > now();

-- name: TakeDeviceRequest :one
-- The request the app asks about with its secret, locked while it is
-- answered, and whether its time is up.
SELECT id, user_id, device_name, state, (expires_at <= now())::boolean AS expired
FROM device_requests WHERE id = $1 AND secret_hash = $2
FOR UPDATE;

-- name: SetDeviceRequestState :exec
UPDATE device_requests SET state = $2 WHERE id = $1;
