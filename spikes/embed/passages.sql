-- Passage vectors in Postgres (#174): sizes and query times of the
-- layouts docs/decisions/passage-vectors.md compares. Loaded by
-- passages-pg.sh from 100,000 passage vectors of the corpus
-- (embed passage-export), blended into more as passage-scale does.
\set ON_ERROR_STOP on
\timing on
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE base (chunk_id uuid, library_id uuid, v vector(384));
\copy base FROM '/in/base.tsv'
CREATE TABLE b AS SELECT row_number() OVER () AS rn, library_id, v FROM base;
CREATE UNIQUE INDEX ON b (rn);
ANALYZE b;

-- blended(k): 100,000 new vectors per k, each two real ones mixed.
CREATE FUNCTION blended(rounds int) RETURNS TABLE (chunk_id uuid, library_id uuid, v vector)
LANGUAGE sql AS $$
    SELECT gen_random_uuid(), b1.library_id,
           l2_normalize(b1.v * array_fill(0.75::real, ARRAY[384])::vector
                      + b2.v * array_fill(0.25::real, ARRAY[384])::vector)
      FROM generate_series(1, rounds) k
      JOIN b b1 ON true
      JOIN b b2 ON b2.rn = (b1.rn + k * 7919) % (SELECT count(*) FROM b) + 1
$$;

-- Today's book_vectors keeps fp32 vectors; halfvec is half of that.
CREATE TABLE p_vec (chunk_id uuid PRIMARY KEY, library_id uuid NOT NULL, embedding vector(384) NOT NULL);
INSERT INTO p_vec SELECT * FROM blended(10);
CREATE TABLE p_half (chunk_id uuid PRIMARY KEY, library_id uuid NOT NULL, embedding halfvec(384) NOT NULL);
INSERT INTO p_half SELECT chunk_id, library_id, embedding::halfvec FROM p_vec;

-- The signs and an int8 copy of each vector (384 bytes and a 4-byte scale;
-- the bytes here stand in for it, as only their size counts), in one table
-- and in two.
CREATE TABLE p_both (chunk_id uuid PRIMARY KEY, library_id uuid NOT NULL, bits bit(384) NOT NULL, vec bytea NOT NULL);
INSERT INTO p_both
SELECT chunk_id, library_id, binary_quantize(v)::bit(384),
       decode(repeat(md5(chunk_id::text), 24) || 'abcdef01', 'hex')
  FROM blended(32);
CREATE TABLE p_bit (chunk_id uuid PRIMARY KEY, library_id uuid NOT NULL, bits bit(384) NOT NULL);
INSERT INTO p_bit SELECT chunk_id, library_id, bits FROM p_both;
CREATE TABLE p_i8 (chunk_id uuid PRIMARY KEY, vec bytea NOT NULL);
INSERT INTO p_i8 SELECT chunk_id, vec FROM p_both;
VACUUM ANALYZE p_vec, p_half, p_both, p_bit, p_i8;

CREATE TABLE sizes AS
SELECT relname AS "table", c.reltuples::bigint AS "rows",
       pg_table_size(c.oid) AS heap, pg_indexes_size(c.oid) AS indexes
  FROM pg_class c WHERE relname IN ('p_vec', 'p_half', 'p_both', 'p_bit', 'p_i8');

-- The query vectors: twenty passages of the base, in each type.
CREATE TABLE queries AS SELECT rn, v FROM b WHERE rn % 5000 = 0;

-- bench runs sql with each query vector as $1 (cast to type) and returns
-- the median milliseconds.
CREATE FUNCTION bench(sql text, typ text) RETURNS numeric LANGUAGE plpgsql AS $$
DECLARE
    q record;
    t timestamptz;
    ms numeric[] := '{}';
    lit text;
BEGIN
    FOR q IN SELECT v FROM queries ORDER BY rn LOOP
        lit := CASE typ
            WHEN 'bit' THEN quote_literal(binary_quantize(q.v)::text) || '::bit(384)'
            ELSE quote_literal(q.v::text) || '::' || typ END;
        t := clock_timestamp();
        EXECUTE 'SELECT count(*) FROM (' || replace(sql, '$1', lit) || ') x';
        ms := ms || (extract(epoch FROM clock_timestamp() - t) * 1000)::numeric;
    END LOOP;
    RETURN (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY x) FROM unnest(ms) x);
END $$;

-- Two of the four libraries are visible, as a filter in the scan.
CREATE TABLE visible AS SELECT DISTINCT library_id FROM base ORDER BY 1 LIMIT 2;
CREATE TABLE one_visible AS SELECT library_id FROM visible LIMIT 1;

SET max_parallel_workers_per_gather = 2;
CREATE TABLE timings (what text, ms numeric);
-- Each once to warm the cache, then measured.
SELECT bench('SELECT chunk_id FROM p_half WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'halfvec');
INSERT INTO timings VALUES ('halfvec 1M, exact, 2 of 4 libraries',
    bench('SELECT chunk_id FROM p_half WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'halfvec'));
SELECT bench('SELECT chunk_id FROM p_vec WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'vector');
INSERT INTO timings VALUES ('vector 1M, exact, 2 of 4 libraries',
    bench('SELECT chunk_id FROM p_vec WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'vector'));
SELECT bench('SELECT chunk_id FROM p_bit WHERE library_id IN (SELECT * FROM visible) ORDER BY bits <~> $1 LIMIT 2000', 'bit');
INSERT INTO timings VALUES ('bit 3.2M, exact, 2 of 4 libraries, best 2000',
    bench('SELECT chunk_id FROM p_bit WHERE library_id IN (SELECT * FROM visible) ORDER BY bits <~> $1 LIMIT 2000', 'bit'));
SELECT bench('SELECT chunk_id FROM p_both WHERE library_id IN (SELECT * FROM visible) ORDER BY bits <~> $1 LIMIT 2000', 'bit');
INSERT INTO timings VALUES ('bit in the int8 table 3.2M, exact, best 2000',
    bench('SELECT chunk_id FROM p_both WHERE library_id IN (SELECT * FROM visible) ORDER BY bits <~> $1 LIMIT 2000', 'bit'));
INSERT INTO timings VALUES ('int8 rows of 2000 chunks by key',
    bench('SELECT vec FROM p_i8 WHERE chunk_id IN (SELECT chunk_id FROM p_bit WHERE bits <~> $1 IS NOT NULL LIMIT 2000)', 'bit')
    - bench('SELECT chunk_id FROM p_bit WHERE bits <~> $1 IS NOT NULL LIMIT 2000', 'bit'));

\echo HNSW on halfvec, 1M
SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 3;
CREATE TABLE build (what text, seconds numeric);
DO $$
DECLARE t timestamptz := clock_timestamp();
BEGIN
    CREATE INDEX p_half_hnsw ON p_half USING hnsw (embedding halfvec_cosine_ops);
    INSERT INTO build VALUES ('hnsw halfvec 1M', extract(epoch FROM clock_timestamp() - t));
END $$;
SET hnsw.ef_search = 100;
SET hnsw.iterative_scan = relaxed_order;
SELECT bench('SELECT chunk_id FROM p_half WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'halfvec');
INSERT INTO timings VALUES ('halfvec 1M, HNSW, 2 of 4 libraries',
    bench('SELECT chunk_id FROM p_half WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> $1 LIMIT 100', 'halfvec'));
INSERT INTO timings VALUES ('halfvec 1M, HNSW, 1 of 4 libraries',
    bench('SELECT chunk_id FROM p_half WHERE library_id IN (SELECT * FROM one_visible) ORDER BY embedding <=> $1 LIMIT 100', 'halfvec'));

-- Recall of the HNSW search against the exact one, best 100, 2 of 4.
CREATE TABLE recall (hnsw_recall_at_100 numeric);
DO $$
DECLARE
    q record;
    exact uuid[];
    approx uuid[];
    total numeric := 0;
    n int := 0;
BEGIN
    FOR q IN SELECT v FROM queries LOOP
        SET LOCAL enable_indexscan = off;
        SELECT array_agg(chunk_id) INTO exact FROM (SELECT chunk_id FROM p_half
         WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> q.v::halfvec LIMIT 100) x;
        SET LOCAL enable_indexscan = on;
        SELECT array_agg(chunk_id) INTO approx FROM (SELECT chunk_id FROM p_half
         WHERE library_id IN (SELECT * FROM visible) ORDER BY embedding <=> q.v::halfvec LIMIT 100) x;
        total := total + (SELECT count(*) FROM unnest(approx) a WHERE a = ANY (exact));
        n := n + 1;
    END LOOP;
    INSERT INTO recall VALUES (total / n / 100);
END $$;

UPDATE sizes s SET indexes = pg_indexes_size(s."table"::regclass);
\pset format aligned
SELECT "table", "rows", pg_size_pretty(heap) heap, heap / greatest("rows", 1) AS heap_per_row,
       pg_size_pretty(indexes) indexes, indexes / greatest("rows", 1) AS index_per_row FROM sizes ORDER BY 1;
SELECT * FROM build;
SELECT what, round(ms, 1) AS ms_p50 FROM timings;
SELECT * FROM recall;
