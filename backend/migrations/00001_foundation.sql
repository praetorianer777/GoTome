-- +goose Up
-- Foundation: what every later migration may rely on.

-- IDs come from uuidv7(), which Postgres has built in from version 18 on.
-- +goose StatementBegin
DO $$
BEGIN
    IF current_setting('server_version_num')::int < 180000 THEN
        RAISE EXCEPTION 'GOtome needs PostgreSQL 18 or newer, and this server runs %. Upgrade it and start again.',
            current_setting('server_version');
    END IF;
END;
$$;
-- +goose StatementEnd

-- Case-insensitive text, for names people type: user names, e-mail addresses.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
