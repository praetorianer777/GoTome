-- +goose Up
-- A library may be left out of full-text search (#266): its text is still
-- read, for similar books, duplicates and typo repair, but its chunks are
-- not handed to the index.
ALTER TABLE libraries ADD COLUMN fulltext boolean NOT NULL DEFAULT true;

-- Turning it off deletes the library's entries from the index in one step.
ALTER TABLE fulltext_outbox ADD COLUMN delete_libraries uuid[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE fulltext_outbox DROP COLUMN delete_libraries;
ALTER TABLE libraries DROP COLUMN fulltext;
