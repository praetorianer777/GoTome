-- +goose Up
-- The version each derived structure was built with: "pg_search" for the
-- search index, "chunks" for how text is cut into chunks. A version that
-- differs at start is what triggers a rebuild.
CREATE TABLE index_versions (
    name       text PRIMARY KEY,
    version    text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE index_versions;
