-- name: ServerVersion :one
-- Which Postgres answered, for the line the server logs once it is connected.
SELECT current_setting('server_version')::text AS server_version;
