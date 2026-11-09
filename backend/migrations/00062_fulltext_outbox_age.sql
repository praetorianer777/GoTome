-- +goose Up
-- When the change was recorded, to tell how far behind the index is.
ALTER TABLE fulltext_outbox ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE fulltext_outbox DROP COLUMN created_at;
