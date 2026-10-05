-- +goose Up
-- Who moved a file to the trash; the purge finds the old ones by when.
ALTER TABLE book_files ADD COLUMN trashed_by uuid REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX book_files_trashed_idx ON book_files (trashed_at) WHERE trashed_at IS NOT NULL;

-- +goose Down
DROP INDEX book_files_trashed_idx;
ALTER TABLE book_files DROP COLUMN trashed_by;
