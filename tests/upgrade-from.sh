#!/usr/bin/env bash
# Prints the image an upgrade is tested from: the latest release before this
# commit, as the release workflow published it, or, while there is none, one
# built from the commit this branch left main at (on main itself, the one
# before). The build is kept under the commit's name for the next run.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=$(git tag --merged HEAD^ --list 'v*' --sort=-v:refname | head -1)
if [[ -n $tag ]]; then
  echo "ghcr.io/praetorianer777/gotome:${tag#v}"
  exit 0
fi

base=$(git merge-base HEAD origin/main)
if [[ $base == $(git rev-parse HEAD) ]]; then
  base=$(git rev-parse HEAD^)
fi
image=gotome-upgrade-from:${base:0:12}
if ! docker image inspect "$image" >/dev/null 2>&1; then
  tree=$(mktemp -d)
  git worktree add --quiet --detach "$tree" "$base"
  trap 'git worktree remove --force "$tree"' EXIT
  echo "Building $image from $base" >&2
  docker build --quiet -f "$tree/deploy/Dockerfile" -t "$image" "$tree" >&2
fi
echo "$image"
