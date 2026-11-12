-- +goose Up
-- The weights of the match score learned from a library's own books (#490),
-- or imported from a file someone shared; and the last run of a measure or
-- a learning, with its progress and its report (numbers only).
CREATE TABLE match_score_models (
    slot       text PRIMARY KEY CHECK (slot IN ('learned', 'imported')),
    model      jsonb NOT NULL,
    books      integer NOT NULL,
    trained_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE match_runs (
    kind             text PRIMARY KEY CHECK (kind IN ('measure', 'learn')),
    state            text NOT NULL CHECK (state IN ('queued', 'running', 'done', 'failed')),
    done             integer NOT NULL DEFAULT 0,
    total            integer NOT NULL DEFAULT 0,
    estimate_seconds integer NOT NULL DEFAULT 0,
    requested        integer NOT NULL DEFAULT 0,
    started_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz,
    error            text NOT NULL DEFAULT '',
    report           jsonb
);

-- +goose Down
DROP TABLE match_runs;
DROP TABLE match_score_models;
