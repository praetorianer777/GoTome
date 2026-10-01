-- +goose Up
-- A session's own ID, by which its owner sees and ends it. The token's hash
-- stays the key it is looked up by, and is never shown.
ALTER TABLE sessions ADD COLUMN id uuid NOT NULL DEFAULT uuidv7();
ALTER TABLE sessions ADD CONSTRAINT sessions_id_key UNIQUE (id);

-- +goose Down
ALTER TABLE sessions DROP COLUMN id;
