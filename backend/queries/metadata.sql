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

