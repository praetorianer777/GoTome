-- +goose Up
-- What makes an EPUB slow or troublesome on an e-reader (#233): its
-- measures (epub.Measures) and the findings past the limits, both written
-- at extraction or, for files read before, by the ingest.measure_files
-- job. NULL findings are a file not measured yet; a file that could not be
-- measured has none. Both are cleared when the file's bytes change.
ALTER TABLE book_files ADD COLUMN epub_measures jsonb;
ALTER TABLE book_files ADD COLUMN ereader_findings text[];

-- The job's work: none once every EPUB is measured.
CREATE INDEX book_files_unmeasured ON book_files (id)
    WHERE format = 'epub' AND ereader_findings IS NULL AND trashed_at IS NULL AND missing_at IS NULL;
-- The library's filter and its facet start from the files with findings.
CREATE INDEX book_files_ereader_findings ON book_files (book_id)
    WHERE cardinality(ereader_findings) > 0 AND trashed_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION clear_epub_measures() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.epub_measures := NULL;
    NEW.ereader_findings := NULL;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER book_files_epub_measures BEFORE UPDATE OF sha256 ON book_files
    FOR EACH ROW WHEN (NEW.sha256 IS DISTINCT FROM OLD.sha256) EXECUTE FUNCTION clear_epub_measures();

-- +goose Down
DROP TRIGGER book_files_epub_measures ON book_files;
DROP FUNCTION clear_epub_measures();
DROP INDEX book_files_ereader_findings;
DROP INDEX book_files_unmeasured;
ALTER TABLE book_files DROP COLUMN ereader_findings;
ALTER TABLE book_files DROP COLUMN epub_measures;
