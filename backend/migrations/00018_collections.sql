-- +goose Up
-- A collection is a shelf a person fills by hand, in an order they choose.
-- Private ones are their owner's alone; a shared one every signed-in person
-- may look at, but it shows each of them only the books of libraries they
-- may see: a collection never grants access to a book.
CREATE TABLE collections (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (btrim(name) <> ''),
    description text NOT NULL DEFAULT '',
    visibility  text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'shared')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX collections_owner_idx ON collections (owner_id);
CREATE INDEX collections_shared_idx ON collections (id) WHERE visibility = 'shared';

-- Positions order a collection's books. They need not be consecutive, and
-- are not unique, so that a reorder can hand the same positions round in
-- one statement.
CREATE TABLE collection_items (
    collection_id uuid NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    book_id       uuid NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    position      integer NOT NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, book_id)
);

CREATE INDEX collection_items_order_idx ON collection_items (collection_id, position);
CREATE INDEX collection_items_book_idx ON collection_items (book_id);

-- +goose Down
DROP TABLE collection_items;
DROP TABLE collections;
