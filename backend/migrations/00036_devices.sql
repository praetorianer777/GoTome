-- +goose Up
-- A device is an e-reader a person signs in with a token of its own (#195):
-- an app password that OPDS clients and KOReader send as HTTP Basic auth,
-- and which reaches only the routes for devices.
CREATE TABLE devices (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (btrim(name) <> ''),
    -- SHA-256 of the token: a copy of this table signs nobody in.
    token_hash   bytea NOT NULL UNIQUE,
    -- SHA-256 of the token's MD5 in hex, which is what KOReader's progress
    -- sync sends in place of the password (#198).
    sync_key_hash bytea NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE INDEX devices_user_id_idx ON devices (user_id);

-- The catalogue's authors and series, in the order it lists them.
CREATE INDEX authors_sort_name_idx ON authors (sort_name, id);
CREATE INDEX series_name_idx ON series (name_key, id);

-- +goose Down
DROP INDEX series_name_idx;
DROP INDEX authors_sort_name_idx;
DROP TABLE devices;
