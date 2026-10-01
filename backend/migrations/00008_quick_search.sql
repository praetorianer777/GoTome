-- +goose Up
-- Trigram indexes on the compared forms of titles and names, so that a
-- misspelt or half-typed one is found: "sandersen" finds Sanderson. This
-- is the quick search by title, author and series; searching the text of
-- the books is another matter.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX books_title_trgm_idx ON books USING gin (title_key gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX authors_name_trgm_idx ON authors USING gin (name_key gin_trgm_ops);
CREATE INDEX series_name_trgm_idx ON series USING gin (name_key gin_trgm_ops);

-- +goose Down
DROP INDEX series_name_trgm_idx;
DROP INDEX authors_name_trgm_idx;
DROP INDEX books_title_trgm_idx;
DROP EXTENSION IF EXISTS pg_trgm;
