-- +goose Up
-- Libraries: where books live on disk, and the boundary of who may see them.

CREATE TABLE libraries (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL CHECK (btrim(name) <> ''),
    -- The directory inside the app container. No two libraries share one.
    root_path  text NOT NULL UNIQUE,
    -- managed: GOtome owns the layout and uploads land here.
    -- external: a folder somebody else arranges, scanned where it lies.
    mode       text NOT NULL CHECK (mode IN ('managed', 'external')),
    -- Whether GOtome may change files here. Always true for managed ones.
    writable   boolean NOT NULL,
    visibility text NOT NULL CHECK (visibility IN ('shared', 'private')),
    owner_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (mode <> 'managed' OR writable)
);

CREATE UNIQUE INDEX libraries_name_key ON libraries (lower(name));

-- Who may see a private library besides its owner.
CREATE TABLE library_members (
    library_id uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (library_id, user_id)
);

CREATE INDEX library_members_user_id_idx ON library_members (user_id);

-- The one definition of which libraries a person may see. Every query that
-- returns anything belonging to a library joins or filters through it, so the
-- rule cannot be written differently in two places:
--
--   a shared library is seen by everybody who is signed in;
--   a private one by its owner and its members;
--   sees_all is for administrators, who run the storage all of them live on.
--
-- A plain SQL function, so the planner inlines it into the calling query.
-- +goose StatementBegin
CREATE FUNCTION visible_library_ids(viewer uuid, sees_all boolean)
RETURNS SETOF uuid
LANGUAGE sql
STABLE
AS $$
    SELECT l.id
    FROM libraries l
    WHERE sees_all
       OR l.visibility = 'shared'
       OR l.owner_id = viewer
       OR EXISTS (
            SELECT 1 FROM library_members m
            WHERE m.library_id = l.id AND m.user_id = viewer
       )
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION visible_library_ids(uuid, boolean);
DROP TABLE library_members;
DROP TABLE libraries;
