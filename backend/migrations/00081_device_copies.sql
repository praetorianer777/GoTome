-- +goose Up
-- The copy of an EPUB that is sent to a device in place of the library's
-- file, with its e-reader findings fixed (#501). The bytes lie under
-- GOTOME_DATA_DIR/device-copies, named by source_sha256 and fix_version;
-- the row says what they are, so that a device that holds the copy is
-- found again by its hashes. Derived: a backup leaves the rows out.
--   ready       the copy lies on disk
--   evicted     the copy was removed to make room; its hashes stay
--   unfixable   Fix found nothing to change, or could not read the file
CREATE TABLE device_copies (
    source_sha256 bytea NOT NULL,
    fix_version   smallint NOT NULL,
    state         text NOT NULL CHECK (state IN ('ready', 'evicted', 'unfixable')),
    sha256        bytea,
    size_bytes    bigint,
    koreader_md5  text,
    made_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_sha256, fix_version),
    CHECK (state = 'unfixable' OR (sha256 IS NOT NULL AND size_bytes IS NOT NULL AND koreader_md5 IS NOT NULL))
);

CREATE INDEX device_copies_sha256_idx ON device_copies (sha256) WHERE sha256 IS NOT NULL;
CREATE INDEX device_copies_koreader_md5_idx ON device_copies (koreader_md5) WHERE koreader_md5 IS NOT NULL;

-- Whether the last download of a book to a device was a fixed copy.
CREATE TABLE device_sends (
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    book_id   uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    fixed     boolean NOT NULL,
    sent_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, book_id)
);

-- +goose Down
DROP TABLE device_sends;
DROP TABLE device_copies;
