#!/usr/bin/env bash
# Runs the genre spike (#451) on the host's Go toolchain, offline. It needs
# what the earlier spikes left in .cache: the chunk file (#14), the int8
# passage vectors (#15) and the model files (make embed-assets).
#
#   spikes/genres/run.sh gutenberg   records of the public corpus
#   spikes/genres/run.sh eval        the measurements on it (results/)
#   spikes/genres/run.sh shipped     the size and top words of a default model
#   spikes/genres/run.sh library DIR N   records of N books of a library, read
#                                        only, folder by folder; then
#                                        eval with LIBRARY=<file>
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
cache=$root/.cache
# A worktree keeps the embedding spike's files in the main checkout.
embedcache=${EMBED_CACHE:-$cache/embed}
work=${GENRES_WORK:-$cache/genres}
ort=$embedcache/onnxruntime-linux-x64-1.30.0/lib/libonnxruntime.so
mkdir -p "$work/model-root" "$work/bin"
# The harness opens the model offline: it fails rather than downloads.
ln -sfn "$embedcache/model" "$work/model-root/multilingual-e5-small"

export GOFLAGS=-mod=mod GOPROXY=off
export GOMODCACHE=${GOMODCACHE:-$cache/go/mod} GOCACHE=${GOCACHE:-$cache/go/build}
(cd "$root/spikes/genres" && go build -o "$work/bin/genres" .)
genres=$work/bin/genres
threads=${THREADS:-2}

case ${1:?step} in
gutenberg)
	"$genres" gutenberg -chunks "$cache/corpus/chunks.jsonl.gz" \
		-vectors "$embedcache/vectors-direct-int8-16-passage.gob" \
		-model-root "$work/model-root" -ort "$ort" -threads "$threads" -out "$work/gutenberg.gob"
	;;
library)
	"$genres" library -root "${2:?library folder}" -n "${3:-1500}" \
		-model-root "$work/model-root" -ort "$ort" -threads "$threads" -out "$work/library.gob"
	;;
eval)
	lib=()
	[[ -f ${LIBRARY:-} ]] && lib=(-library "$LIBRARY")
	"$genres" eval -gutenberg "$work/gutenberg.gob" "${lib[@]}" -workers "$threads" -out "$root/spikes/genres/results"
	;;
shipped)
	"$genres" shipped -gutenberg "$work/gutenberg.gob" -out "$root/spikes/genres/results/shipped.json" -workers "$threads"
	;;
stats)
	"$genres" stats -in "${2:?records file}"
	;;
*)
	echo "unknown step $1" >&2
	exit 2
	;;
esac
