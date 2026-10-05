#!/usr/bin/env bash
# Runs the embedding spike's measurements. Each run of a backend gets a
# container of its own with 2 GB of memory (MEMORY), no swap and four CPUs,
# as the search spike's engines did.
#
#   spikes/embed/run.sh check|speed|books|quality
set -euo pipefail

step=${1:?step: check, speed, books or quality}
root=$(cd "$(dirname "$0")/../.." && pwd)
embed=$root/.cache/embed
ort=$embed/onnxruntime-linux-x64-1.30.0/lib/libonnxruntime.so
image=${EMBED_GO_IMAGE:-golang:1.27-bookworm}
results=$root/spikes/embed/results
mkdir -p "$results"

[[ -f $embed/model/model.onnx ]] || { echo "no model: run make embed-assets first" >&2; exit 1; }

# run <name> <backend> <command and flags>: one limited container.
run() {
	local name=$1 backend=$2
	shift 2
	# The direct backend needs cgo, as ONNX Runtime does: it is in embed-ort.
	[[ $backend == direct ]] && backend=ort
	docker run --rm --name "gotome-embed-$name" -u "$(id -u):$(id -g)" \
		--memory "${MEMORY:-2g}" --memory-swap "${MEMORY:-2g}" --cpus 4 -e HOME=/tmp \
		-v "$root:/work" -v "$embed:/embed" -v "$root/.cache/corpus:/corpus" \
		-w /work/spikes/embed "$image" "/embed/embed-$backend" "$@"
}

ortflags=(-ort /embed/onnxruntime-linux-x64-1.30.0/lib)
[[ -f $ort ]] || ortflags=()

case $step in
check)
	for b in go ort; do
		for w in fp32 int8; do
			mem=2g
			[[ $b/$w == go/fp32 ]] && mem=8g
			MEMORY=$mem run "check-$b-$w" "$b" check -backend "$b" -weights "$w" "${ortflags[@]}" -out "results/check-$b-$w.json" || true
		done
	done
	# What hugot does without the harness's cut: its Go tokenizer cuts nothing.
	run check-ort-fp32-default ort check -backend ort -weights fp32 "${ortflags[@]}" -max-tokens 0 -out results/check-ort-fp32-default.json || true
	;;
speed)
	# BACKENDS narrows the run, as BACKENDS="ort direct" for those two.
	for b in ${BACKENDS:-go ort direct}; do
		for w in fp32 int8; do
			# The pure-Go backend with fp32 weights does not fit in 2 GB.
			mem=2g
			[[ $b/$w == go/fp32 ]] && mem=8g
			MEMORY=$mem run "speed-$b-$w" "$b" speed -backend "$b" -weights "$w" "${ortflags[@]}" \
				-n "${SPEED_PASSAGES:-128}" -cgroup /sys/fs/cgroup -out "results/speed-$b-$w.json" || true
		done
	done
	;;
books)
	backend=${BACKEND:-ort}
	weights=${WEIGHTS:-fp32}
	samples=${SAMPLES:-64}
	prefix=${PREFIX:-passage: }
	run "books-$backend-$weights" "$backend" books -backend "$backend" -weights "$weights" "${ortflags[@]}" \
		-samples "$samples" -prefix "$prefix" -out "/embed/vectors-$backend-$weights-$samples-${prefix%%:*}.gob"
	;;
quality)
	for v in "$embed"/vectors-*.gob; do
		name=$(basename "$v" .gob)
		run "quality-$name" go quality -vectors "/embed/$name.gob" -samples "${QUALITY_SAMPLES:-16,32,64}" -out "results/quality-${name#vectors-}.json"
	done
	;;
*)
	echo "unknown step $step" >&2
	exit 2
	;;
esac
