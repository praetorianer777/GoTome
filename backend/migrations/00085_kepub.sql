-- +goose Up
-- A Kobo reader may be sent a KEPUB made from the EPUB (#411), as its owner
-- chooses per device; off until measured on a real reader.
ALTER TABLE devices ADD COLUMN kepub boolean NOT NULL DEFAULT false;

-- Copies are of two kinds: the EPUB with its findings fixed (#501) and the
-- KEPUB made from the file. Both lie in the same cache, by the hash of the
-- file they were made from; fix_version is the version of the maker of the
-- kind.
ALTER TABLE device_copies ADD COLUMN kind text NOT NULL DEFAULT 'fix' CHECK (kind IN ('fix', 'kepub'));
ALTER TABLE device_copies DROP CONSTRAINT device_copies_pkey;
ALTER TABLE device_copies ADD PRIMARY KEY (source_sha256, kind, fix_version);

-- +goose Down
DELETE FROM device_copies WHERE kind <> 'fix';
ALTER TABLE device_copies DROP CONSTRAINT device_copies_pkey;
ALTER TABLE device_copies ADD PRIMARY KEY (source_sha256, fix_version);
ALTER TABLE device_copies DROP COLUMN kind;
ALTER TABLE devices DROP COLUMN kepub;
