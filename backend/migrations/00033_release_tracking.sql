-- +goose Up
-- New and upcoming books of the authors and series people follow (#69).

-- An author or a series as the providers are asked about it, once for
-- everyone who follows it. Names are compared by their key, as books'.
CREATE TABLE release_subjects (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    kind       text NOT NULL CHECK (kind IN ('author', 'series')),
    name       text NOT NULL CHECK (btrim(name) <> ''),
    name_key   text NOT NULL CHECK (name_key <> ''),
    -- When it was last asked about, by every provider that answered.
    polled_at  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, name_key)
);
CREATE INDEX release_subjects_polled ON release_subjects (polled_at NULLS FIRST);

-- A person following a subject. A subject nobody follows any more goes,
-- with what was found for it.
CREATE TABLE trackers (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subject_id uuid NOT NULL REFERENCES release_subjects (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, subject_id)
);
CREATE INDEX trackers_subject ON trackers (subject_id);

-- Which provider has answered about a subject, and since when: what a
-- provider lists the first time it answers is the subject's past, not news.
CREATE TABLE release_polls (
    subject_id uuid NOT NULL REFERENCES release_subjects (id) ON DELETE CASCADE,
    provider   text NOT NULL,
    first_at   timestamptz NOT NULL DEFAULT now(),
    last_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject_id, provider)
);

-- A book of a subject, once however many providers list it: dedupe_key is
-- the key of its title. Its date is as exact as the best source says
-- (precision), and day, month or year it is the first day of that span.
CREATE TABLE releases (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    subject_id    uuid NOT NULL REFERENCES release_subjects (id) ON DELETE CASCADE,
    dedupe_key    text NOT NULL CHECK (dedupe_key <> ''),
    title         text NOT NULL,
    authors       text[] NOT NULL DEFAULT '{}',
    -- The authors' keys in both orders of their names, compared with
    -- authors.name_key to tell whether someone has the book already.
    author_keys   text[] NOT NULL DEFAULT '{}',
    series        text,
    series_index  double precision,
    release_date  date,
    precision     text CHECK (precision IN ('day', 'month', 'year')),
    isbns         text[] NOT NULL DEFAULT '{}',
    provider      text NOT NULL,
    provider_id   text NOT NULL,
    cover_url     text,
    -- Found by a provider's first answer about the subject: never told of
    -- as announced, only, once its day comes, as out.
    backlog       boolean NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((release_date IS NULL) = (precision IS NULL)),
    UNIQUE (subject_id, dedupe_key)
);
CREATE INDEX releases_date ON releases (release_date) WHERE precision = 'day';

-- How exact a release date is: none, a year, a month, a day.
CREATE FUNCTION precision_rank(p text) RETURNS integer
    LANGUAGE sql IMMUTABLE
    RETURN coalesce(array_position(ARRAY['year', 'month', 'day'], p), 0);

-- What each person was told of a release, so that each is told once.
CREATE TABLE release_notices (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    release_id uuid NOT NULL REFERENCES releases (id) ON DELETE CASCADE,
    stage      text NOT NULL CHECK (stage IN ('announced', 'released')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, release_id, stage)
);

-- +goose Down
DROP TABLE release_notices;
DROP TABLE releases;
DROP FUNCTION precision_rank;
DROP TABLE release_polls;
DROP TABLE trackers;
DROP TABLE release_subjects;
