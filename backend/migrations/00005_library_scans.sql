-- +goose Up
-- One row per scan of a library: who asked for it, how far it got and what it
-- found. A scan may take several runs of its job, after a restart or when it
-- pauses to let the queue breathe; the row is what holds them together.
CREATE TABLE library_scans (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    library_id     uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    state          text NOT NULL DEFAULT 'queued'
                   CHECK (state IN ('queued', 'running', 'done', 'failed')),
    -- NULL when the schedule asked.
    requested_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    requested_at   timestamptz NOT NULL DEFAULT now(),
    -- The job in the queue that does the scan.
    job_id         bigint,
    started_at     timestamptz,
    finished_at    timestamptz,
    -- Files of a known format the last walk found.
    files_seen     integer NOT NULL DEFAULT 0,
    files_added    integer NOT NULL DEFAULT 0,
    files_changed  integer NOT NULL DEFAULT 0,
    files_moved    integer NOT NULL DEFAULT 0,
    -- Files that had been missing and are back.
    files_restored integer NOT NULL DEFAULT 0,
    files_missing  integer NOT NULL DEFAULT 0,
    -- Files and folders that could not be read.
    files_skipped  integer NOT NULL DEFAULT 0,
    books_added    integer NOT NULL DEFAULT 0,
    -- In words for whoever looks at the library; the detail is in the log.
    error          text
);

-- A library has at most one scan waiting or running.
CREATE UNIQUE INDEX library_scans_active_key ON library_scans (library_id)
    WHERE state IN ('queued', 'running');
CREATE INDEX library_scans_library_idx ON library_scans (library_id, requested_at DESC, id DESC);

-- +goose Down
DROP TABLE library_scans;
