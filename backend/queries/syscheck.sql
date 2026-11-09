-- name: OutboxLag :one
-- How many changes the full-text index is still to get, and since when the
-- oldest has waited (now when none does).
SELECT (SELECT count(*) FROM fulltext_outbox)::bigint AS pending,
       coalesce((SELECT created_at FROM fulltext_outbox ORDER BY id LIMIT 1), now())::timestamptz AS oldest;

-- name: CountLibrariesWithFailedScan :one
-- Libraries whose last finished scan failed.
SELECT count(*)::int FROM (
    SELECT DISTINCT ON (library_id) state
    FROM library_scans
    WHERE state IN ('done', 'failed')
    ORDER BY library_id, requested_at DESC, id DESC
) last
WHERE state = 'failed';
