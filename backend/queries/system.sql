-- name: ServerVersion :one
-- Which Postgres answered, for the line the server logs once it is connected.
SELECT current_setting('server_version')::text AS server_version;

-- name: PostgresSettings :many
-- Settings by name, in bytes where they are sizes (whose unit is 8kB, kB
-- or MB), as they are otherwise.
SELECT name::text AS name,
       (setting::bigint * CASE
            WHEN unit IS NULL THEN 1
            WHEN unit ~ '^[0-9]' THEN pg_size_bytes(unit)
            ELSE pg_size_bytes('1' || unit)
        END)::bigint AS value
FROM pg_catalog.pg_settings
WHERE name = ANY(@names::text[]);

-- name: DatabaseSize :one
SELECT pg_database_size(current_database())::bigint AS bytes;
