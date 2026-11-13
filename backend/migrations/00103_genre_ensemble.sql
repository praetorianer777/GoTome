-- +goose Up
-- Why the ensemble suggested a genre (#528): each input's probability for it
-- and the book's words that weighed most, for the review to show.
ALTER TABLE book_genres ADD COLUMN reasons jsonb;

-- When a file's words were counted: a book counted after its genres were
-- looked at is looked at again, as the ensemble reads the counts.
ALTER TABLE file_words ADD COLUMN counted_at timestamptz NOT NULL DEFAULT now();

-- vector and combined leave the setting (#528): an installation set to
-- either suggests with the ensemble, which replaces both.
UPDATE settings SET value = 'ensemble', updated_at = now()
WHERE key = 'genres.suggest' AND value IN ('vector', 'combined');

-- +goose Down
UPDATE settings SET value = 'combined', updated_at = now()
WHERE key = 'genres.suggest' AND value = 'ensemble';
ALTER TABLE file_words DROP COLUMN counted_at;
ALTER TABLE book_genres DROP COLUMN reasons;
