-- +goose Up
-- Since when each model of embed.Specs, by the name embedding.model takes,
-- is no longer the chosen one (#357): its files and vectors are removed
-- once it has not been chosen for embedding.removeUnusedAfterDays. The
-- chosen model has no row. Kept here rather than read from the setting's
-- updated_at, which any save of the setting moves and which a setting
-- taken back to its default has none of.
CREATE TABLE unused_embedding_models (
    name  text PRIMARY KEY,
    since timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE unused_embedding_models;
