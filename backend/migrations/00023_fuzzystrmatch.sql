-- +goose Up
-- levenshtein() ranks the words of the text a typo may stand for.
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;

-- +goose Down
DROP EXTENSION IF EXISTS fuzzystrmatch;
