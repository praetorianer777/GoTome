-- +goose Up
-- A bulk change may fix the books' EPUBs for e-readers (#233).
ALTER TABLE bulk_changes DROP CONSTRAINT bulk_changes_action_check;
ALTER TABLE bulk_changes ADD CONSTRAINT bulk_changes_action_check
    CHECK (action IN ('edit', 'fetch', 'writeBack', 'fix'));

-- +goose Down
DELETE FROM bulk_changes WHERE action = 'fix';
ALTER TABLE bulk_changes DROP CONSTRAINT bulk_changes_action_check;
ALTER TABLE bulk_changes ADD CONSTRAINT bulk_changes_action_check
    CHECK (action IN ('edit', 'fetch', 'writeBack'));
