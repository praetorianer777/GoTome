#!/usr/bin/env bash
# Runs one engine at one scale under the limits of #14: the engine's
# container gets 2 GB of memory, no swap, and four CPUs. Postgres runs in the
# image the compose file pins, with a fresh volume each time.
#
#   spikes/search/bench.sh <pgsearch|fts|bleve> [books]
#
# books is 0 or left out for the corpus once; more copies it.
set -euo pipefail

engine=${1:?engine: pgsearch, fts or bleve}
books=${2:-0}
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$root/.cache/search-spike
corpus=${CORPUS_DIR:-$root/.cache/corpus}
db_image=$(sed -n 's/^ *image: \(paradedb\/paradedb:.*\)$/\1/p' "$root/deploy/docker-compose.yml")
go_image=${SEARCH_GO_IMAGE:-golang:1.27-bookworm}
name=gotome-search-spike
limits=(--memory 2g --memory-swap 2g --cpus 4)
label=$([[ $books == 0 ]] && echo base || echo "$books")

mkdir -p "$work" "$root/.cache/go" "$root/spikes/search/results"
[[ -f $corpus/chunks.jsonl.gz ]] || { echo "no chunks: run make search-spike-extract first" >&2; exit 1; }

cleanup() {
	docker rm -f "$name-db" >/dev/null 2>&1 || true
	docker volume rm "$name-pg" >/dev/null 2>&1 || true
	docker network rm "$name" >/dev/null 2>&1 || true
	rm -rf "$corpus/bleve-index"
}
trap cleanup EXIT
cleanup

run=(docker run --rm -u "$(id -u):$(id -g)" --network "$name"
	-v "$root:/work" -v "$corpus:/corpus" -e HOME=/tmp -w /work/spikes/search)
docker network create "$name" >/dev/null

case $engine in
pgsearch | fts)
	docker volume create "$name-pg" >/dev/null
	id=$(docker run -d --name "$name-db" --network "$name" "${limits[@]}" --shm-size 1g \
		-e POSTGRES_PASSWORD=spike -v "$name-pg:/var/lib/postgresql" "$db_image" \
		-c shared_buffers=512MB -c work_mem=32MB -c maintenance_work_mem=512MB \
		-c max_parallel_maintenance_workers=3 -c effective_cache_size=1GB \
		-c max_wal_size=16GB -c checkpoint_timeout=30min)
	until docker exec "$name-db" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done
	"${run[@]}" -v /sys/fs/cgroup:/host-cgroup:ro "$go_image" \
		/work/.cache/search-spike/search bench -engine "$engine" -books "$books" \
		-dsn "postgres://postgres:spike@$name-db/postgres?plan_cache_mode=force_custom_plan" \
		-cgroup "/host-cgroup/system.slice/docker-$id.scope" \
		-out "results/$engine-$label.json"
	;;
bleve)
	"${run[@]}" "${limits[@]}" -e GOMEMLIMIT=1600MiB "$go_image" \
		/work/.cache/search-spike/search bench -engine bleve -books "$books" \
		-index /corpus/bleve-index -cgroup /sys/fs/cgroup \
		-out "results/bleve-$label.json"
	;;
*)
	echo "unknown engine $engine" >&2
	exit 2
	;;
esac
