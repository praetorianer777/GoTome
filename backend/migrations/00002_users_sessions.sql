-- +goose Up
-- People who sign in, and the sessions they sign in with.

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    -- citext: "Steve" and "steve" are the same person.
    username      citext NOT NULL UNIQUE,
    email         citext UNIQUE,
    -- NULL for an account that only signs in through an identity provider.
    password_hash text,
    role          text NOT NULL CHECK (role IN ('admin', 'editor', 'reader')),
    -- How much the user may upload; NULL is unlimited.
    quota_bytes   bigint CHECK (quota_bytes >= 0),
    disabled_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    -- SHA-256 of the token in the cookie: a copy of this table signs nobody in.
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
