-- +goose Up
-- A device uploads books only if the person allowed it when making the
-- device (#211).
ALTER TABLE devices ADD COLUMN can_upload boolean NOT NULL DEFAULT false;
-- What the person chose when accepting a request, for the device made from it.
ALTER TABLE device_requests ADD COLUMN can_upload boolean NOT NULL DEFAULT false;

-- The device's own files are looked up by hash, by the stored one or by the
-- one the file had before a write-back.
CREATE INDEX book_files_original_sha256_idx ON book_files (original_sha256) WHERE original_sha256 IS NOT NULL;

-- +goose Down
DROP INDEX book_files_original_sha256_idx;
ALTER TABLE device_requests DROP COLUMN can_upload;
ALTER TABLE devices DROP COLUMN can_upload;
