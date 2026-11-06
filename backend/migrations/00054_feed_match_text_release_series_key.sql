-- +goose Up
-- What a feed's filters are matched against, kept so that new filters can
-- be matched against the items stored (#322): the key of the item's
-- title, categories and summary. NULL for an item stored before, until its
-- feed lists it again; until then its title stands for it.
ALTER TABLE feed_items ADD COLUMN match_text text;

-- A release's series by its key, as the catalogue compares series (#322).
-- What is here before is keyed roughly in SQL, without folding accents;
-- the next poll that lists the release keys it as catalog.Key does.
ALTER TABLE releases ADD COLUMN series_key text;
UPDATE releases
SET series_key = nullif(btrim(regexp_replace(lower(series), '[^[:alnum:]]+', ' ', 'g')), '')
WHERE series IS NOT NULL;
CREATE INDEX releases_series_key ON releases (series_key) WHERE series_key IS NOT NULL;

-- +goose Down
DROP INDEX releases_series_key;
ALTER TABLE releases DROP COLUMN series_key;
ALTER TABLE feed_items DROP COLUMN match_text;
