-- +goose Up
-- The reading time a Kobo reader reported for a book (#415). The reader sends
-- the minutes it has counted in all, so a session is the part beyond what was
-- seen before. Kept apart from kobo_books, which a reader that syncs from the
-- start is told anew of, or the minutes it still reports would count twice.
CREATE TABLE kobo_reading_time (
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    book_id   uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    minutes   integer NOT NULL,
    PRIMARY KEY (device_id, book_id)
);

-- +goose Down
DROP TABLE kobo_reading_time;
