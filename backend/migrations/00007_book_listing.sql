-- +goose Up
-- The first author's sort name, kept on the book so that a library of
-- thousands can be listed by author a page at a time through an index.
ALTER TABLE books ADD COLUMN author_sort text NOT NULL DEFAULT '';

UPDATE books b
SET author_sort = COALESCE((
    SELECT a.sort_name
    FROM book_contributors c
    JOIN authors a ON a.id = c.author_id
    WHERE c.book_id = b.id AND c.role = 'author'
    ORDER BY c.position
    LIMIT 1
), '');

CREATE INDEX books_library_author_idx ON books (library_id, author_sort, sort_title, id) WHERE deleted_at IS NULL;
CREATE INDEX books_library_added_idx ON books (library_id, created_at, id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX books_library_added_idx;
DROP INDEX books_library_author_idx;
ALTER TABLE books DROP COLUMN author_sort;
