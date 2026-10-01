-- +goose Up
-- Settings an administrator changes while GOtome runs. A plain value is kept
-- as text; a secret only sealed, under the key in GOTOME_SECRET_KEY_FILE,
-- which lives outside the database (internal/secret).
CREATE TABLE settings (
    key        text PRIMARY KEY CHECK (key <> ''),
    value      text,
    sealed     bytea,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    CHECK ((value IS NULL) <> (sealed IS NULL))
);

-- +goose Down
DROP TABLE settings;
