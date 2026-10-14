#!/usr/bin/env bash
# Runs the measurements of #244 on a sample of a library's EPUBs: Postgres
# with pglz and with lz4 TOAST compression, zstd stores of GOtome's own, and
# Tantivy directly; and those of #268: Manticore Search, against Tantivy
# without a store run again beside it. The library is mounted read-only; everything written
# goes to .cache/text-store, the numbers to spikes/text-store/results.
#
#   spikes/text-store/bench.sh <library> [step...]
#
# Steps: extract, pglz, lz4, zstd, tantivy (all, in that order, by default),
# and readtime, tantivy-none and manticore, which run only when named.
set -euo pipefail

library=${1:?the library folder to sample}
shift
steps=("$@")
[[ ${#steps[@]} -gt 0 ]] || steps=(extract pglz lz4 zstd tantivy)
root=$(cd "$(dirname "$0")/../.." && pwd)
spike=$root/spikes/text-store
work=$root/.cache/text-store
db_image=$(sed -n 's/^ *image: \(paradedb\/paradedb:.*\)$/\1/p' "$root/deploy/docker-compose.yml")
go_image=golang:1.27-bookworm
name=gotome-text-store-spike
manticore_image=manticoresearch/manticore:29.9.0@sha256:461b419739a2bbbffb4be0b32db26c41d7b435bf7fd1fce95b414ca5ccbb3775

mkdir -p "$work" "$root/.cache/go" "$spike/results"

go=(docker run --rm -u "$(id -u):$(id -g)" -v "$root:/work" -v "$root/.cache/go:/cache"
	-e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod
	-e GOFLAGS=-buildvcs=false -e GOTOOLCHAIN=local -w /work/spikes/text-store)

cleanup() {
	rm -f "$work/manticore-sampling"
	docker rm -f "$name-db" "$name-manticore" >/dev/null 2>&1 || true
	docker volume rm "$name-pg" "$name-manticore" >/dev/null 2>&1 || true
	docker network rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"${go[@]}" "$go_image" go build -o /work/.cache/text-store/text-store .

for step in "${steps[@]}"; do
	case $step in
	extract)
		"${go[@]}" -v "$library:/library:ro" "$go_image" \
			/work/.cache/text-store/text-store extract -out /work/.cache/text-store/chunks.jsonl.gz
		;;
	pglz | lz4)
		cleanup
		docker network create "$name" >/dev/null
		docker volume create "$name-pg" >/dev/null
		# The limits of #14: 2 GB, four CPUs, the same settings.
		docker run -d --name "$name-db" --network "$name" --memory 2g --memory-swap 2g --cpus 4 \
			--shm-size 1g -e POSTGRES_PASSWORD=spike -v "$name-pg:/var/lib/postgresql" "$db_image" \
			-c shared_buffers=512MB -c work_mem=32MB -c maintenance_work_mem=512MB \
			-c max_parallel_maintenance_workers=3 -c effective_cache_size=1GB \
			-c max_wal_size=16GB -c checkpoint_timeout=30min >/dev/null
		until docker exec "$name-db" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done
		sleep 2
		"${go[@]}" --network "$name" "$go_image" \
			/work/.cache/text-store/text-store pg -compression "$step" \
			-dsn "postgres://postgres:spike@$name-db/postgres?plan_cache_mode=force_custom_plan" \
			-out "results/pg-$step.json"
		docker exec "$name-db" du -sb /var/lib/postgresql | awk '{printf "  data directory %.1f MB\n", $1/1e6}'
		cleanup
		;;
	zstd)
		python3 -I "$spike/zstd_store.py" "$work/chunks.jsonl.gz" "$spike/results/zstd.json"
		;;
	tantivy)
		cargo build --offline --release --manifest-path "$spike/tantivy/Cargo.toml" --target-dir "$work/target"
		for store in none lz4 zstd3 zstd19-128; do
			rm -rf "$work/tantivy-$store"
			"$work/target/release/tantivy-spike" "$work/chunks.jsonl.gz" "$work/tantivy-$store" "$store" \
				"$spike/queries.json" "$spike/results/tantivy-$store.json"
		done
		;;
	readtime)
		"${go[@]}" -v "$library:/library:ro" "$go_image" \
			/work/.cache/text-store/text-store readtime -out results/readtime.json
		;;
	tantivy-none)
		# Beside manticore, with as many runs, both windows, and snippets
		# from the text handed in, as Manticore makes them.
		cargo build --offline --release --manifest-path "$spike/tantivy/Cargo.toml" --target-dir "$work/target"
		index=$work/tantivy-none-$(date +%s)
		RUNS=20 WINDOWS=1000,10 "$work/target/release/tantivy-spike" "$work/chunks.jsonl.gz" "$index" none \
			"$spike/queries.json" "$spike/results/tantivy-none-handed-in.json"
		;;
	manticore)
		# The image is the owner's to pull; nothing is downloaded here.
		docker image inspect "$manticore_image" >/dev/null
		cleanup
		docker network create "$name" >/dev/null
		docker volume create "$name-manticore" >/dev/null
		# The limits #14 gave Postgres: 2 GB and four CPUs.
		# Not under /etc/manticoresearch, which the entrypoint chowns.
		docker run -d --pull never --name "$name-manticore" --network "$name" --memory 2g --memory-swap 2g --cpus 4 \
			-v "$name-manticore:/var/lib/manticore" -v "$spike/manticore.conf:/spike/manticore.conf:ro" \
			"$manticore_image" searchd -c /spike/manticore.conf --nodetach >/dev/null
		phase=$work/manticore-phase samples=$work/manticore-samples
		echo starting >"$phase"
		: >"$samples"
		touch "$work/manticore-sampling"
		while [[ -f $work/manticore-sampling ]]; do
			echo "$(cat "$phase") $(docker stats --no-stream --format '{{.MemUsage}}' "$name-manticore" 2>/dev/null | cut -d' ' -f1)" >>"$samples"
		done &
		"${go[@]}" --network "$name" "$go_image" /work/.cache/text-store/text-store manticore-load \
			-url "http://$name-manticore:9308" -phase /work/.cache/text-store/manticore-phase \
			-out /work/.cache/text-store/manticore-load.json
		docker exec "$name-manticore" find /var/lib/manticore -type f -printf '%s %f\n' >"$work/manticore-files"
		echo restart >"$phase"
		start=$(date +%s%N)
		docker restart "$name-manticore" >/dev/null
		until docker exec "$name-manticore" mysql -h127.0.0.1 -P9306 -e 'SHOW TABLES' >/dev/null 2>&1; do sleep 0.2; done
		restart=$(awk -v s="$start" -v e="$(date +%s%N)" 'BEGIN { printf "%.1f", (e - s) / 1e9 }')
		"${go[@]}" --network "$name" "$go_image" /work/.cache/text-store/text-store manticore-search \
			-url "http://$name-manticore:9308" -phase /work/.cache/text-store/manticore-phase \
			-load /work/.cache/text-store/manticore-load.json -files /work/.cache/text-store/manticore-files \
			-samples /work/.cache/text-store/manticore-samples -restart-seconds "$restart" \
			-out results/manticore.json
		cleanup
		;;
	*)
		echo "unknown step $step" >&2
		exit 2
		;;
	esac
done
