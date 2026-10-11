-- +goose Up
-- KOReader names a document by an MD5 of samples of its file (#198), which
-- its progress sync sends: worked out for each ebook file by the
-- kosync.hash_files job, and cleared when the file's bytes change, so that
-- the job works it out again.
ALTER TABLE book_files ADD COLUMN koreader_md5 text;
CREATE INDEX book_files_koreader_md5_idx ON book_files (koreader_md5) WHERE koreader_md5 IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION clear_koreader_md5() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.koreader_md5 := NULL;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER book_files_koreader_md5 BEFORE UPDATE OF sha256 ON book_files
    FOR EACH ROW WHEN (NEW.sha256 IS DISTINCT FROM OLD.sha256) EXECUTE FUNCTION clear_koreader_md5();

-- +goose Down
DROP TRIGGER book_files_koreader_md5 ON book_files;
DROP FUNCTION clear_koreader_md5();
DROP INDEX book_files_koreader_md5_idx;
ALTER TABLE book_files DROP COLUMN koreader_md5;
