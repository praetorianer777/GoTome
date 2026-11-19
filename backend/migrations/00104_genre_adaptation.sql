-- +goose Up
-- The ensemble adapted to this installation's own books (#530): the shipped
-- training merged with what its tags and accepted suggestions teach. One row.
-- Derived, so a backup leaves it out and the daily job makes it again; it
-- belongs to the shipped ensemble it was trained beside (shipped_version),
-- and a refreshed default ignores it until the job has retrained.
CREATE TABLE genre_adaptation (
    singleton       boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    shipped_version text NOT NULL,
    model           text NOT NULL,
    version         text NOT NULL,
    labelled        integer NOT NULL,
    books           integer NOT NULL,
    trained_at      timestamptz NOT NULL DEFAULT now(),
    ensemble        bytea NOT NULL,
    words           bytea NOT NULL
);

-- +goose Down
DROP TABLE genre_adaptation;
