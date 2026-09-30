#!/usr/bin/env bash
set -euo pipefail

# The push gate: .claude/hooks/branch-guard.sh runs this before every git push,
# and CI runs the same script. Exits non-zero on the first failing stage.
# Stages are added by the issue that introduces what they test.

cd "$(dirname "$0")"

echo "🐚 Shell script tests"
.claude/hooks/tests/branch-guard-test.sh

echo "✅ All tests passed"
