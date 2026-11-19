-- +goose Up
-- What a device follows (#545): a collection, a smart shelf, or the owner's
-- Reading or Want to read list. Every visible book with an ebook on a
-- source is queued for the device, and leaves it again when it leaves the
-- source, unless it was sent by hand.
CREATE TABLE device_sources (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    device_id      uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    kind           text NOT NULL CHECK (kind IN ('collection', 'smart', 'reading', 'wishlist')),
    collection_id  uuid REFERENCES collections (id) ON DELETE CASCADE,
    smart_shelf_id uuid REFERENCES smart_shelves (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'collection') = (collection_id IS NOT NULL)),
    CHECK ((kind = 'smart') = (smart_shelf_id IS NOT NULL))
);

CREATE UNIQUE INDEX device_sources_collection_idx ON device_sources (device_id, collection_id) WHERE collection_id IS NOT NULL;
CREATE UNIQUE INDEX device_sources_smart_idx ON device_sources (device_id, smart_shelf_id) WHERE smart_shelf_id IS NOT NULL;
CREATE UNIQUE INDEX device_sources_list_idx ON device_sources (device_id, kind) WHERE kind IN ('reading', 'wishlist');
CREATE INDEX device_sources_collection_ref_idx ON device_sources (collection_id) WHERE collection_id IS NOT NULL;
CREATE INDEX device_sources_smart_ref_idx ON device_sources (smart_shelf_id) WHERE smart_shelf_id IS NOT NULL;

-- A delivery is by hand unless a source made it: existing rows, and rows a
-- device recorded itself, are.
ALTER TABLE deliveries ADD COLUMN manual boolean NOT NULL DEFAULT true;

-- The sources a delivery is on. One that is not by hand and has none left
-- is taken off the device.
CREATE TABLE delivery_origins (
    delivery_id uuid NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
    source_id   uuid NOT NULL REFERENCES device_sources (id) ON DELETE CASCADE,
    PRIMARY KEY (delivery_id, source_id)
);

CREATE INDEX delivery_origins_source_idx ON delivery_origins (source_id);

-- +goose Down
DROP TABLE delivery_origins;
ALTER TABLE deliveries DROP COLUMN manual;
DROP TABLE device_sources;
