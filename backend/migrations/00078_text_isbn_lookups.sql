-- +goose Up
-- Which ISBN read from a book's text it was last queued for a lookup by, so
-- that the hourly sweep queues each such book once (#499).
CREATE TABLE text_isbn_lookups (
    book_id   uuid PRIMARY KEY REFERENCES books (id) ON DELETE CASCADE,
    isbn      text NOT NULL,
    queued_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE text_isbn_lookups;
