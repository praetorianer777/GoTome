#!/usr/bin/env bash
# Upgrades an installation of an earlier image to this one, and restores its
# backup into a fresh installation of this one, as docs/backup-and-upgrade.md
# tells an operator to:
#
#   1. The earlier image is set up with an administrator and a reader, a
#      managed library holding an EPUB with a cover, a place read in it, a
#      collection, a device, a notification and a secret setting, and finds
#      the book by its text.
#   2. deploy/backup.sh backs it up while it runs.
#   3. The installation is started on this image: all of that still there,
#      the device's token signing in to OPDS, Kobo's sync and KOReader's.
#   4. It is deleted, volumes and all, and deploy/restore.sh brings the backup
#      back on this image: the same again, the book's file and cover as they
#      were, and what the backup leaves out made anew: the search, and the
#      text's signature for duplicates. Vectors are not: the development
#      stack is offline and has no model to make them with.
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
cover=web/public/icon-512.png
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
sql() { docker compose exec -T db psql -U gotome -d gotome -Atc "$1"; }
sha() { sha256sum | cut -d' ' -f1; }
# signIn starts a session of its own as the user.
signIn() {
  rm -f "$work/cookies"
  json POST /auth/login -d "{\"username\":\"$1\",\"password\":\"$password\"}" >/dev/null ||
    fail "$1 cannot sign in ($2)"
}

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

# signed waits until the book's text is signed for duplicates, which the
# backup leaves out and a restore makes anew from the file. The fixture's
# text is too short for a signature of its own, which leaves the row.
signed() {
  for _ in $(seq 30); do
    if [[ $(sql "SELECT count(*) FROM file_signatures") == 1 ]]; then
      return 0
    fi
    sleep 2
  done
  fail "the book's text is not signed ($1)"
}

# holds checks what every stage must still hold, signed in afresh.
holds() {
  signIn reader "$1"
  [[ $(call GET /auth/me | jq -r .role) == reader ]] || fail "the reader is not a reader ($1)"
  signIn admin "$1"
  [[ $(call GET /books | jq '.books | length') == 1 ]] || fail "the library does not hold the one book ($1)"
  found "$1"
  signed "$1"
  [[ $(call GET /settings | jq '[.settings[] | select(.key == "metadata.hardcoverToken")][0].isSet') == true ]] ||
    fail "the secret setting is gone ($1)"
  local id file
  id=$(call GET /books | jq -r '.books[0].id')
  file=$(call GET "/books/$id" | jq -r '.files[0].id')
  [[ $(call GET "/files/$file/download" | sha) == $(cat "$work/file.sha") ]] ||
    fail "the book's file is not the one backed up ($1)"
  [[ $(call GET "/books/$id/covers/large" | sha) == $(cat "$work/cover.sha") ]] ||
    fail "the book's cover is not the one backed up ($1)"
  [[ $(call GET "/books/$id/progress" | jq .ebook.fraction) == 0.42 ]] ||
    fail "the place read in the book is gone ($1)"
  [[ $(call GET /collections | jq -c '[.collections[] | {name, books}]') == '[{"name":"Favourites","books":1}]' ]] ||
    fail "the collection is gone ($1)"
  [[ $(call GET /notifications | jq -r '[.notifications[].kind] | index("device.requested")') != null ]] ||
    fail "the notification is gone ($1)"
  local device
  device=$(cat "$work/device")
  curl -fsS -u "admin:$device" "$api/opds" >/dev/null || fail "the device's token does not open the OPDS catalogue ($1)"
  curl -fsS "$api/kobo/$device/v1/initialization" >/dev/null || fail "the device's token does not open Kobo's sync ($1)"
  curl -fsS -H "x-auth-user: admin" -H "x-auth-key: $(printf '%s' "$device" | md5sum | cut -d' ' -f1)" \
    "$api/kosync/users/auth" >/dev/null || fail "the device's token does not sign in to KOReader's sync ($1)"
}

step "Setting up $previous"
GOTOME_IMAGE=$previous docker compose up -d --wait --quiet-pull
reach
json POST /setup -d "{\"username\":\"admin\",\"password\":\"$password\"}" >/dev/null
library=$(json POST /libraries -d '{"name":"Managed","mode":"managed","visibility":"shared"}' | jq -r .id)
id=$(call POST "/libraries/$library/uploads" -F "file=@$book" | jq -r .bookId)
json PATCH /settings -d "{\"values\":{\"metadata.hardcoverToken\":\"$token\"}}" >/dev/null
json POST /users -d "{\"username\":\"reader\",\"password\":\"$password\",\"role\":\"reader\"}" >/dev/null
found "$previous"
file=$(call GET "/books/$id" | jq -r '.files[0].id')
json PUT "/books/$id/progress/ebook" -d "{\"fileId\":\"$file\",\"locator\":\"fraction:0.42\",\"fraction\":0.42,\"clientId\":\"upgrade-test\"}" >/dev/null
collection=$(json POST /collections -d '{"name":"Favourites"}' | jq -r .id)
json POST "/collections/$collection/books" -d "{\"books\":[\"$id\"]}" >/dev/null
json POST /auth/devices -d '{"name":"KOReader"}' | jq -r .token >"$work/device"
json POST /device/requests -d '{"username":"admin","deviceName":"Kobo"}' >/dev/null
call PUT "/books/$id/cover" -F "file=@$cover" >/dev/null
# The cover is written into the EPUB too, which changes its bytes.
for _ in $(seq 60); do
  call GET "/files/$file/download" | sha >"$work/file.sha"
  [[ $(cat "$work/file.sha") != $(sha <"$book") ]] && break
  sleep 1
done
[[ $(cat "$work/file.sha") != $(sha <"$book") ]] || fail "the cover is not written into the EPUB ($previous)"
call GET "/books/$id/covers/large" | sha >"$work/cover.sha"
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
