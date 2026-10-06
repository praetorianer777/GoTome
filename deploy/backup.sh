#!/usr/bin/env bash
# Backs a running GOtome up into an empty folder:
#
#   database.dump  the database, without what GOtome makes again by itself
#   data.tar       GOtome's own files: covers and managed libraries
#   secret-key     the key the stored secrets (provider keys and tokens, the
#                  identity provider's client secret) are sealed with
#   VERSION        the version that made the backup
#
#   deploy/backup.sh /path/to/empty/folder
#
# Run it where `docker compose` finds the installation: in the folder of its
# compose file, or with COMPOSE_FILE and COMPOSE_PROJECT_NAME set. GOtome
# keeps running. The database is copied first, so files that arrive while
# the rest is copied are only more than it names, which a scan takes in.
#
# Not in it: external libraries, which are your own folders, and the
# embedding model, which is downloaded again. Keep the secret key as safe as
# the rest: whoever has it and the database can open the stored secrets.
set -euo pipefail

dir=${1:?usage: $0 <empty folder for the backup>}
mkdir -p "$dir"
if [[ -n $(ls -A "$dir") ]]; then
  echo "$dir is not empty" >&2
  exit 1
fi
umask 077

# Rows GOtome makes again from the files: the text cut into chunks and the
# words of the search, the duplicate signatures, the vectors, and the cache of
# what metadata sources answered. Their tables stay in the dump, empty;
# restore.sh has the text read again, which fills them.
derived=(book_chunks search_words file_signatures file_lsh book_vectors provider_records)
exclude=()
for table in "${derived[@]}"; do
  exclude+=("--exclude-table-data=$table")
done

echo "Copying the database"
docker compose exec -T db pg_dump -U gotome -d gotome --format=custom "${exclude[@]}" >"$dir/database.dump"

echo "Copying GOtome's files"
docker compose exec -T app tar -C /data --exclude=./models -cf - . >"$dir/data.tar"

echo "Copying the secret key"
docker compose exec -T app cat /secrets/secret-key >"$dir/secret-key"

docker compose exec -T app gotome version >"$dir/VERSION"
echo "Backed up into $dir: $(du -sh "$dir" | cut -f1)"
