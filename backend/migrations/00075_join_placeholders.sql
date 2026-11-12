-- +goose Up
-- A book read but not owned is joined to the library's book once it is one
-- (#459): the joins are runs of their own on the Changes page, and an import
-- row may be a "maybe", a looser match for the person to accept.
ALTER TABLE curation_runs DROP CONSTRAINT curation_runs_kind_check;
ALTER TABLE curation_runs ADD CONSTRAINT curation_runs_kind_check
    CHECK (kind IN ('duplicates', 'cleanup', 'matches', 'placeholders'));

ALTER TABLE reading_import_rows DROP CONSTRAINT reading_import_rows_match_check;
ALTER TABLE reading_import_rows ADD CONSTRAINT reading_import_rows_match_check
    CHECK (match IN ('book', 'new', 'ambiguous', 'maybe', 'skipped'));

-- +goose Down
UPDATE reading_import_rows SET match = 'new', book_id = NULL, candidates = '{}' WHERE match = 'maybe';
ALTER TABLE reading_import_rows DROP CONSTRAINT reading_import_rows_match_check;
ALTER TABLE reading_import_rows ADD CONSTRAINT reading_import_rows_match_check
    CHECK (match IN ('book', 'new', 'ambiguous', 'skipped'));
DELETE FROM curation_runs WHERE kind = 'placeholders';
ALTER TABLE curation_runs DROP CONSTRAINT curation_runs_kind_check;
ALTER TABLE curation_runs ADD CONSTRAINT curation_runs_kind_check
    CHECK (kind IN ('duplicates', 'cleanup', 'matches'));
