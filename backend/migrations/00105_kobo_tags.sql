-- +goose Up
-- What a Kobo reader was told of the person's collections (#543): each one
-- by its ID with the name and the books on the device it was last sent
-- with, so that a sync tells of a change once and of a removal once.
CREATE TABLE kobo_tags (
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    tag_id    uuid NOT NULL,
    name      text NOT NULL,
    book_ids  uuid[] NOT NULL DEFAULT '{}',
    synced_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, tag_id)
);

-- +goose Down
DROP TABLE kobo_tags;
