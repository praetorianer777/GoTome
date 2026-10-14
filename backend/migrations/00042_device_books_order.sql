-- +goose Up
-- A device's page lists its books, the last sent first (#237).
CREATE INDEX deliveries_device_idx ON deliveries (device_id, queued_at DESC, id DESC);

-- +goose Down
DROP INDEX deliveries_device_idx;
