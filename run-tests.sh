#!/usr/bin/env bash
# The push gate: .claude/hooks/branch-guard.sh runs this before every git push,
# and CI runs the same script. Exits non-zero on the first failing stage.
#
# ./run-tests.sh --full runs everything: the browser suite in every browser
# rather than the smoke tests in one. CI does that; a push does not, so that a
# push stays quick.
set -euo pipefail
cd "$(dirname "$0")"

FULL=
for arg in "$@"; do
  case "$arg" in
    --full) FULL=1 ;;
    *) echo "usage: $0 [--full]" >&2; exit 2 ;;
  esac
done
export FULL

echo "🐚 Shell script tests"
.claude/hooks/tests/branch-guard-test.sh
tests/test-release.sh

# Each layer joins the gate in the change that introduces it, through a
# Makefile target of the same name, so this file only decides the order.
has_target() { make -n "$1" >/dev/null 2>&1; }

for layer in check-go fuzz-go check-web; do
  if has_target "$layer"; then
    echo "🔍 $layer"
    make "$layer"
  fi
done

if has_target stack-up; then
  # One compose project and port per checkout, so gates running in parallel
  # worktrees never share a database or a port.
  trap 'make stack-down >/dev/null 2>&1 || true' EXIT
  echo "🐳 Stack"
  make stack-up
  layers="stack-check test-integration test-e2e"
  # The real embedding model is downloaded once per cache, which a push does
  # not wait for.
  [[ -n $FULL ]] && layers="stack-check embed-check test-integration test-e2e"
  for layer in $layers; do
    if has_target "$layer"; then
      echo "🧪 $layer"
      make "$layer"
    fi
  done
fi

echo "✅ All tests passed"
