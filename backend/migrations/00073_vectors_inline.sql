-- +goose Up
-- A vector of 1,024 numbers (bge-m3) is 4 KB, over the 2 KB at which Postgres
-- moves a value into the table's TOAST relation. Comparing every book's
-- vectors then reads two relations and a second index per vector, which
-- took most of the time of similar books, recommendations and the search by
-- description on a library of 50,000 books (#456). Inline, a vector is one
-- read. The rows are written again so that the stored ones follow.
ALTER TABLE book_vectors ALTER COLUMN embedding SET STORAGE PLAIN;
UPDATE book_vectors SET embedding = (embedding::real[])::vector
WHERE pg_column_size(embedding) > 2000;

-- +goose Down
ALTER TABLE book_vectors ALTER COLUMN embedding SET STORAGE EXTERNAL;
