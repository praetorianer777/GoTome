-- +goose Up
-- What an audio file's tags say about its place among the parts of a book.
-- The scan orders parts by their names; once every part's tags are read,
-- these order them instead.
ALTER TABLE book_files
    ADD COLUMN track_number integer CHECK (track_number >= 0),
    ADD COLUMN disc_number  integer CHECK (disc_number >= 0);

-- The chapters an audio file marks, in order.
CREATE TABLE audio_chapters (
    file_id  uuid NOT NULL REFERENCES book_files (id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    title    text NOT NULL,
    start_ms bigint NOT NULL CHECK (start_ms >= 0),
    end_ms   bigint NOT NULL CHECK (end_ms >= start_ms),
    PRIMARY KEY (file_id, position)
);

-- +goose Down
DROP TABLE audio_chapters;
ALTER TABLE book_files DROP COLUMN disc_number, DROP COLUMN track_number;
