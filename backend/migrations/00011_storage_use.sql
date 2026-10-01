-- +goose Up
-- What each user's uploads take up: the one place that says what counts.
-- A file that is gone from disk or in the trash takes up nothing.
CREATE VIEW storage_use AS
SELECT uploaded_by AS user_id, sum(size_bytes)::bigint AS used_bytes
FROM book_files
WHERE uploaded_by IS NOT NULL AND trashed_at IS NULL AND missing_at IS NULL
GROUP BY uploaded_by;

CREATE INDEX book_files_uploaded_by_idx ON book_files (uploaded_by) WHERE uploaded_by IS NOT NULL;

-- +goose Down
DROP INDEX book_files_uploaded_by_idx;
DROP VIEW storage_use;
