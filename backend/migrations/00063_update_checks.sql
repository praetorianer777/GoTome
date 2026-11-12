-- +goose Up
-- The one row of what GitHub last said about the latest release.
CREATE TABLE update_checks (
    id               boolean PRIMARY KEY DEFAULT true CHECK (id),
    latest_version   text,
    url              text,
    published_at     timestamptz,
    security         boolean NOT NULL DEFAULT false,
    checked_at       timestamptz,
    attempted_at     timestamptz,
    error            text,
    notified_version text
);

-- +goose Down
DROP TABLE update_checks;
