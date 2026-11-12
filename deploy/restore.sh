#!/usr/bin/env bash
# Restores a backup made by backup.sh into an installation that has never
# started, and starts it:
#
#   deploy/restore.sh /path/to/backup
#
# Run it where `docker compose` finds the new installation, as backup.sh
# does, before its first `docker compose up`. It refuses one that already
# has a secret key or a database with tables in it.
#
# The image may be newer than the one that made the backup: GOtome brings
# the database up to date when it starts. The search, the duplicate
# signatures and the vectors are made again in the background from the
# files; until then the search finds less. External libraries must be
# mounted where they were.
set -euo pipefail

dir=${1:?usage: $0 <folder of a backup made by backup.sh>}
for file in database.dump data.tar secret-key; do
  if [[ ! -s $dir/$file ]]; then
    echo "$dir/$file is missing or empty: not a backup made by backup.sh" >&2
    exit 1
  fi
done

# init only adds what is missing, so the key put there first is the one the
# installation keeps; it writes the database password beside it.
if docker compose run --rm --no-deps -T --entrypoint sh init -c 'test -e /secrets/secret-key'; then
  echo "This installation has a secret key already: restore into one that has never started." >&2
  exit 1
fi
echo "Putting the secret key in place"
docker compose run --rm --no-deps -T --entrypoint sh init -c '
  umask 077
  cat >/secrets/.secret-key &&
    chown 10001:10001 /secrets/.secret-key &&
    chmod 0400 /secrets/.secret-key &&
    mv /secrets/.secret-key /secrets/secret-key' <"$dir/secret-key"

echo "Starting the database"
docker compose up -d --wait db
# The database image comes with tables of its own extensions; GOtome's
# migrations table says GOtome has been here.
if [[ $(docker compose exec -T db psql -U gotome -d gotome -Atc "SELECT to_regclass('public.goose_db_version') IS NOT NULL") == t ]]; then
  echo "The database holds an installation already: restore into one that has never started." >&2
  exit 1
fi

echo "Restoring the database"
# The database image puts its extensions into every new database; the dump
# brings the ones GOtome uses, so it goes into one made from template0.
docker compose exec -T db psql -U gotome -d postgres -v ON_ERROR_STOP=1 -qc "DROP DATABASE gotome" -c "CREATE DATABASE gotome TEMPLATE template0"
docker compose exec -T db pg_restore -U gotome -d gotome --no-owner --exit-on-error <"$dir/database.dump"
# The chunks were left out of the backup: every text file is read again,
# which makes the search, the signatures and the vectors anew.
docker compose exec -T db psql -U gotome -d gotome -v ON_ERROR_STOP=1 -qc "UPDATE book_files SET chunked_at = NULL"

echo "Restoring GOtome's files"
docker compose run --rm --no-deps -T --entrypoint tar app -C /data -xf - <"$dir/data.tar"

echo "Starting GOtome"
docker compose up -d --wait
echo "Restored from $dir ($(cat "$dir/VERSION" 2>/dev/null || echo "version unknown")). The search is made again in the background."
