-- +goose Up
-- The rated books of a library, which a full-text search by an account with
-- an age limit leaves out of the index's query (#563): most books are not
-- rated, so the list stays short and is read without reading every book.
CREATE INDEX books_rated ON books (library_id, age_rating) WHERE age_rating IS NOT NULL;

-- +goose Down
DROP INDEX books_rated;
