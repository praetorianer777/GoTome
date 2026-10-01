-- +goose Up
-- Where each person is in a book, once for its text and once for its audio:
-- the two are read on different devices at different speeds.
CREATE TABLE reading_progress (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    medium      text NOT NULL CHECK (medium IN ('ebook', 'audio')),
    -- The file the position is in; a locator means nothing in another.
    file_id     uuid REFERENCES book_files (id) ON DELETE SET NULL,
    -- Where exactly, as the reader writes it: an EPUB CFI, a PDF page, a
    -- millisecond of the audio.
    locator     text NOT NULL,
    fraction    double precision NOT NULL CHECK (fraction BETWEEN 0 AND 1),
    chapter     text,
    page        integer,
    position_ms bigint,
    -- Which device or browser wrote it, as the client names itself.
    client_id   text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id, medium)
);

-- A stretch of reading on one client: from the first position written to
-- the last, while the writes keep coming.
CREATE TABLE reading_sessions (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id       uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    medium        text NOT NULL CHECK (medium IN ('ebook', 'audio')),
    client_id     text NOT NULL,
    started_at    timestamptz NOT NULL DEFAULT now(),
    ended_at      timestamptz NOT NULL DEFAULT now(),
    from_fraction double precision NOT NULL,
    to_fraction   double precision NOT NULL
);

CREATE INDEX reading_sessions_user_idx ON reading_sessions (user_id, book_id, medium, ended_at);

-- Every time a person reached the end of a book, so that reading it again
-- counts again.
CREATE TABLE reading_finishes (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id     uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    medium      text NOT NULL CHECK (medium IN ('ebook', 'audio')),
    finished_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX reading_finishes_user_idx ON reading_finishes (user_id, book_id);

-- +goose Down
DROP TABLE reading_finishes;
DROP TABLE reading_sessions;
DROP TABLE reading_progress;
