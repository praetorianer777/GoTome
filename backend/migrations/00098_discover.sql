-- +goose Up
-- Books beyond the libraries that a person's taste points to (#393). The
-- candidates and the runs are derived: they are asked of the providers again,
-- so a backup leaves them out. What a person dismissed is theirs.
CREATE TABLE discover_runs (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    -- Asked for from the page: the run starts again at once, not tomorrow.
    requested    boolean NOT NULL DEFAULT false
);

-- What a run has asked the providers, so that a pass cut off goes on where
-- it stopped and a run asks each subject once.
CREATE TABLE discover_asks (
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind     text NOT NULL CHECK (kind IN ('author', 'series', 'genre')),
    name_key text NOT NULL,
    asked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, name_key)
);

CREATE TABLE discover_candidates (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider      text NOT NULL,
    provider_id   text NOT NULL,
    -- What the provider calls the work, to tell editions of one from two books.
    work          text NOT NULL DEFAULT '',
    title         text NOT NULL,
    -- Keys by which libraries, wishes and dismissals are matched, as releases
    -- keep them: the title's key and the authors' in both orders.
    title_keys    text[] NOT NULL,
    author_keys   text[] NOT NULL DEFAULT '{}',
    isbns         text[] NOT NULL DEFAULT '{}',
    -- The provider's record, as a wish is made from it.
    record        jsonb NOT NULL,
    score         double precision NOT NULL,
    -- Why: author, series_next, series_gap, series_more or genre.
    reason_kind   text NOT NULL CHECK (reason_kind IN ('author', 'series_next', 'series_gap', 'series_more', 'genre')),
    reason_name   text NOT NULL,
    -- The best rating the person gave the author's books, 0 where none.
    reason_stars  smallint NOT NULL DEFAULT 0,
    because       text[] NOT NULL DEFAULT '{}',
    found_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider, provider_id)
);
CREATE INDEX discover_candidates_ranked ON discover_candidates (user_id, score DESC, id);

-- "Not interested" in a book no library holds: it is compared by the keys of
-- its title and its authors, as the books a person dismissed among the
-- recommendations of the libraries are.
CREATE TABLE discover_dismissals (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title_keys  text[] NOT NULL,
    author_keys text[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX discover_dismissals_user ON discover_dismissals (user_id);

-- +goose Down
DROP TABLE discover_dismissals;
DROP TABLE discover_candidates;
DROP TABLE discover_asks;
DROP TABLE discover_runs;
