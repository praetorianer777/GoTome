-- +goose Up
-- Resolving certain duplicates by themselves (#466): a run works through the
-- open pairs of the classes it was asked for, in a job that stops and goes on,
-- and an undo of a run is a job as well.
ALTER TABLE curation_runs
    -- The classes of duplicate pairs a duplicates run was asked to handle.
    ADD COLUMN classes         text[] NOT NULL DEFAULT '{}',
    -- The last pair the run has looked at; the next pass goes on after it.
    ADD COLUMN resume_after    uuid,
    -- Pairs the run looked at and left for a person.
    ADD COLUMN skipped         integer NOT NULL DEFAULT 0,
    -- Whose eyes the run works with: the libraries they see.
    ADD COLUMN scope_viewer    uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN scope_sees_all  boolean NOT NULL DEFAULT false,
    -- Set while the run is being undone by a job, with whose scope it uses.
    ADD COLUMN undo_requested_at timestamptz,
    ADD COLUMN undo_viewer     uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN undo_sees_all   boolean NOT NULL DEFAULT false,
    -- The last action the undo has tried; the next try goes on before it.
    ADD COLUMN undo_cursor_at  timestamptz,
    ADD COLUMN undo_cursor_id  uuid;

-- A dry run records what it would do as actions that change nothing; a
-- link keeps two books and relates them.
ALTER TABLE curation_actions DROP CONSTRAINT curation_actions_kind_check;
ALTER TABLE curation_actions ADD CONSTRAINT curation_actions_kind_check
    CHECK (kind IN ('merge', 'replace', 'edit', 'link', 'skip'));

-- One book holds the other's text. The smaller ID is the first of the two:
-- contains says that it is the one that holds the other, contained_in that
-- it is the one held.
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related', 'recording', 'format', 'contains', 'contained_in'));

-- +goose Down
DELETE FROM book_relations WHERE kind IN ('contains', 'contained_in');
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related', 'recording', 'format'));
DELETE FROM curation_actions WHERE kind IN ('link', 'skip');
ALTER TABLE curation_actions DROP CONSTRAINT curation_actions_kind_check;
ALTER TABLE curation_actions ADD CONSTRAINT curation_actions_kind_check
    CHECK (kind IN ('merge', 'replace', 'edit'));
ALTER TABLE curation_runs
    DROP COLUMN undo_cursor_id, DROP COLUMN undo_cursor_at, DROP COLUMN undo_sees_all, DROP COLUMN undo_viewer, DROP COLUMN undo_requested_at,
    DROP COLUMN scope_sees_all, DROP COLUMN scope_viewer, DROP COLUMN skipped,
    DROP COLUMN resume_after, DROP COLUMN classes;
