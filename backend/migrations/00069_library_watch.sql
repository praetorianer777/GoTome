-- +goose Up
-- Whether GOtome watches the library's folder and scans it by itself soon
-- after files arrive or go (#409).
ALTER TABLE libraries ADD COLUMN watch boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE libraries DROP COLUMN watch;
