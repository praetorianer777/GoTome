-- +goose Up
-- Books a person read elsewhere (#536), told on Discover: theirs alone, no
-- book row, no wish. The keys hide the book and its other editions from
-- Discover as a dismissal does; the rest is what counts for their taste
-- (author and stars, series place, genres) and what the list "Read
-- elsewhere" shows to take one back.
CREATE TABLE discover_read (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title        text NOT NULL,
    authors      text[] NOT NULL DEFAULT '{}',
    series       text NOT NULL DEFAULT '',
    series_key   text NOT NULL DEFAULT '',
    series_index double precision,
    genres       text[] NOT NULL DEFAULT '{}',
    stars        smallint CHECK (stars BETWEEN 1 AND 5),
    title_keys   text[] NOT NULL,
    author_keys  text[] NOT NULL DEFAULT '{}',
    isbns        text[] NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX discover_read_user ON discover_read (user_id, created_at DESC);

-- +goose Down
DROP TABLE discover_read;
