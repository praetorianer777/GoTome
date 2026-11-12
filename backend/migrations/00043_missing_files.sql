-- +goose Up
-- The books with files a scan no longer found (#234): the library's filter
-- and its facet start from these, which are none in a library that is whole.
CREATE INDEX book_files_missing ON book_files (book_id)
    WHERE missing_at IS NOT NULL AND trashed_at IS NULL;

-- +goose Down
DROP INDEX book_files_missing;
