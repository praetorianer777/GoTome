-- +goose Up
-- Which version of epub.Measure measured a file (#262). The
-- ingest.measure_files job measures again every EPUB measured by an older
-- one, keeping its findings until then; NULL is a file not measured yet,
-- or measured before versions were recorded.
ALTER TABLE book_files ADD COLUMN measures_version smallint;

DROP INDEX book_files_unmeasured;
CREATE INDEX book_files_unmeasured ON book_files (measures_version, id)
    WHERE format = 'epub' AND trashed_at IS NULL AND missing_at IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION clear_epub_measures() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.epub_measures := NULL;
    NEW.ereader_findings := NULL;
    NEW.measures_version := NULL;
    RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION clear_epub_measures() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.epub_measures := NULL;
    NEW.ereader_findings := NULL;
    RETURN NEW;
END $$;
-- +goose StatementEnd

DROP INDEX book_files_unmeasured;
CREATE INDEX book_files_unmeasured ON book_files (id)
    WHERE format = 'epub' AND ereader_findings IS NULL AND trashed_at IS NULL AND missing_at IS NULL;
ALTER TABLE book_files DROP COLUMN measures_version;
