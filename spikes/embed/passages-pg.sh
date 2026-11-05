#!/usr/bin/env bash
# Measures passage vectors in Postgres (#174): the pinned database image
# under #14's limits, loaded from passage-export's file. See passages.sql.
#
#   spikes/embed/passages-pg.sh <base.tsv>
set -euo pipefail
base=$(realpath "${1:?the file passage-export wrote}")
here=$(cd "$(dirname "$0")" && pwd)
image=$(sed -n 's/^ *image: \(paradedb\/.*\)$/\1/p' "$here/../../deploy/docker-compose.yml")
name=gotome-passages-pg
docker rm -f "$name" >/dev/null 2>&1 || true
docker volume rm "$name" >/dev/null 2>&1 || true
trap 'docker rm -f "$name" >/dev/null 2>&1; docker volume rm "$name" >/dev/null 2>&1' EXIT
docker run -d --name "$name" --memory 2g --memory-swap 2g --cpus 4 --shm-size 1g \
	-e POSTGRES_PASSWORD=spike -v "$name:/var/lib/postgresql" \
	-v "$base:/in/base.tsv:ro" -v "$here/passages.sql:/in/passages.sql:ro" \
	"$image" -c shared_buffers=512MB -c max_wal_size=8GB >/dev/null
# The image starts a server of its own for its set-up first.
until docker logs "$name" 2>&1 | grep -q 'PostgreSQL init process complete'; do sleep 1; done
until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
docker exec "$name" psql -U postgres -v ON_ERROR_STOP=1 -f /in/passages.sql
