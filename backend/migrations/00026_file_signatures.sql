-- +goose Up
-- The MinHash signature of a book's primary text file, made from its
-- chunks: how many different shingles it has and their minimum hashes.
-- Text too short to sign has a row without a signature, so that it is not
-- read again for one.
CREATE TABLE file_signatures (
    file_id   uuid PRIMARY KEY REFERENCES book_files (id) ON DELETE CASCADE,
    version   smallint NOT NULL,
    shingles  integer NOT NULL CHECK (shingles >= 0),
    signature bytea CHECK (octet_length(signature) = 2048),
    signed_at timestamptz NOT NULL DEFAULT now()
);

-- The buckets a signature falls in, of the whole text and of its segments.
-- Files that share a bucket are compared; the key leads, so that finding a
-- file's companions is one look into the index per bucket.
CREATE TABLE file_lsh (
    band    smallint NOT NULL,
    bucket  bigint NOT NULL,
    file_id uuid NOT NULL REFERENCES file_signatures (file_id) ON DELETE CASCADE,
    PRIMARY KEY (band, bucket, file_id)
);

CREATE INDEX file_lsh_file_idx ON file_lsh (file_id);

ALTER TABLE duplicate_evidence DROP CONSTRAINT duplicate_evidence_kind_check;
ALTER TABLE duplicate_evidence ADD CONSTRAINT duplicate_evidence_kind_check
    CHECK (kind IN ('sha256', 'content', 'isbn', 'title_author', 'overlap'));

-- +goose Down
DELETE FROM duplicate_evidence WHERE kind = 'overlap';
ALTER TABLE duplicate_evidence DROP CONSTRAINT duplicate_evidence_kind_check;
ALTER TABLE duplicate_evidence ADD CONSTRAINT duplicate_evidence_kind_check
    CHECK (kind IN ('sha256', 'content', 'isbn', 'title_author'));
DROP TABLE file_lsh;
DROP TABLE file_signatures;
