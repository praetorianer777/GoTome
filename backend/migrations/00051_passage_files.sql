-- +goose Up
-- Which primary text files have a vector per passage (#174,
-- docs/decisions/passage-vectors.md): the vectors themselves are files of
-- the passage store under the data directory, like the full-text index,
-- and this says of which model and of which chunking of the file they
-- are. A file whose row is missing, of another model or of another
-- chunked_at is embedded again. Derived, like the chunks: not in backups.
CREATE TABLE passage_files (
    file_id       uuid PRIMARY KEY REFERENCES book_files (id) ON DELETE CASCADE,
    model         text NOT NULL,
    model_version text NOT NULL,
    chunked_at    timestamptz NOT NULL,
    passages      integer NOT NULL CHECK (passages >= 0),
    embedded_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE passage_files;
