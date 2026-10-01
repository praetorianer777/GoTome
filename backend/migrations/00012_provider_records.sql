-- +goose Up
-- What metadata providers answered, so that asking again within a while
-- costs them nothing. The request is kept without its secrets: an API key
-- is never part of the key or the URL stored here.
CREATE TABLE provider_records (
    provider    text NOT NULL,
    -- SHA-256 of the method, URL and body as sent, without secrets.
    request_key bytea NOT NULL CHECK (octet_length(request_key) = 32),
    url         text NOT NULL,
    -- 200, or 404 for "the provider knows no such thing", which is worth
    -- remembering too.
    status      integer NOT NULL CHECK (status IN (200, 404)),
    body        bytea NOT NULL,
    fetched_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (provider, request_key)
);

CREATE INDEX provider_records_expires_idx ON provider_records (expires_at);

-- +goose Down
DROP TABLE provider_records;
