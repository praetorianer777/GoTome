-- +goose Up
-- The ISBN a file's text prints beside "Print" or "Taschenbuch" is another
-- edition's: it is asked about when a book is looked up (#488) but never
-- stored as the book's identifier. Derived from the file, read again with it.
ALTER TABLE book_files ADD COLUMN text_print_isbn text;

-- +goose Down
ALTER TABLE book_files DROP COLUMN text_print_isbn;
