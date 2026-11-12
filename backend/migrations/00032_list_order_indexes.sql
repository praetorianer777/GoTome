-- +goose Up
-- The orders of a list of every library the viewer sees (#75). The indexes
-- that lead with library_id serve a list of one library; across libraries
-- a list in title order otherwise reads every book and sorts them, where
-- walking one of these stops after a page.
CREATE INDEX books_sort_idx ON books (sort_title, id) WHERE deleted_at IS NULL;
CREATE INDEX books_author_idx ON books (author_sort, sort_title, id) WHERE deleted_at IS NULL;
CREATE INDEX books_added_idx ON books (created_at, id) WHERE deleted_at IS NULL;
-- The language as filters compare it (catalog.languageOf): with its
-- statistics the planner stops taking a language for one book in 50,000.
CREATE INDEX books_language_idx ON books ((lower(split_part(replace(language, '_', '-'), '-', 1)))) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX books_language_idx;
DROP INDEX books_added_idx;
DROP INDEX books_author_idx;
DROP INDEX books_sort_idx;
