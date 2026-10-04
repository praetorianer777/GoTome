-- +goose Up
-- A placeholder is a book the library does not hold yet: wished for, made
-- from a provider's record, without files. The first file imported that is
-- the same book fulfils it, and it is a book like any other from then on.
ALTER TABLE books ADD COLUMN placeholder boolean NOT NULL DEFAULT false;

CREATE INDEX books_placeholders_idx ON books (library_id) WHERE placeholder AND deleted_at IS NULL;

-- +goose Down
DROP INDEX books_placeholders_idx;
ALTER TABLE books DROP COLUMN placeholder;
