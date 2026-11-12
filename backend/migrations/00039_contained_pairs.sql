-- +goose Up
-- A pair is contained when one book holds the other's text but the two are
-- not one text (#215): a story in its collection, an episode in its
-- omnibus. Every piece of its evidence is shared text with a Jaccard figure
-- under 0.5, which the overlap check records only where the containment is
-- high; a file, content, ISBN or title-and-author match keeps it a
-- duplicate.
CREATE FUNCTION duplicate_contains(kind text, detail text) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT kind = 'overlap' AND coalesce(
        (SELECT m[1]::real < 0.5 FROM regexp_match(detail, 'jaccard=([0-9.]+)') AS m), false)
$$;

-- Kept with the score by the check that changes the pair's evidence.
ALTER TABLE duplicate_pairs ADD COLUMN contained boolean NOT NULL DEFAULT false;

UPDATE duplicate_pairs p SET contained = coalesce(
    (SELECT bool_and(duplicate_contains(e.kind, e.detail)) FROM duplicate_evidence e WHERE e.pair_id = p.id), false);

DROP INDEX duplicate_pairs_score_idx;
CREATE INDEX duplicate_pairs_score_idx ON duplicate_pairs (state, contained, score DESC, id DESC);

-- +goose Down
DROP INDEX duplicate_pairs_score_idx;
CREATE INDEX duplicate_pairs_score_idx ON duplicate_pairs (state, score DESC, id DESC);
ALTER TABLE duplicate_pairs DROP COLUMN contained;
DROP FUNCTION duplicate_contains(text, text);
