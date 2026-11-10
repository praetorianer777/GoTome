-- +goose Up
-- Curation runs (#464): every change made to books other than by hand in an
-- editor, or by one click that cannot be taken back, is an action of a run,
-- with the state of the books it touched before and after. A run is a
-- person's merge, replace or clean-up, or one rule's pass over the library.
CREATE TABLE curation_runs (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    kind           text NOT NULL CHECK (kind IN ('duplicates', 'cleanup', 'matches')),
    -- The rule of an automatic run; a person's run has none.
    rule           text,
    started_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    dry_run        boolean NOT NULL DEFAULT false,
    -- Files an automatic run trashes stay until a person confirms it.
    automatic      boolean NOT NULL DEFAULT false,
    state          text NOT NULL DEFAULT 'running'
                   CHECK (state IN ('planned', 'running', 'done', 'confirmed', 'undone')),
    -- The bulk change that makes the actions of a clean-up.
    bulk_change_id uuid UNIQUE REFERENCES bulk_changes (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    confirmed_at   timestamptz,
    undone_at      timestamptz
);

CREATE INDEX curation_runs_created_idx ON curation_runs (created_at DESC, id DESC);

CREATE TABLE curation_actions (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    run_id        uuid NOT NULL REFERENCES curation_runs (id) ON DELETE CASCADE,
    library_id    uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    kind          text NOT NULL CHECK (kind IN ('merge', 'replace', 'edit')),
    rule          text,
    reason        text NOT NULL DEFAULT '',
    -- The books it touched, the surviving one first.
    books         uuid[] NOT NULL,
    -- The files it moved to the trash.
    trashed_files uuid[] NOT NULL DEFAULT '{}',
    -- The rows of the books in every table curation.Tables names, before
    -- and after, as JSON.
    before        jsonb NOT NULL,
    after         jsonb NOT NULL,
    state         text NOT NULL DEFAULT 'done' CHECK (state IN ('done', 'undone')),
    -- Why the last try to undo it did not, if it did not.
    note          text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    undone_at     timestamptz,
    undone_by     uuid REFERENCES users (id) ON DELETE SET NULL
);

CREATE INDEX curation_actions_run_idx ON curation_actions (run_id, created_at, id);
CREATE INDEX curation_actions_library_idx ON curation_actions (library_id);

-- +goose Down
DROP TABLE curation_actions;
DROP TABLE curation_runs;
