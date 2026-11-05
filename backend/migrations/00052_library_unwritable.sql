-- +goose Up
-- A writable library whose folder GOtome found it cannot write to (#299),
-- such as a mount that turned read-only: since when, and the system's
-- reason. Set once, which is when the library's managers are told, and
-- cleared when a scan finds the folder writable again or someone changes
-- whether the library is writable.
ALTER TABLE libraries
    ADD COLUMN unwritable_since  timestamptz,
    ADD COLUMN unwritable_reason text;

-- +goose Down
ALTER TABLE libraries
    DROP COLUMN unwritable_since,
    DROP COLUMN unwritable_reason;
