-- +goose Up
-- How often each word occurs in a primary text file (#526), counted while
-- its text is in memory at chunking, for the genres' word weights
-- (docs/decisions/genre-detection.md). The counts are valid while
-- chunked_at is the file's: a file read again gets new ones in the
-- transaction that chunks it. Derived, like the chunks: not in backups.
CREATE TABLE file_words (
    file_id    uuid PRIMARY KEY REFERENCES book_files (id) ON DELETE CASCADE,
    version    integer NOT NULL,
    chunked_at timestamptz NOT NULL,
    words      integer NOT NULL CHECK (words >= 0),
    counts     bytea NOT NULL
);

-- +goose Down
DROP TABLE file_words;
