-- +goose Up
-- Feeds people follow for new books (#70): RSS or Atom at an address the
-- person typed, with the words or authors an item must name.

-- A person's feed. Nobody else's: two people with one address fetch it
-- each, as each has their own filters and their own past of it.
CREATE TABLE feed_trackers (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    url           text NOT NULL CHECK (url <> ''),
    -- The feed's own title, from its last good answer.
    title         text,
    -- An item must name one of the keywords, if there are any, and one of
    -- the authors, if there are any; both compared by their keys.
    keywords      text[] NOT NULL DEFAULT '{}',
    authors       text[] NOT NULL DEFAULT '{}',
    -- What the feed's last good answer said to ask it with next time.
    etag          text,
    last_modified text,
    -- When the feed last answered, with items or as unchanged; NULL until
    -- its first answer, whose items are its past and not news.
    checked_at    timestamptz,
    -- The failures in a row since, and what the last one was.
    failures      integer NOT NULL DEFAULT 0,
    last_error    text,
    -- When it is fetched next; NULL once it failed too often, until its
    -- owner asks for it again.
    next_fetch_at timestamptz DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, url)
);
CREATE INDEX feed_trackers_due ON feed_trackers (next_fetch_at) WHERE next_fetch_at IS NOT NULL;

-- Each item a feed listed, once, by its guid or id, else its link: what is
-- told of it is told once however often the feed lists it.
CREATE TABLE feed_items (
    tracker_id    uuid NOT NULL REFERENCES feed_trackers (id) ON DELETE CASCADE,
    item_key      text NOT NULL CHECK (item_key <> ''),
    title         text NOT NULL,
    link          text,
    authors       text[] NOT NULL DEFAULT '{}',
    published_at  timestamptz,
    -- Whether it names what the tracker's filters ask for.
    matched       boolean NOT NULL,
    -- Listed in the feed's first answer: never told of.
    backlog       boolean NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tracker_id, item_key)
);
CREATE INDEX feed_items_matched ON feed_items (tracker_id, first_seen_at) WHERE matched;

-- +goose Down
DROP TABLE feed_items;
DROP TABLE feed_trackers;
