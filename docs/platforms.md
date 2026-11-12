# Running GOtome on a NAS, on arm64 and with little memory

GOtome is built for home servers: a NAS, a small PC, a Raspberry Pi. The
image is published for amd64 and arm64 under one name, and Docker takes
the one that fits. What follows is what differs from the general
[getting-started.md](getting-started.md).

## How much machine

| | At least | Comfortable |
|---|---|---|
| Memory | 2 GB | 4 GB; 8 GB for the `bge-m3` model on the CPU |
| CPU | 2 cores, 64-bit (amd64 or arm64) | 4 cores |
| Disk for the database and `/data` | an SSD, if any of it is | |

The books themselves may be on spinning disks or a network share. The
database volume (`db-data`) and the data volume (`app-data`, with the
search index) are read at random, many small reads at a time: on an SSD
the library answers in milliseconds, on a spinning disk it may take
seconds.

32-bit systems (armv7, i386) are not supported. A Raspberry Pi needs a
64-bit operating system.

## On a NAS

The compose file works as it is on any NAS that runs Docker Compose. The
points that differ:

- **Paths.** Mount the share that holds your books by its path on the
  NAS: `/volume1/books:/books:ro` on Synology, `/mnt/user/books:/books:ro`
  on Unraid, `/share/books:/books:ro` on QNAP,
  `/mnt/<pool>/books:/books:ro` on TrueNAS SCALE.
- **Permissions.** GOtome runs as user 10001, which is nobody on the NAS.
  It reads a share whose files every user may read, which is the default
  on most systems. Where a share is restricted (Synology's ACLs, TrueNAS's
  datasets), give the user ID 10001 read access, or read and write for a
  writable library, or make the files readable by everyone.
- **Synology Container Manager**: **Project**, **Create**, choose the
  `deploy` folder (copied onto the NAS, with `.env`) as the path, and let
  it use the existing `docker-compose.yml`.
- **Unraid**: with the Docker Compose Manager plugin, add a stack, paste
  the compose file, and put the `.env` values into the stack's
  environment file.
- **TrueNAS SCALE**: install it as a custom app from YAML, and paste the
  compose file with your values written in place of the `${...}`
  references.
- **Docker's own volumes** (`app-data`, `db-data`, `secrets`) are kept
  where the NAS keeps Docker's data, usually on its first volume. If that
  is a spinning disk and the NAS has an SSD, put Docker's data there, or
  replace `app-data` and `db-data` with folders on the SSD, such as
  `/volume2/docker/gotome/data:/data` and
  `/volume2/docker/gotome/db:/var/lib/postgresql`. Make the folder for
  `/data` belong to user 10001 first (`chown 10001 /volume2/docker/gotome/data`):
  unlike a named volume, a folder does not take the image's owner. Do not
  put the database on a network share.

Back up a NAS installation with `deploy/backup.sh`
([backup-and-upgrade.md](backup-and-upgrade.md)) into a folder on the NAS
that its own backup then takes along. A snapshot of the volumes while
Postgres runs is not a consistent backup of the database.

## On arm64

Everything runs on arm64, including the search engine and the language
model; CI builds and starts the whole stack on arm64 for every change.
What is slower on a small ARM board is working out similar books. That
is background work, and nothing else waits for it; see
[troubleshooting.md](troubleshooting.md#similar-books-on-slow-hardware).

## With little memory

GOtome's container, Postgres, and the language model share the machine's
memory. Postgres sizes itself to the machine it first starts on, and keeps
those settings in its volume; on a machine with less memory than that, or
with a container limit, it may take more than there is, and the kernel
kills it mid-query. It recovers and is killed again.

GOtome checks this when it starts and every ten minutes. Administrators
see the result on the **Overview** page, in the panel **This machine**:
the memory GOtome's container sees, the CPUs, what Postgres may take, the
model, and the free disk. When something does not fit, the panel says
what and gives the commands that fix it, ready to copy; the log says the
same. They change Postgres's settings with `ALTER SYSTEM`, which keeps
them in the database's volume, and restart it:

```bash
cd deploy
docker compose exec db psql -U gotome -d gotome -c "ALTER SYSTEM SET shared_buffers = '800MB';"
docker compose restart db
```

[backup-and-upgrade.md](backup-and-upgrade.md#moving-to-another-machine)
explains what is counted and lists what fits by memory size.

Beyond Postgres, on a machine of 2 to 4 GB:

- Keep the default model, `multilingual-e5-small` (about 700 MB), or turn
  **Work out what books are about** off: everything else works without it.
  Not `bge-m3`, which takes about 3 GB on the CPU; it can run on another
  machine's GPU ([embedding-server.md](embedding-server.md)).
- `GOTOME_EMBED_THREADS=1` keeps the model to one core
  ([configuration.md](configuration.md)).
- A memory limit on GOtome's container (`mem_limit` in an override file)
  is respected: GOtome holds its own memory under three quarters of it.
