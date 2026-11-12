-- +goose Up
-- What a Kobo reader was told it holds (#230): its library sync sends a book
-- once, again when its details or another device's reading place change
-- after synced_at and progress_at, and as removed once it leaves the
-- device's deliveries.
CREATE TABLE kobo_books (
    device_id   uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    synced_at   timestamptz NOT NULL,
    -- When the reading place last sent was written; NULL when none was.
    progress_at timestamptz,
    PRIMARY KEY (device_id, book_id)
);

-- +goose Down
DROP TABLE kobo_books;
