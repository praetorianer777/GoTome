-- +goose Up
-- An API key is a bearer token for scripts and third-party clients (#417):
-- its owner's account, narrowed to the scopes it was made with.
CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (btrim(name) <> ''),
    -- SHA-256 of the key: a copy of this table signs nobody in.
    key_hash     bytea NOT NULL UNIQUE,
    -- The key's first characters, so a person can tell their keys apart.
    key_prefix   text NOT NULL,
    scopes       text[] NOT NULL CHECK (cardinality(scopes) > 0),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz
);

CREATE INDEX api_keys_user_id_idx ON api_keys (user_id);

-- +goose Down
DROP TABLE api_keys;
