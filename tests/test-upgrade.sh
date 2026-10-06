#!/usr/bin/env bash
# Upgrades an installation of an earlier image to this one, and restores its
# backup into a fresh installation of this one, as docs/backup-and-upgrade.md
# tells an operator to:
#
#   1. The earlier image is set up with an account, a managed library holding
#      an EPUB, and a secret setting, and finds the book by its text.
#   2. deploy/backup.sh backs it up while it runs.
#   3. The installation is started on this image: the same account, book,
#      search and secret.
#   4. It is deleted, volumes and all, and deploy/restore.sh brings the backup
#      back on this image: the same again, the search made anew, and the
#      book's file the one uploaded.
#
#   tests/test-upgrade.sh <earlier image> <this image> <compose project>
#
# make upgrade-test picks the images and the project. Docker picks the port.
set -euo pipefail
cd "$(dirname "$0")/.."

usage="usage: $0 <earlier image> <this image> <compose project>"
previous=${1:?$usage}
current=${2:?$usage}
export COMPOSE_PROJECT_NAME=${3:?$usage}
export COMPOSE_FILE=$PWD/deploy/docker-compose.yml:$PWD/deploy/docker-compose.dev.yml
export GOTOME_ENV=development
# Any free port: the test is not for people to look at.
export GOTOME_PORT=0

api=
# reach finds the port Docker gave the app, which every start changes.
reach() {
  api=http://localhost:$(docker compose port app 8080 | cut -d: -f2)/api/v1
}
work=$(mktemp -d)
cleanup() {
  status=$?
  if [[ $status != 0 ]]; then
    docker compose logs --no-color --tail 80 app || true
  fi
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

book="e2e/fixtures/books/Jane Austen/Emma.epub"
# A word of the novel's text, not of its title or metadata: only the
# full-text search finds it.
word=Woodhouse
password=upgrade-test-only
token=upgrade-test-token

step() { echo "▶ $*"; }
fail() {
  echo "✗ $*" >&2
  exit 1
}

call() {
  local method=$1 path=$2
  shift 2
  curl -fsS -b "$work/cookies" -c "$work/cookies" -X "$method" "$api$path" "$@"
}
json() { call "$@" -H 'Content-Type: application/json'; }

# found waits until the full-text search finds the book, which takes the
# text being read and cut into chunks first.
found() {
  for _ in $(seq 90); do
    if [[ $(call GET "/search?q=$word" | jq '.books | length') == 1 ]]; then
      return 0
    fi
    sleep 2
  done
  fail "the search does not find the book by \"$word\" ($1)"
}

# holds checks what every stage must still hold, signed in afresh.
holds() {
  rm -f "$work/cookies"
  json POST /auth/login -d "{\"username\":\"admin\",\"password\":\"$password\"}" >/dev/null ||
    fail "the administrator cannot sign in ($1)"
  [[ $(call GET /books | jq '.books | length') == 1 ]] || fail "the library does not hold the one book ($1)"
  found "$1"
  [[ $(call GET /settings | jq '[.settings[] | select(.key == "metadata.hardcoverToken")][0].isSet') == true ]] ||
    fail "the secret setting is gone ($1)"
  local file
  file=$(call GET "/books/$(call GET /books | jq -r '.books[0].id')" | jq -r '.files[0].id')
  [[ $(call GET "/files/$file/download" | sha256sum | cut -d' ' -f1) == $(sha256sum "$book" | cut -d' ' -f1) ]] ||
    fail "the book's file is not the one uploaded ($1)"
}

step "Setting up $previous"
GOTOME_IMAGE=$previous docker compose up -d --wait --quiet-pull
reach
json POST /setup -d "{\"username\":\"admin\",\"password\":\"$password\"}" >/dev/null
library=$(json POST /libraries -d '{"name":"Managed","mode":"managed","visibility":"shared"}' | jq -r .id)
call POST "/libraries/$library/uploads" -F "file=@$book" >/dev/null
json PATCH /settings -d "{\"values\":{\"metadata.hardcoverToken\":\"$token\"}}" >/dev/null
holds "$previous"

step "Backing it up"
deploy/backup.sh "$work/backup"
for file in database.dump data.tar secret-key VERSION; do
  [[ -s $work/backup/$file ]] || fail "the backup has no $file"
done
# Read as SQL by the database image's pg_restore: the dump is compressed.
docker compose exec -T db pg_restore -f - <"$work/backup/database.dump" >"$work/backup.sql"
grep -q 'metadata.hardcoverToken' "$work/backup.sql" || fail "the backup has no settings"
if grep -qr "$token" "$work/backup.sql" "$work/backup/data.tar"; then
  fail "the backup holds the secret setting unsealed"
fi

step "Upgrading it to $current"
GOTOME_IMAGE=$current docker compose up -d --wait
reach
holds "upgraded"

step "Restoring the backup into a new installation of $current"
docker compose down -v
GOTOME_IMAGE=$current deploy/restore.sh "$work/backup"
reach
holds "restored"

echo "✓ Upgraded from $previous and restored its backup on $current"
