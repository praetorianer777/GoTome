-- +goose Up
-- Two recordings of one audiobook by other narrators.
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related', 'recording'));

-- +goose Down
DELETE FROM book_relations WHERE kind = 'recording';
ALTER TABLE book_relations DROP CONSTRAINT book_relations_kind_check;
ALTER TABLE book_relations ADD CONSTRAINT book_relations_kind_check
    CHECK (kind IN ('edition', 'translation', 'related'));
