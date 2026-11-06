-- +goose Up
-- A file whose write-back was passed by because its library's folder could
-- not be written (#321): once it can be again, its write-back is queued
-- anew. Cleared when that is queued and when a write-back succeeds, so a
-- file already written is not written twice.
ALTER TABLE book_files ADD COLUMN write_back_pending boolean NOT NULL DEFAULT false;

CREATE INDEX book_files_write_back_pending ON book_files (library_id) WHERE write_back_pending;

-- +goose Down
DROP INDEX book_files_write_back_pending;
ALTER TABLE book_files DROP COLUMN write_back_pending;
