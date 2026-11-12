-- +goose Up
-- Renaming and sorting the files of a library into folders by their books'
-- metadata (#414): a pattern per library, opt-in for an external one, and
-- the runs that apply it, recorded so that they can be undone.
ALTER TABLE libraries
    ADD COLUMN organise_enabled boolean NOT NULL DEFAULT false,
    -- NULL is the default pattern of the code.
    ADD COLUMN organise_pattern text;

ALTER TABLE curation_runs DROP CONSTRAINT curation_runs_kind_check;
ALTER TABLE curation_runs ADD CONSTRAINT curation_runs_kind_check
    CHECK (kind IN ('duplicates', 'cleanup', 'matches', 'placeholders', 'organise'));
ALTER TABLE curation_runs
    ADD COLUMN organise_library uuid REFERENCES libraries (id) ON DELETE CASCADE,
    ADD COLUMN organise_pattern text;

ALTER TABLE curation_actions DROP CONSTRAINT curation_actions_kind_check;
ALTER TABLE curation_actions ADD CONSTRAINT curation_actions_kind_check
    CHECK (kind IN ('merge', 'replace', 'edit', 'link', 'skip', 'organise'));

-- +goose Down
DELETE FROM curation_actions WHERE kind = 'organise';
ALTER TABLE curation_actions DROP CONSTRAINT curation_actions_kind_check;
ALTER TABLE curation_actions ADD CONSTRAINT curation_actions_kind_check
    CHECK (kind IN ('merge', 'replace', 'edit', 'link', 'skip'));
ALTER TABLE curation_runs DROP COLUMN organise_pattern, DROP COLUMN organise_library;
DELETE FROM curation_runs WHERE kind = 'organise';
ALTER TABLE curation_runs DROP CONSTRAINT curation_runs_kind_check;
ALTER TABLE curation_runs ADD CONSTRAINT curation_runs_kind_check
    CHECK (kind IN ('duplicates', 'cleanup', 'matches', 'placeholders'));
ALTER TABLE libraries DROP COLUMN organise_pattern, DROP COLUMN organise_enabled;
