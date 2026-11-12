-- +goose Up
-- An e-reader asking to be added to an account (#218): its app sends the user
-- name, the person accepts or declines on their profile, and the app then
-- collects the token of a new device with the secret only it holds.
CREATE TABLE device_requests (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    -- NULL when the name asked for is nobody's, or its account has as many
    -- requests waiting as it may: such a request answers the app like any
    -- other and is shown to no one.
    user_id     uuid REFERENCES users (id) ON DELETE CASCADE,
    device_name text NOT NULL,
    -- What the app shows, for the person to compare before accepting.
    code        text NOT NULL,
    -- SHA-256 of the secret the app polls with.
    secret_hash bytea NOT NULL UNIQUE,
    -- The address the request came from, shown beside it.
    address     text NOT NULL,
    state       text NOT NULL DEFAULT 'pending'
                CHECK (state IN ('pending', 'accepted', 'declined', 'collected')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);

CREATE INDEX device_requests_user_id_idx ON device_requests (user_id, created_at) WHERE user_id IS NOT NULL;
CREATE INDEX device_requests_expires_at_idx ON device_requests (expires_at);

-- +goose Down
DROP TABLE device_requests;
