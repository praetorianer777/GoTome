-- +goose Up
-- A book sent to a device (#196): it waits in the device's feed until the
-- device downloads it there. One row per device and book; sending a book
-- the device already has queues it again.
CREATE TABLE deliveries (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    device_id    uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    book_id      uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    queued_at    timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    UNIQUE (device_id, book_id)
);

CREATE INDEX deliveries_queued_idx ON deliveries (device_id, queued_at) WHERE delivered_at IS NULL;
CREATE INDEX deliveries_book_idx ON deliveries (book_id);

-- +goose Down
DROP TABLE deliveries;
