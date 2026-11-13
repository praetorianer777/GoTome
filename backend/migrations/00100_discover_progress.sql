-- +goose Up
-- How far a person's run is (#533): what its pass worked out to ask, and
-- until when a pass holds it. Both are derived like the run itself.
ALTER TABLE discover_runs
    ADD COLUMN planned         boolean NOT NULL DEFAULT false,
    ADD COLUMN planned_authors integer NOT NULL DEFAULT 0,
    ADD COLUMN planned_series  integer NOT NULL DEFAULT 0,
    ADD COLUMN planned_genres  integer NOT NULL DEFAULT 0,
    -- The deadline of the pass working the run; empty between passes. A pass
    -- that died leaves a time in the past, which is no pass.
    ADD COLUMN pass_until      timestamptz;

-- +goose Down
ALTER TABLE discover_runs
    DROP COLUMN pass_until,
    DROP COLUMN planned_genres,
    DROP COLUMN planned_series,
    DROP COLUMN planned_authors,
    DROP COLUMN planned;
