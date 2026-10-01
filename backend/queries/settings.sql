-- name: ListSettings :many
SELECT * FROM settings;

-- name: GetSetting :one
SELECT * FROM settings WHERE key = $1;

-- name: PutSetting :exec
INSERT INTO settings (key, value, sealed, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, sealed = EXCLUDED.sealed,
    updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = $1;

-- name: AddKeyCheck :exec
-- The first start under a key leaves this behind; every later one opens it.
INSERT INTO settings (key, sealed) VALUES ($1, $2)
ON CONFLICT (key) DO NOTHING;
