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

- `database.dump`: the database in `pg_dump`'s custom format. Where the
  books' text is (`book_chunks`, `search_words`, `fulltext_outbox`), the
  duplicate signatures (`file_signatures`, `file_lsh`), the vectors
  (`book_vectors`) and the cache of what metadata sources answered
  (`provider_records`) are left out: GOtome makes them again from the
  files, and they are most of the database.
- `data.tar`: covers and managed libraries, without the full-text index
  (`search/`), which GOtome makes again from the files.
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

Back up first. If `.env` pins `GOTOME_IMAGE` to a version, change it to the
new one. Then pull the new image and start it:

```bash
cd deploy
docker compose pull
docker compose up -d
```

At start GOtome applies its database migrations, and builds the search
index again if the new version stores it differently or cuts text
differently, as [search-index.md](search-index.md) describes. Search answers
throughout, from what it has.

The database image is pinned in the compose file. A release that changes it
says so in the changelog. Within Postgres 18 the new image starts on the
existing volume. A new major version of Postgres cannot read the old one's
data: back up with the old compose file, then restore into a new
installation with the new one.

Going back to an older version is restoring a backup made by it: migrations
only go forward.

## Moving to another machine

Backing up on the old machine and restoring on the new one moves an
installation, as does copying its volumes. Either way the database brings
its settings along: Postgres keeps them in the `db-data` volume, and the
database image sized its memory for the machine the database was first
made on (`postgresql.conf`). On a smaller machine, Postgres may then take
more memory than there is, and the kernel kills it, in the middle of a
VACUUM or a large query; it recovers and is killed again.

GOtome checks this at start and every hour, logs a warning while something
does not fit, and shows it to administrators on the overview, with the
commands to run. It compares with the memory GOtome's container sees: its
limit, or the machine's where it has none. Postgres's container may have a
limit of its own, which GOtome cannot see. What is counted:

| Part | Memory |
|---|---|
| Postgres | `shared_buffers`, one `maintenance_work_mem` (a VACUUM), and `work_mem` for each of GOtome's connections (16) |
| The app | about 512 MB: the server, the full-text index's writer, the programs that read files |
| The embedding model, when it runs on the CPU | multilingual-e5-small about 700 MB, its fp32 weights 1.1 GB, bge-m3 about 3 GB |

Together they may take 80% of the memory; the rest is for the kernel and
the file cache, which Postgres and the full-text index read through. A
machine with less memory than bge-m3 needs runs a smaller model, or bge-m3
on an embedding server ([embedding-server.md](embedding-server.md)).

The settings are changed with `ALTER SYSTEM`, which writes them to
`postgresql.auto.conf` in the volume, where they win over
`postgresql.conf`; a restart of the database applies them:

```bash
cd deploy
docker compose exec db psql -U gotome -d gotome -c "ALTER SYSTEM SET shared_buffers = '1024MB';"
docker compose restart db
```

What fits, by the machine's memory, with the default embedding model on
the CPU (with bge-m3 there, the overview suggests less):

| Memory | `shared_buffers` | `maintenance_work_mem` | `work_mem` | `effective_cache_size` |
|---|---|---|---|---|
| 2 GB | 200MB | 100MB | 6MB | 1024MB |
| 4 GB | 800MB | 256MB | 16MB | 2048MB |
| 8 GB | 1600MB | 256MB | 16MB | 4096MB |
| 16 GB and more | a fifth of it | 256MB | 16MB | half of it |

Keep `max_parallel_workers` to the number of CPUs, and
`max_parallel_workers_per_gather` and `max_parallel_maintenance_workers`
to half of them: each worker takes its own `work_mem`. `ALTER SYSTEM RESET
<name>` goes back to the value in `postgresql.conf`.

### Disk space

Postgres stops when its disk is full, and starts again only once there is
room to recover. GOtome warns when the disk of its data directory has less
than 10 GB or 10% free, or less than the database and the full-text index
take together. Docker usually keeps the database's volume on the same disk;
where it does not, watch that disk as well. Leave room for:

- the write-ahead log, which grows during large imports before a
  checkpoint recycles it;
- VACUUM, and above all `VACUUM FULL` or a rebuilt index, which write a
  table's or an index's new copy before removing the old one;
- the full-text index under `/data/search`, which is built again beside
  the old one when its engine changes, and so needs its size again;
- new images: each update brings an app image of about 1 GB, and the old
  ones stay until they are removed:

```bash
docker image prune
```

A backup made with `backup.sh` needs room on its own disk too: about the
size of the books and the database without the text.

## What is tested

`make upgrade-test`, which CI runs on every pull request, sets up an
installation on the previous release's image (before the first release, on
the image of the commit the change started from), backs it up, upgrades it
in place, then deletes it and restores the backup on the new image. Both
must sign in, hold the uploaded book and its file unchanged, keep a secret
setting, and find the book by a word of its text.
