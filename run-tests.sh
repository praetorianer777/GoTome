#!/usr/bin/env bash
# The push gate: .claude/hooks/branch-guard.sh runs this before every git push,
# and CI runs the same script. Exits non-zero on the first failing stage.
set -euo pipefail
cd "$(dirname "$0")"

echo "🐚 Shell script tests"
.claude/hooks/tests/branch-guard-test.sh

# Each layer joins the gate in the change that introduces it, through a
# Makefile target of the same name, so this file only decides the order.
has_target() { make -n "$1" >/dev/null 2>&1; }

for layer in check-go check-web; do
  if has_target "$layer"; then
    echo "🔍 $layer"
    make "$layer"
  fi
done

echo "✅ All tests passed"
