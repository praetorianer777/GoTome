-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY created_at, id;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = $1;

-- name: CountWebhooks :one
SELECT count(*) FROM webhooks;

-- name: CreateWebhook :one
INSERT INTO webhooks (id, name, url, kinds, secret_sealed, trusted, enabled, created_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(url), sqlc.arg(kinds)::text[], sqlc.arg(secret_sealed), sqlc.arg(trusted), sqlc.arg(enabled), sqlc.arg(created_by))
RETURNING *;

-- name: UpdateWebhook :one
UPDATE webhooks SET name = sqlc.arg(name), url = sqlc.arg(url), kinds = sqlc.arg(kinds)::text[],
    trusted = sqlc.arg(trusted), enabled = sqlc.arg(enabled),
    secret_sealed = COALESCE(sqlc.narg(secret_sealed)::bytea, secret_sealed), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteWebhook :execrows
DELETE FROM webhooks WHERE id = $1;

-- name: ListWebhooksForKind :many
SELECT id FROM webhooks WHERE enabled AND (cardinality(kinds) = 0 OR sqlc.arg(kind)::text = ANY (kinds));

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (hook_id, kind, payload) VALUES ($1, $2, $3) RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_deliveries WHERE hook_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: RecordWebhookAttempt :exec
UPDATE webhook_deliveries SET attempts = attempts + 1, updated_at = now(),
    status = sqlc.arg(status)::text, last_error = sqlc.narg(last_error)::text,
    response_code = sqlc.narg(response_code)::integer,
    delivered_at = CASE WHEN sqlc.arg(status)::text = 'delivered' THEN now() ELSE delivered_at END
WHERE id = sqlc.arg(id);

-- name: WebhookLastDeliveries :many
SELECT DISTINCT ON (hook_id) hook_id, status, created_at, response_code
FROM webhook_deliveries ORDER BY hook_id, created_at DESC, id DESC;

-- name: TrimWebhookDeliveries :execrows
DELETE FROM webhook_deliveries d
WHERE d.created_at < sqlc.arg(before)::timestamptz
    OR (d.status <> 'pending' AND d.id IN (
        SELECT x.id FROM (
            SELECT id, row_number() OVER (PARTITION BY hook_id ORDER BY created_at DESC, id DESC) AS n
            FROM webhook_deliveries) x
        WHERE x.n > sqlc.arg(keep)::int));
