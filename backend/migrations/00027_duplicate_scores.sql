-- +goose Up
-- How sure one piece of evidence makes it that two books are one: an equal
-- file surely, equal content almost, a shared ISBN very likely, the same
-- title and author likely, and shared text as much as the larger of its
-- Jaccard and containment figures.
CREATE FUNCTION duplicate_score(kind text, detail text) RETURNS real
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE kind
        WHEN 'sha256' THEN 1.0
        WHEN 'content' THEN 0.99
        WHEN 'isbn' THEN 0.9
        WHEN 'title_author' THEN 0.75
        WHEN 'overlap' THEN (
            SELECT greatest(m[1]::real, m[2]::real, m[3]::real)
            FROM regexp_match(detail, 'jaccard=([0-9.]+) a_in_b=([0-9.]+) b_in_a=([0-9.]+)') AS m)
        ELSE 0
    END::real
$$;

-- A pair's score is that of its strongest evidence, kept by the check that
-- changes its evidence, so that lists sort and filter by it.
ALTER TABLE duplicate_pairs ADD COLUMN score real NOT NULL DEFAULT 0;

UPDATE duplicate_pairs p SET score = coalesce(
    (SELECT max(duplicate_score(e.kind, e.detail)) FROM duplicate_evidence e WHERE e.pair_id = p.id), 0);

CREATE INDEX duplicate_pairs_score_idx ON duplicate_pairs (state, score DESC, id DESC);

-- +goose Down
DROP INDEX duplicate_pairs_score_idx;
ALTER TABLE duplicate_pairs DROP COLUMN score;
DROP FUNCTION duplicate_score(text, text);
