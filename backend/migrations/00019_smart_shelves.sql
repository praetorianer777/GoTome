-- +goose Up
-- A smart shelf is a rule tree, as a filter on the library takes it, under a
-- name. What is on it is worked out each time it is looked at, never stored,
-- so a book is on it as soon as it matches. Rules on status and rating are
-- the viewer's own. Who may look at one is as for collections.
CREATE TABLE smart_shelves (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (btrim(name) <> ''),
    filter     jsonb NOT NULL,
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'shared')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX smart_shelves_owner_idx ON smart_shelves (owner_id);
CREATE INDEX smart_shelves_shared_idx ON smart_shelves (id) WHERE visibility = 'shared';

-- +goose Down
DROP TABLE smart_shelves;
