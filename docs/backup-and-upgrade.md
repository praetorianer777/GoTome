# Backing up, restoring and upgrading

An installation keeps what matters in three places:

| What | Where | In a backup |
|---|---|---|
| The catalogue, accounts, reading state, shelves, settings | the database (`db-data` volume) | yes, without what GOtome makes again by itself |
| Covers and managed libraries | GOtome's files (`app-data` volume, `/data`) | yes, without the embedding model, which is downloaded again |
| The key stored secrets are sealed with | `secrets` volume, `secret-key` | yes |
| External libraries | your own folders, mounted into the app | no: back them up as you back up any folder |

The database password in the `secrets` volume is not needed: a restored
installation gets a new one.

Without the secret key, the stored secrets (metadata provider keys, the
identity provider's client secret) cannot be opened, and GOtome does not
start with another key over them. Keep the key as safe as the rest:
whoever has it and the database can open those secrets.

## Backing up

`deploy/backup.sh` copies a running installation into an empty folder. Run
it where `docker compose` finds the installation: the folder of its compose
file, or with `COMPOSE_FILE` and `COMPOSE_PROJECT_NAME` set.

```bash
cd deploy
./backup.sh /backups/gotome-$(date +%F)
```

GOtome keeps running. The folder then holds:

- `database.dump`: the database in `pg_dump`'s custom format. The text of the
  books (`book_chunks`, `search_words`), the duplicate signatures
  (`file_signatures`, `file_lsh`), the vectors (`book_vectors`) and the
  cache of what metadata sources answered (`provider_records`) are left out:
  GOtome makes them again from the files, and they are most of the database.
- `data.tar`: covers and managed libraries.
- `secret-key`, readable by its owner alone.
- `VERSION`: the version that made the backup.

The database is copied before the files, so files added meanwhile are only
more than the database names; the next scan takes them in.

## Restoring

`deploy/restore.sh` brings a backup back into an installation that has never
started: a new folder with the compose file, or the old one after
`docker compose down -v`, which deletes its volumes. It refuses one that has
a secret key or a database already.

```bash
cd deploy
./restore.sh /backups/gotome-2026-10-06
```

It puts the secret key in place before `init` runs (which then only adds a
new database password), restores the database into a fresh one, restores
the files, and starts GOtome. Mount external libraries where they were
before: their books are found by their paths.

What was left out is made again in the background: every text file is read
again, which fills the full-text search, the duplicate signatures and, if
embedding is on, the vectors. The jobs page shows the work; until it is
done the search finds fewer books. The image may be newer than the one
that made the backup: GOtome brings the database up to date when it starts.

## Upgrading

Back up first. Then pull the new image and start it:

```bash
cd deploy
docker compose pull
docker compose up -d
```

At start GOtome applies its database migrations, and updates and rebuilds the
search index if the database image brings a new `pg_search` or text is cut
differently, as [search-index.md](search-index.md) describes. Search answers
throughout, from what it has.

The database image is pinned in the compose file. A release that changes it
says so in the changelog. Within Postgres 18 the new image starts on the
existing volume. A new major version of Postgres cannot read the old one's
data: back up with the old compose file, then restore into a new
installation with the new one.

Going back to an older version is restoring a backup made by it: migrations
only go forward.

## What is tested

`make upgrade-test`, which CI runs on every pull request, sets up an
installation on the previous release's image (before the first release, on
the image of the commit the change started from), backs it up, upgrades it
in place, then deletes it and restores the backup on the new image. Both
must sign in, hold the uploaded book and its file unchanged, keep a secret
setting, and find the book by a word of its text.
